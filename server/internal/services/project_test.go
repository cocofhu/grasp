package services

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestProjectCRUDAndDeleteConstraint(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "proj.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)

	p, err := s.Create("Alpha", "desc", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Name != "Alpha" || p.Description != "desc" {
		t.Fatalf("create = %+v", p)
	}
	if _, err := s.Create("Alpha", "", nil, nil); err != ErrProjectNameExists {
		t.Fatalf("dup name: %v", err)
	}

	name := "Alpha2"
	desc := "d2"
	p, err = s.Update(p.ID, &name, &desc, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Alpha2" || p.Description != "d2" {
		t.Fatalf("update = %+v", p)
	}

	if err := db.Create(&models.WorkflowDef{
		ID: "wf-1", ProjectID: p.ID, Name: "w", Status: "draft", Version: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(p.ID); err != ErrProjectHasWorkflows {
		t.Fatalf("delete with wf: %v", err)
	}
	if err := db.Delete(&models.WorkflowDef{}, "id = ?", "wf-1").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(p.ID); ok {
		t.Fatal("expected deleted")
	}
}

func TestProjectSecretMergeAndMask(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "secret.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	p, err := s.Create("S", "", []models.EnvEntry{
		{Key: "TOKEN", Value: "plain-secret", Secret: true},
		{Key: "PUBLIC", Value: "visible", Secret: false},
	}, []models.ProjectVariable{
		{Name: "api_key", Type: "string", Value: "sk-123", Secret: true},
		{Name: "region", Type: "string", Value: "cn", Secret: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	maskedEnv := MaskedSandboxEnv(p.SandboxEnv)
	if maskedEnv[0].Value != SecretMask || maskedEnv[1].Value != "visible" {
		t.Fatalf("mask env = %+v", maskedEnv)
	}
	maskedVars := MaskedProjectVars(p.Variables)
	if maskedVars[0].Value != SecretMask || maskedVars[1].Value != "cn" {
		t.Fatalf("mask vars = %+v", maskedVars)
	}

	// Empty/mask keeps plaintext; new value overwrites; delete key by omission; toggle secret.
	env := []models.EnvEntry{
		{Key: "TOKEN", Value: SecretMask, Secret: true},
		{Key: "PUBLIC", Value: "visible2", Secret: false},
		{Key: "NEW", Value: "n", Secret: false},
	}
	vars := []models.ProjectVariable{
		{Name: "api_key", Type: "string", Value: "", Secret: true},
		{Name: "region", Type: "string", Value: "us", Secret: true}, // become secret with new value
	}
	p, err = s.Update(p.ID, nil, nil, &env, &vars, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ProjectEnvMap(p.SandboxEnv)["TOKEN"]; got != "plain-secret" {
		t.Fatalf("TOKEN preserved = %q", got)
	}
	if got := ProjectEnvMap(p.SandboxEnv)["PUBLIC"]; got != "visible2" {
		t.Fatalf("PUBLIC = %q", got)
	}
	if got := ProjectEnvMap(p.SandboxEnv)["NEW"]; got != "n" {
		t.Fatalf("NEW = %q", got)
	}
	var apiKey, region models.ProjectVariable
	for _, v := range p.Variables {
		switch v.Name {
		case "api_key":
			apiKey = v
		case "region":
			region = v
		}
	}
	if apiKey.Value != "sk-123" {
		t.Fatalf("api_key preserved = %v", apiKey.Value)
	}
	if region.Value != "us" || !region.Secret {
		t.Fatalf("region = %+v", region)
	}

	// Explicit new secret value.
	env2 := []models.EnvEntry{{Key: "TOKEN", Value: "rotated", Secret: true}}
	p, err = s.Update(p.ID, nil, nil, &env2, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ProjectEnvMap(p.SandboxEnv)["TOKEN"]; got != "rotated" {
		t.Fatalf("TOKEN rotated = %q", got)
	}
}

func TestProjectSecretTogglePreservesPlaintext(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "toggle.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	p, err := s.Create("T", "", []models.EnvEntry{
		{Key: "TOKEN", Value: "real-secret", Secret: true},
	}, []models.ProjectVariable{
		{Name: "api_key", Type: "string", Value: "sk-real", Secret: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	// secret → non-secret with masked value must keep plaintext (not store ****).
	env := []models.EnvEntry{{Key: "TOKEN", Value: SecretMask, Secret: false}}
	vars := []models.ProjectVariable{{Name: "api_key", Type: "string", Value: SecretMask, Secret: false}}
	p, err = s.Update(p.ID, nil, nil, &env, &vars, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ProjectEnvMap(p.SandboxEnv)["TOKEN"]; got != "real-secret" {
		t.Fatalf("TOKEN after un-secret = %q", got)
	}
	tokSecret := false
	for _, e := range p.SandboxEnv {
		if e.Key == "TOKEN" {
			tokSecret = e.Secret
		}
	}
	if tokSecret {
		t.Fatal("TOKEN should no longer be secret")
	}
	var apiKey models.ProjectVariable
	for _, v := range p.Variables {
		if v.Name == "api_key" {
			apiKey = v
		}
	}
	if apiKey.Value != "sk-real" || apiKey.Secret {
		t.Fatalf("api_key after un-secret = %+v", apiKey)
	}

	// non-secret → secret with mask/empty keeps plaintext and flips flag.
	env2 := []models.EnvEntry{{Key: "TOKEN", Value: "", Secret: true}}
	vars2 := []models.ProjectVariable{{Name: "api_key", Type: "string", Value: SecretMask, Secret: true}}
	p, err = s.Update(p.ID, nil, nil, &env2, &vars2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ProjectEnvMap(p.SandboxEnv)["TOKEN"]; got != "real-secret" {
		t.Fatalf("TOKEN after re-secret = %q", got)
	}
	for _, e := range p.SandboxEnv {
		if e.Key == "TOKEN" && !e.Secret {
			t.Fatal("TOKEN should be secret again")
		}
	}
	for _, v := range p.Variables {
		if v.Name == "api_key" && (v.Value != "sk-real" || !v.Secret) {
			t.Fatalf("api_key after re-secret = %+v", v)
		}
	}
}

func TestProjectAcceptsPlatformAuthEnvKeyForcedSecret(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "authenv.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	// Create with official auth key unmarked as Secret — must force Secret.
	p, err := s.Create("A", "", []models.EnvEntry{
		{Key: "CURSOR_API_KEY", Value: "cursor-secret", Secret: false},
	}, nil)
	if err != nil {
		t.Fatalf("create auth key: %v", err)
	}
	if len(p.SandboxEnv) != 1 || !p.SandboxEnv[0].Secret || p.SandboxEnv[0].Value != "cursor-secret" {
		t.Fatalf("create forced secret = %+v", p.SandboxEnv)
	}
	masked := MaskedSandboxEnv(p.SandboxEnv)
	if masked[0].Value != SecretMask {
		t.Fatalf("masked create = %q", masked[0].Value)
	}

	// Update another official key without Secret flag; force Secret + keep prior on mask.
	env := []models.EnvEntry{
		{Key: "CURSOR_API_KEY", Value: SecretMask, Secret: false},
		{Key: "ANTHROPIC_API_KEY", Value: "anthropic-secret", Secret: false},
	}
	p, err = s.Update(p.ID, nil, nil, &env, nil, nil, nil)
	if err != nil {
		t.Fatalf("update auth key: %v", err)
	}
	byKey := map[string]models.EnvEntry{}
	for _, e := range p.SandboxEnv {
		byKey[e.Key] = e
	}
	if e := byKey["CURSOR_API_KEY"]; !e.Secret || e.Value != "cursor-secret" {
		t.Fatalf("CURSOR after update = %+v", e)
	}
	if e := byKey["ANTHROPIC_API_KEY"]; !e.Secret || e.Value != "anthropic-secret" {
		t.Fatalf("ANTHROPIC after update = %+v", e)
	}
	for _, e := range MaskedSandboxEnv(p.SandboxEnv) {
		if e.Value != SecretMask {
			t.Fatalf("masked %s = %q", e.Key, e.Value)
		}
	}
}

func TestProjectRejectsMaskOnRenamedKey(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "rename.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	p, err := s.Create("R", "", []models.EnvEntry{
		{Key: "TOKEN", Value: "real", Secret: true},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := []models.EnvEntry{{Key: "TOKEN2", Value: SecretMask, Secret: true}}
	if _, err := s.Update(p.ID, nil, nil, &env, nil, nil, nil); !errors.Is(err, ErrSecretPlaceholderOnNewKey) {
		t.Fatalf("rename with mask: %v", err)
	}
}

func TestProjectRejectsMaskOnCreateVars(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "create-mask.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	_, err = s.Create("C", "", nil, []models.ProjectVariable{
		{Name: "api_key", Type: "string", Value: SecretMask, Secret: true},
	})
	if !errors.Is(err, ErrSecretPlaceholderOnNewKey) {
		t.Fatalf("create vars with mask: %v", err)
	}
	_, err = s.Create("C", "", []models.EnvEntry{
		{Key: "TOKEN", Value: SecretMask, Secret: true},
	}, nil)
	if !errors.Is(err, ErrSecretPlaceholderOnNewKey) {
		t.Fatalf("create env with mask: %v", err)
	}
}

func TestDefaultProjectBackfill(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "bf.db"))
	if err != nil {
		t.Fatal(err)
	}
	var p models.Project
	if err := db.Where("id = ?", models.DefaultProjectID).First(&p).Error; err != nil {
		t.Fatalf("default project missing: %v", err)
	}
	if p.Name != models.DefaultProjectName {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestProjectListAndWorkflowLookups(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "proj-list.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)

	list := s.List()
	if len(list) == 0 {
		t.Fatal("expected default project in list")
	}
	if id := s.DefaultProjectID(); id != models.DefaultProjectID {
		t.Fatalf("DefaultProjectID=%q", id)
	}
	if got := FormatProjectHasWorkflowsError(3); !strings.Contains(got, "3") {
		t.Fatalf("format: %q", got)
	}

	p, err := s.Create("LookMe", "", []models.EnvEntry{{Key: "K", Value: "v"}}, []models.ProjectVariable{{Name: "v1", Type: "string", Value: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowDef{
		ID: "wf-look", ProjectID: p.ID, Name: "w", Status: "draft", Version: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	env := s.SandboxEnvForWorkflow("wf-look")
	if len(env) != 1 || env[0].Key != "K" {
		t.Fatalf("env=%+v", env)
	}
	vars := s.VariablesForWorkflow("wf-look")
	if len(vars) != 1 || vars[0].Name != "v1" {
		t.Fatalf("vars=%+v", vars)
	}
	if s.SandboxEnvForWorkflow("missing") != nil || s.VariablesForWorkflow("missing") != nil {
		t.Fatal("missing workflow should yield nil")
	}
	// Workflow with empty project_id
	if err := db.Create(&models.WorkflowDef{
		ID: "wf-empty-pid", ProjectID: "", Name: "e", Status: "draft", Version: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if s.SandboxEnvForWorkflow("wf-empty-pid") != nil {
		t.Fatal("empty project_id")
	}
	// Workflow pointing at deleted project
	if err := db.Create(&models.WorkflowDef{
		ID: "wf-orphan", ProjectID: "no-such-proj", Name: "o", Status: "draft", Version: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if s.SandboxEnvForWorkflow("wf-orphan") != nil || s.VariablesForWorkflow("wf-orphan") != nil {
		t.Fatal("orphan project")
	}

	// DefaultProjectID falls back to oldest when default row is gone.
	db.Exec("DELETE FROM projects WHERE id = ?", models.DefaultProjectID)
	if id := s.DefaultProjectID(); id == "" || id == models.DefaultProjectID {
		t.Fatalf("fallback DefaultProjectID=%q", id)
	}
	db.Exec("DELETE FROM projects")
	if id := s.DefaultProjectID(); id != "" {
		t.Fatalf("empty projects DefaultProjectID=%q", id)
	}
}

func TestTotalTokensByProjectIDs(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "proj_tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)

	noRun, err := s.Create("NoRun", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	noUsage, err := s.Create("NoUsage", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := s.Create("Partial", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := s.Create("Zero", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	large, err := s.Create("Large", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	mustCreate(&models.WorkflowDef{ID: "wf-nousage", ProjectID: noUsage.ID, Name: "w", Status: "draft", Version: 1})
	mustCreate(&models.Run{ID: "run-nousage", WorkflowID: "wf-nousage", Status: "completed"})
	mustCreate(&models.StateRun{RunID: "run-nousage", NodeID: "n1", Status: "completed"})

	mustCreate(&models.WorkflowDef{ID: "wf-partial", ProjectID: partial.ID, Name: "w", Status: "draft", Version: 1})
	mustCreate(&models.Run{ID: "run-partial-a", WorkflowID: "wf-partial", Status: "failed"})
	mustCreate(&models.Run{ID: "run-partial-b", WorkflowID: "wf-partial", Status: "running"})
	mustCreate(&models.StateRun{RunID: "run-partial-a", NodeID: "n1", Status: "failed"}) // no usage
	mustCreate(&models.StateRun{
		RunID: "run-partial-a", NodeID: "n2", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 100, OutputTokens: 20},
	})
	mustCreate(&models.StateRun{
		RunID: "run-partial-b", NodeID: "n1", Status: "running",
		Usage: &models.TokenUsage{InputTokens: 5, CacheReadTokens: 3},
	})

	mustCreate(&models.WorkflowDef{ID: "wf-zero", ProjectID: zero.ID, Name: "w", Status: "draft", Version: 1})
	mustCreate(&models.Run{ID: "run-zero", WorkflowID: "wf-zero", Status: "cancelled"})
	mustCreate(&models.StateRun{
		RunID: "run-zero", NodeID: "n1", Status: "cancelled",
		Usage: &models.TokenUsage{},
	})

	mustCreate(&models.WorkflowDef{ID: "wf-large", ProjectID: large.ID, Name: "w", Status: "draft", Version: 1})
	mustCreate(&models.Run{ID: "run-large", WorkflowID: "wf-large", Status: "completed"})
	mustCreate(&models.StateRun{
		RunID: "run-large", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 1_000_000, OutputTokens: 20_000},
	})
	mustCreate(&models.StateRun{
		RunID: "run-large", NodeID: "n2", Status: "completed",
		Usage: &models.TokenUsage{CacheWriteTokens: 400},
	})

	syncTokenLedger(t, db)
	got := s.totalTokensByProjectIDs([]string{noRun.ID, noUsage.ID, partial.ID, zero.ID, large.ID})

	if _, ok := got[noRun.ID]; ok {
		t.Fatalf("no-run project should be absent (null): %v", got[noRun.ID])
	}
	if _, ok := got[noUsage.ID]; ok {
		t.Fatalf("all-nil-usage project should be absent (null): %v", got[noUsage.ID])
	}
	if got[partial.ID] == nil || *got[partial.ID] != 128 {
		t.Fatalf("partial = %v want 128", got[partial.ID])
	}
	if got[zero.ID] == nil || *got[zero.ID] != 0 {
		t.Fatalf("zero = %v want 0", got[zero.ID])
	}
	if got[large.ID] == nil || *got[large.ID] != 1_020_400 {
		t.Fatalf("large = %v want 1020400", got[large.ID])
	}
	syncTokenLedger(t, db)
	if single := s.totalTokens(partial.ID); single == nil || *single != 128 {
		t.Fatalf("TotalTokens(partial) = %v", single)
	}
	syncTokenLedger(t, db)
	if s.totalTokens(noRun.ID) != nil {
		t.Fatal("TotalTokens(noRun) should be nil")
	}

	// g2.1 / g2.5: PM Usage merges into totals with source split; no Usage = no backfill.
	pmOnly, err := s.Create("PMOnly", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(&models.ChatThread{ID: "th-pm-only", ProjectID: pmOnly.ID, UserID: "u1", Title: "t"})
	mustCreate(&models.ChatMessage{
		ID: "msg-hist", ThreadID: "th-pm-only", Role: "assistant", Content: "old",
		Status: "ok", CreatedAt: time.Now(),
	})
	mustCreate(&models.ChatMessage{
		ID: "msg-pm-only", ThreadID: "th-pm-only", Role: "assistant", Content: "hi",
		Status: "ok", CreatedAt: time.Now(),
		Usage: &models.TokenUsage{InputTokens: 7, OutputTokens: 3},
	})
	syncTokenLedger(t, db)
	bd := s.TokenBreakdown(pmOnly.ID)
	if bd.Workflow != nil {
		t.Fatalf("pm-only should have nil workflow: %v", bd.Workflow)
	}
	if bd.PM == nil || *bd.PM != 10 {
		t.Fatalf("pm-only pm=%v want 10", bd.PM)
	}
	if bd.Total == nil || *bd.Total != 10 {
		t.Fatalf("pm-only total=%v want 10", bd.Total)
	}
	// Merged with workflow project
	mustCreate(&models.ChatThread{ID: "th-partial", ProjectID: partial.ID, UserID: "u1", Title: "t"})
	mustCreate(&models.ChatMessage{
		ID: "msg-partial-pm", ThreadID: "th-partial", Role: "assistant", Content: "x",
		Status: "ok", CreatedAt: time.Now(),
		Usage: &models.TokenUsage{InputTokens: 2},
	})
	syncTokenLedger(t, db)
	merged := s.TokenBreakdown(partial.ID)
	if merged.Total == nil || *merged.Total != 130 {
		t.Fatalf("partial+pm total=%v want 130", merged.Total)
	}
	if merged.Workflow == nil || *merged.Workflow != 128 {
		t.Fatalf("partial workflow=%v want 128", merged.Workflow)
	}
	if merged.PM == nil || *merged.PM != 2 {
		t.Fatalf("partial pm=%v want 2", merged.PM)
	}
}

func TestEnvEntryIsEnabledLegacyCompat(t *testing.T) {
	var legacy models.EnvEntry
	if err := json.Unmarshal([]byte(`{"key":"LEGACY","value":"1"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if !legacy.IsEnabled() {
		t.Fatal("legacy JSON without enabled must IsEnabled()==true")
	}
	if legacy.Enabled != nil {
		t.Fatalf("legacy Enabled pointer = %v, want nil", legacy.Enabled)
	}

	on := models.EnvEntry{Key: "ON", Value: "1", Enabled: boolPtr(true)}
	off := models.EnvEntry{Key: "OFF", Value: "0", Enabled: boolPtr(false)}
	if !on.IsEnabled() || off.IsEnabled() {
		t.Fatalf("explicit true/false: on=%v off=%v", on.IsEnabled(), off.IsEnabled())
	}
}

func TestProjectEnvMapSkipsDisabled(t *testing.T) {
	env := []models.EnvEntry{
		{Key: "KEEP", Value: "yes"},
		{Key: "NIL_ON", Value: "n", Enabled: nil},
		{Key: "EXPLICIT_ON", Value: "e", Enabled: boolPtr(true)},
		{Key: "OFF", Value: "no", Enabled: boolPtr(false)},
		{Key: "", Value: "empty-key", Enabled: boolPtr(true)},
	}
	m := ProjectEnvMap(env)
	if len(m) != 3 {
		t.Fatalf("map size=%d want 3: %+v", len(m), m)
	}
	if m["KEEP"] != "yes" || m["NIL_ON"] != "n" || m["EXPLICIT_ON"] != "e" {
		t.Fatalf("enabled keys = %+v", m)
	}
	if _, ok := m["OFF"]; ok {
		t.Fatal("disabled OFF must be skipped")
	}
}

func TestProjectEnvEnabledRoundTrip(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "env-enabled.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	p, err := s.Create("EnvEn", "", []models.EnvEntry{
		{Key: "A", Value: "1", Secret: false, Enabled: boolPtr(true)},
		{Key: "B", Value: "2", Secret: true, Enabled: boolPtr(false)},
		{Key: "C", Value: "3", Secret: false}, // nil Enabled
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]models.EnvEntry{}
	for _, e := range p.SandboxEnv {
		byKey[e.Key] = e
	}
	if !byKey["A"].IsEnabled() || byKey["B"].IsEnabled() || !byKey["C"].IsEnabled() {
		t.Fatalf("after create: %+v", p.SandboxEnv)
	}
	m := ProjectEnvMap(p.SandboxEnv)
	if _, ok := m["B"]; ok {
		t.Fatal("B disabled must not inject after create")
	}
	if m["A"] != "1" || m["C"] != "3" {
		t.Fatalf("inject map after create = %+v", m)
	}

	// Settings / SandboxEnvForWorkflow still return disabled rows (full list).
	if len(p.SandboxEnv) != 3 {
		t.Fatalf("settings list len=%d want 3", len(p.SandboxEnv))
	}
	if err := db.Create(&models.WorkflowDef{
		ID: "wf-env-en", ProjectID: p.ID, Name: "w", Status: "draft", Version: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	full := s.SandboxEnvForWorkflow("wf-env-en")
	if len(full) != 3 {
		t.Fatalf("SandboxEnvForWorkflow len=%d want 3 (incl. disabled)", len(full))
	}
	masked := MaskedSandboxEnv(p.SandboxEnv)
	for _, e := range masked {
		if e.Key == "B" {
			if e.IsEnabled() || e.Value != SecretMask || e.Enabled == nil || *e.Enabled {
				t.Fatalf("masked disabled B = %+v", e)
			}
		}
	}

	// Update round-trip: keep B disabled with mask; flip A off; leave C nil→submit false then true.
	env := []models.EnvEntry{
		{Key: "A", Value: "1", Secret: false, Enabled: boolPtr(false)},
		{Key: "B", Value: SecretMask, Secret: true, Enabled: boolPtr(false)},
		{Key: "C", Value: "3", Secret: false, Enabled: boolPtr(true)},
	}
	p, err = s.Update(p.ID, nil, nil, &env, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	byKey = map[string]models.EnvEntry{}
	for _, e := range p.SandboxEnv {
		byKey[e.Key] = e
	}
	if byKey["A"].IsEnabled() {
		t.Fatal("A should stay disabled after update")
	}
	if byKey["B"].IsEnabled() || byKey["B"].Value != "2" {
		t.Fatalf("B disabled+secret preserve = %+v", byKey["B"])
	}
	if !byKey["C"].IsEnabled() {
		t.Fatal("C should be enabled")
	}
	m = ProjectEnvMap(p.SandboxEnv)
	if len(m) != 1 || m["C"] != "3" {
		t.Fatalf("inject after disable A = %+v", m)
	}
}

func TestNormalizeUnknownModelDisplayName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{models.TokenUsageModelUnknown, "", false},
		{models.TokenUsageModelUnknownDisplay, "", false},
		{" gpt-5 ", "gpt-5", false},
		{strings.Repeat("a", 65), "", true},
		{strings.Repeat("中", 64), strings.Repeat("中", 64), false},
		{strings.Repeat("中", 65), "", true},
	}
	for _, tc := range cases {
		got, err := NormalizeUnknownModelDisplayName(tc.in)
		if tc.wantErr {
			if !errors.Is(err, ErrUnknownModelDisplayNameTooLong) {
				t.Fatalf("in=%q err=%v", tc.in, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("in=%q got=%q err=%v want=%q", tc.in, got, err, tc.want)
		}
	}
}

func TestProjectUpdateUnknownModelDisplayName(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "proj_unk.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectService(db)
	p, err := s.Create("UnkAlias", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	alias := "gpt-5"
	p, err = s.Update(p.ID, nil, nil, nil, nil, nil, &alias)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "gpt-5" {
		t.Fatalf("alias=%q", p.UnknownModelDisplayName)
	}
	sameAsDefault := models.TokenUsageModelUnknownDisplay
	p, err = s.Update(p.ID, nil, nil, nil, nil, nil, &sameAsDefault)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("new default name should clear, got %q", p.UnknownModelDisplayName)
	}
	sameAsLegacy := models.TokenUsageModelUnknown
	p, err = s.Update(p.ID, nil, nil, nil, nil, nil, &sameAsLegacy)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("default name should clear, got %q", p.UnknownModelDisplayName)
	}
	tooLong := strings.Repeat("x", 65)
	if _, err := s.Update(p.ID, nil, nil, nil, nil, nil, &tooLong); !errors.Is(err, ErrUnknownModelDisplayNameTooLong) {
		t.Fatalf("want too-long err, got %v", err)
	}
	blank := "   "
	p, err = s.Update(p.ID, nil, nil, nil, nil, nil, &blank)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("blank should clear, got %q", p.UnknownModelDisplayName)
	}
}
