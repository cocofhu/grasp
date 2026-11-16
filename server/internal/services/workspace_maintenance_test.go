package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestCodexRefreshTargetsSelectedCredentialAndCannotReviveIt(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	svc := NewCredentialService(db)
	login := `{"auth_mode":"chatgpt","tokens":{"access_token":"original","refresh_token":"refresh"}}`
	refreshed := `{"auth_mode":"chatgpt","tokens":{"access_token":"refreshed","refresh_token":"refresh"}}`
	first, err := svc.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "First", Value: login})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "Second", Value: login})
	if err != nil {
		t.Fatal(err)
	}
	if err = saveCodexLoginForTest(svc, second.ID, refreshed); err != nil {
		t.Fatal(err)
	}
	_, got, ok := svc.ResolveSelectedValue(first.ID)
	if !ok || got != login {
		t.Fatal("refresh overwrote another credential")
	}
	_, got, ok = svc.ResolveSelectedValue(second.ID)
	if !ok || got != refreshed {
		t.Fatal("refresh did not update selected credential")
	}
	if err = svc.Revoke(second.ID); err != nil {
		t.Fatal(err)
	}
	if err = saveCodexLoginForTest(svc, second.ID, login); err == nil {
		t.Fatal("revoked credential revived")
	}
	if err = svc.Clear(first.ID); err != nil {
		t.Fatal(err)
	}
	if err = saveCodexLoginForTest(svc, first.ID, refreshed); err == nil {
		t.Fatal("cleared credential revived")
	}
}

func TestMaintenanceArchiveIsPrivateAndRejectsSymlinks(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(filepath.Join(src, "auth.json"), []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivateTree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("archive exposed private files")
	}
	if err = os.Symlink(filepath.Join(src, "auth.json"), filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err = copyPrivateTree(src, dst); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatal("unsafe archive accepted symlink")
	}
}

func TestWorkflowGatePolicyUsesOrdinaryOverrideAndStopsAdmission(t *testing.T) {
	db := newTestDB(t)
	pm := NewPmService(db, NewAgentService(t.TempDir()))
	svc := NewGateAutoInvokeService(db, pm, nil, nil, GateAutoTokenHooks{})
	wf := models.WorkflowDef{ID: "override", Name: "override", Defaults: &models.WorkflowDefaults{GateAuto: &models.WorkflowGateAutoPolicy{Enabled: true, AgentName: "reviewer", Variable: "approve", Prompt: "inspect", EnabledMcps: []string{"pm-workflow-write"}}}}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	p, ok := svc.effectiveWorkspace(GateAutoTask{WorkflowID: wf.ID})
	if !ok || p.PmLeaderAgent != "reviewer" || p.PmGateAutoVar != "approve" || !p.PmLeaderEnabled {
		t.Fatalf("override=%+v %v", p, ok)
	}
	svc.BeginMaintenance()
	svc.Enqueue(GateAutoTask{WorkflowID: wf.ID, Vars: map[string]any{"approve": true}})
	if svc.PendingMaintenance() != 0 {
		t.Fatal("maintenance accepted new gate task")
	}
}

func saveCodexLoginForTest(svc *CredentialService, id, content string) error {
	prev, err := svc.codexLogin(id)
	if err != nil {
		return err
	}
	_, err = svc.saveCodexLogin(prev, content)
	return err
}
