package services

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestRunNodeSandboxRowSharedFallback(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)

	own := runtimeInfo("grasp-sb-own")
	s.RegisterRunSandbox(own)
	if _, err := runNodeSandboxRow(db, own.RunID, "other"); err == nil {
		t.Fatal("per-node run fell back to another node's sandbox")
	}

	shared := runtimeInfo("grasp-sb-shared")
	shared.RunID, shared.NodeID, shared.Shared = "run-shared", "n2", true
	s.RegisterRunSandbox(shared)
	var row models.Sandbox
	db.Where("name = ?", shared.Name).First(&row)
	if !row.Shared || row.NodeID != "n2" {
		t.Fatalf("shared row = %+v", row)
	}
	got, err := runNodeSandboxRow(db, "run-shared", "n1")
	if err != nil || got.Name != shared.Name {
		t.Fatalf("fallback = %+v, %v", got, err)
	}
	if name, ok := (&PreviewService{db: db}).SandboxForRunNode("run-shared", "n1"); !ok || name != shared.Name {
		t.Fatalf("preview lookup = %q %v", name, ok)
	}
	if _, err := runNodeSandboxRow(db, "", "n1"); err == nil {
		t.Fatal("empty run id matched")
	}

	// A later node re-registering moves the row to it (one row per container).
	shared.NodeID = "n3"
	s.RegisterRunSandbox(shared)
	var n int64
	db.Model(&models.Sandbox{}).Where("run_id = ?", "run-shared").Count(&n)
	if n != 1 {
		t.Fatalf("shared rows = %d", n)
	}
}

func TestSettingsSharedRunSandboxKnob(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	config.StoreConfig(&config.Config{})
	svc := NewSettingsService(db, nil, nil)
	defer sandbox.SetSharedRunEnabled(false)
	if _, err := svc.Update(map[string]int{KeySharedRunSandbox: 1}); err != nil {
		t.Fatal(err)
	}
	if !sandbox.SharedRunEnabled() {
		t.Fatal("knob did not enable shared run sandboxes")
	}
	if _, err := svc.Update(map[string]int{KeySharedRunSandbox: 2}); err == nil {
		t.Fatal("knob accepted 2")
	}
	if _, err := svc.Update(map[string]int{KeySharedRunSandbox: 0}); err != nil || sandbox.SharedRunEnabled() {
		t.Fatalf("disable: err=%v enabled=%v", err, sandbox.SharedRunEnabled())
	}
	if boolInt(true) != 1 || boolInt(false) != 0 {
		t.Fatal("boolInt")
	}
}

func TestSaveRejectsUnknownSandboxMode(t *testing.T) {
	s := NewWorkflowService(newTestDB(t))
	wf := &models.WorkflowDef{ID: "wf-sbx", Name: "S", Graph: validGraph(),
		Defaults: &models.WorkflowDefaults{Sandbox: &models.WorkflowSandboxPolicy{Mode: "pooled"}}}
	if err := s.Save(wf); err == nil || !strings.Contains(err.Error(), "sandbox mode") {
		t.Fatalf("err = %v", err)
	}
	wf.Defaults.Sandbox.Mode = models.SandboxModeShared
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-sbx")
	if got.Defaults == nil || got.Defaults.Sandbox == nil || got.Defaults.Sandbox.Mode != models.SandboxModeShared {
		t.Fatalf("defaults = %+v", got.Defaults)
	}
}
