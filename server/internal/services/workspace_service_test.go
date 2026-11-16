package services

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkspaceUpdateNameAndDescription(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	name, desc := "  Alpha2 ", "d2"
	ws, err := s.Update(&name, &desc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "Alpha2" || ws.Description != "d2" {
		t.Fatalf("update = %+v", ws)
	}
	blank := "  "
	if _, err := s.Update(&blank, nil, nil); !errors.Is(err, ErrEmptyWorkspaceName) {
		t.Fatalf("blank name: %v", err)
	}
	if n := s.WorkflowCount(); n != 0 {
		t.Fatalf("workflow count = %d", n)
	}
	if err := db.Create(&models.WorkflowDef{ID: "wf-1", Name: "w", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if n := s.WorkflowCount(); n != 1 {
		t.Fatalf("workflow count = %d", n)
	}
}

func TestWorkflowVarsSecretMergeAndMask(t *testing.T) {
	existing, err := sanitizeWorkspaceVars([]models.WorkspaceVariable{
		{Name: "api_key", Type: "string", Value: "sk-123", Secret: true},
		{Name: "region", Type: "string", Value: "cn", Secret: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	maskedEnv := MaskedSandboxEnv([]models.EnvEntry{
		{Key: "TOKEN", Value: "plain-secret", Secret: true},
		{Key: "PUBLIC", Value: "visible"},
	})
	if maskedEnv[0].Value != SecretMask || maskedEnv[1].Value != "visible" {
		t.Fatalf("mask env = %+v", maskedEnv)
	}
	maskedVars := MaskedWorkspaceVars(existing)
	if maskedVars[0].Value != SecretMask || maskedVars[1].Value != "cn" {
		t.Fatalf("mask vars = %+v", maskedVars)
	}

	// Empty/mask keeps plaintext; new value overwrites; toggle secret.
	merged, err := mergeWorkspaceVars(existing, []models.WorkspaceVariable{
		{Name: "api_key", Type: "string", Value: "", Secret: true},
		{Name: "region", Type: "string", Value: "us", Secret: true}, // become secret with new value
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged[0].Value != "sk-123" {
		t.Fatalf("api_key preserved = %v", merged[0].Value)
	}
	if merged[1].Value != "us" || !merged[1].Secret {
		t.Fatalf("region = %+v", merged[1])
	}
}

func TestWorkflowVarsSecretToggleRequiresExplicitPublicValue(t *testing.T) {
	existing := []models.WorkspaceVariable{{Name: "api_key", Type: "string", Value: "sk-real", Secret: true}}

	// A mask is not consent to expose the stored secret as a public value.
	vars := []models.WorkspaceVariable{{Name: "api_key", Type: "string", Value: SecretMask, Secret: false}}
	if _, err := mergeWorkspaceVars(existing, vars); !errors.Is(err, ErrSecretPlaceholderOnPublicValue) {
		t.Fatalf("un-secret mask accepted: %v", err)
	}
	for _, value := range []any{"", nil, "public replacement"} {
		vars[0].Value = value
		merged, err := mergeWorkspaceVars(existing, vars)
		if err != nil {
			t.Fatal(err)
		}
		if got := merged[0]; got.Secret || got.Value != value {
			t.Fatalf("un-secret value=%v produced %+v", value, got)
		}
	}
}

func TestWorkflowVarsRejectMaskOnRenamedKey(t *testing.T) {
	existing := []models.WorkspaceVariable{{Name: "token", Type: "string", Value: "v", Secret: true}}
	vars := []models.WorkspaceVariable{{Name: "token2", Type: "string", Value: SecretMask, Secret: true}}
	if _, err := mergeWorkspaceVars(existing, vars); !errors.Is(err, ErrSecretPlaceholderOnNewKey) {
		t.Fatalf("rename with mask: %v", err)
	}
}

func TestWorkflowVarsRejectMaskOnCreate(t *testing.T) {
	_, err := sanitizeWorkspaceVars([]models.WorkspaceVariable{
		{Name: "api_key", Type: "string", Value: SecretMask, Secret: true},
	})
	if !errors.Is(err, ErrSecretPlaceholderOnNewKey) {
		t.Fatalf("create vars with mask: %v", err)
	}
}

func TestWorkspaceSeededOnEmptyDB(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "bf.db"))
	if err != nil {
		t.Fatal(err)
	}
	var p models.Workspace
	if err := db.Where("id = ?", models.WorkspaceID).First(&p).Error; err != nil {
		t.Fatalf("workspace missing: %v", err)
	}
	if p.Name != models.DefaultWorkspaceName {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestWorkspaceVariablesForWorkflow(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ws-vars.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	if err := db.Create(&models.WorkflowDef{ID: "wf-bare", Name: "w", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if vars := s.VariablesForWorkflow("wf-bare"); vars != nil {
		t.Fatalf("workflow without defaults = %+v", vars)
	}
	wf := models.WorkflowDef{ID: "wf-look", Name: "look", Version: 1, Defaults: &models.WorkflowDefaults{Variables: []models.WorkspaceVariable{{Name: "token", Type: "string", Value: "workflow-token", Secret: true}}}}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	vars := s.VariablesForWorkflow("wf-look")
	if len(vars) != 1 || vars[0].Value != "workflow-token" || !vars[0].Secret {
		t.Fatalf("vars=%+v", vars)
	}
	if s.VariablesForWorkflow("missing") != nil {
		t.Fatal("missing workflow should yield nil")
	}
}

func TestWorkspaceTokenBreakdown(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "ws_tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	syncTokenLedger(t, db)
	if bd := s.TokenBreakdown(); bd.Total != nil || bd.Workflow != nil || bd.PM != nil {
		t.Fatalf("empty breakdown = %+v", bd)
	}

	mustCreate(&models.WorkflowDef{ID: "wf-partial", Name: "w", Version: 1})
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
	syncTokenLedger(t, db)
	bd := s.TokenBreakdown()
	if bd.Workflow == nil || *bd.Workflow != 128 || bd.PM != nil || bd.Total == nil || *bd.Total != 128 {
		t.Fatalf("workflow-only breakdown = %+v", bd)
	}

	mustCreate(&models.ChatThread{ID: "th-pm", UserID: "u1", Title: "t"})
	mustCreate(&models.ChatMessage{
		ID: "msg-hist", ThreadID: "th-pm", Role: "assistant", Content: "old",
		Status: "ok", CreatedAt: time.Now(),
	})
	mustCreate(&models.ChatMessage{
		ID: "msg-pm", ThreadID: "th-pm", Role: "assistant", Content: "hi",
		Status: "ok", CreatedAt: time.Now(),
		Usage: &models.TokenUsage{InputTokens: 2},
	})
	syncTokenLedger(t, db)
	merged := s.TokenBreakdown()
	if merged.Total == nil || *merged.Total != 130 {
		t.Fatalf("total=%v want 130", merged.Total)
	}
	if merged.Workflow == nil || *merged.Workflow != 128 {
		t.Fatalf("workflow=%v want 128", merged.Workflow)
	}
	if merged.PM == nil || *merged.PM != 2 {
		t.Fatalf("pm=%v want 2", merged.PM)
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

func TestWorkspaceUpdateUnknownModelDisplayName(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "proj_unk.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	p, err := s.CreateFixture("UnkAlias", "")
	if err != nil {
		t.Fatal(err)
	}
	alias := "gpt-5"
	p, err = s.Update(nil, nil, &alias)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "gpt-5" {
		t.Fatalf("alias=%q", p.UnknownModelDisplayName)
	}
	sameAsDefault := models.TokenUsageModelUnknownDisplay
	p, err = s.Update(nil, nil, &sameAsDefault)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("new default name should clear, got %q", p.UnknownModelDisplayName)
	}
	sameAsLegacy := models.TokenUsageModelUnknown
	p, err = s.Update(nil, nil, &sameAsLegacy)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("default name should clear, got %q", p.UnknownModelDisplayName)
	}
	tooLong := strings.Repeat("x", 65)
	if _, err := s.Update(nil, nil, &tooLong); !errors.Is(err, ErrUnknownModelDisplayNameTooLong) {
		t.Fatalf("want too-long err, got %v", err)
	}
	blank := "   "
	p, err = s.Update(nil, nil, &blank)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("blank should clear, got %q", p.UnknownModelDisplayName)
	}
}
