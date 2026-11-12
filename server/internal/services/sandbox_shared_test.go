package services

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
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

func TestSaveAcceptsLegacySandboxModeDefaults(t *testing.T) {
	s := NewWorkflowService(newTestDB(t))
	var defaults models.WorkflowDefaults
	if err := json.Unmarshal([]byte(`{"inheritWorkspace":true,"variables":[],"sandbox":{"mode":"per_node"}}`), &defaults); err != nil {
		t.Fatal(err)
	}
	wf := &models.WorkflowDef{ID: "wf-legacy", Name: "Legacy", Graph: validGraph(), Defaults: &defaults}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(wf.ID)
	if !ok || got.Defaults == nil || !got.Defaults.InheritWorkspace {
		t.Fatalf("defaults = %+v", got.Defaults)
	}
}
