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
	ws, err := s.Update(&name, &desc, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "Alpha2" || ws.Description != "d2" {
		t.Fatalf("update = %+v", ws)
	}
	blank := "  "
	if _, err := s.Update(&blank, nil, nil, nil, nil); !errors.Is(err, ErrEmptyWorkspaceName) {
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

func TestWorkspaceSecretMergeAndMask(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "secret.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	p, err := s.CreateFixture("S", "", []models.WorkspaceVariable{
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
	maskedVars := MaskedWorkspaceVars(p.Variables)
	if maskedVars[0].Value != SecretMask || maskedVars[1].Value != "cn" {
		t.Fatalf("mask vars = %+v", maskedVars)
	}

	// Empty/mask keeps plaintext; new value overwrites; toggle secret.
	vars := []models.WorkspaceVariable{
		{Name: "api_key", Type: "string", Value: "", Secret: true},
		{Name: "region", Type: "string", Value: "us", Secret: true}, // become secret with new value
	}
	p, err = s.Update(nil, nil, &vars, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var apiKey, region models.WorkspaceVariable
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
}

func TestWorkspaceSecretToggleRequiresExplicitPublicValue(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "toggle.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	_, err = s.CreateFixture("T", "", []models.WorkspaceVariable{
		{Name: "api_key", Type: "string", Value: "sk-real", Secret: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A mask is not consent to expose the stored secret as a public value.
	vars := []models.WorkspaceVariable{{Name: "api_key", Type: "string", Value: SecretMask, Secret: false}}
	if _, err = s.Update(nil, nil, &vars, nil, nil); !errors.Is(err, ErrSecretPlaceholderOnPublicValue) {
		t.Fatalf("un-secret mask accepted: %v", err)
	}
	stored, _ := s.Workspace()
	if !stored.Variables[0].Secret || stored.Variables[0].Value != "sk-real" {
		t.Fatal("rejected update changed the stored secret")
	}
	for _, value := range []any{"", nil, "public replacement"} {
		// Restore a secret before checking each transition.
		secret := []models.WorkspaceVariable{{Name: "api_key", Type: "string", Value: "sk-real", Secret: true}}
		if _, err = s.Update(nil, nil, &secret, nil, nil); err != nil {
			t.Fatal(err)
		}
		vars[0].Value = value
		p, err := s.Update(nil, nil, &vars, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Variables[0]; got.Secret || got.Value != value {
			t.Fatalf("un-secret value=%v produced %+v", value, got)
		}
	}
}

func TestWorkspaceRejectsMaskOnRenamedKey(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "rename.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	_, err = s.CreateFixture("R", "", []models.WorkspaceVariable{{Name: "token", Type: "string", Value: "v", Secret: true}})
	if err != nil {
		t.Fatal(err)
	}
	vars := []models.WorkspaceVariable{{Name: "token2", Type: "string", Value: SecretMask, Secret: true}}
	if _, err := s.Update(nil, nil, &vars, nil, nil); !errors.Is(err, ErrSecretPlaceholderOnNewKey) {
		t.Fatalf("rename with mask: %v", err)
	}
}

func TestWorkspaceRejectsMaskOnCreateVars(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "create-mask.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceService(db)
	_, err = s.CreateFixture("C", "", []models.WorkspaceVariable{
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
	if _, err := s.CreateFixture("LookMe", "", []models.WorkspaceVariable{{Name: "v1", Type: "string", Value: "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowDef{ID: "wf-look", Name: "w", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	vars := s.VariablesForWorkflow("wf-look")
	if len(vars) != 1 || vars[0].Name != "v1" {
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
	p, err := s.CreateFixture("UnkAlias", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	alias := "gpt-5"
	p, err = s.Update(nil, nil, nil, nil, &alias)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "gpt-5" {
		t.Fatalf("alias=%q", p.UnknownModelDisplayName)
	}
	sameAsDefault := models.TokenUsageModelUnknownDisplay
	p, err = s.Update(nil, nil, nil, nil, &sameAsDefault)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("new default name should clear, got %q", p.UnknownModelDisplayName)
	}
	sameAsLegacy := models.TokenUsageModelUnknown
	p, err = s.Update(nil, nil, nil, nil, &sameAsLegacy)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("default name should clear, got %q", p.UnknownModelDisplayName)
	}
	tooLong := strings.Repeat("x", 65)
	if _, err := s.Update(nil, nil, nil, nil, &tooLong); !errors.Is(err, ErrUnknownModelDisplayNameTooLong) {
		t.Fatalf("want too-long err, got %v", err)
	}
	blank := "   "
	p, err = s.Update(nil, nil, nil, nil, &blank)
	if err != nil {
		t.Fatal(err)
	}
	if p.UnknownModelDisplayName != "" {
		t.Fatalf("blank should clear, got %q", p.UnknownModelDisplayName)
	}
}
