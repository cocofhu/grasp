package services

import (
	"strings"
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

func TestWorkflowSandboxModeTogglePersistsWithoutChangingGraph(t *testing.T) {
	s := NewWorkflowService(newTestDB(t))
	wf := &models.WorkflowDef{ID: "wf-toggle", Name: "Toggle", Graph: validGraph(),
		Defaults: &models.WorkflowDefaults{InheritWorkspace: true,
			Variables: []models.WorkspaceVariable{{Name: "REGION", Value: "east"}},
			Sandbox:   &models.WorkflowSandboxPolicy{Mode: models.SandboxModeShared}}}
	for _, mode := range []string{models.SandboxModeShared, models.SandboxModePerNode, models.SandboxModeShared} {
		wf.Defaults.Sandbox.Mode = mode
		if err := s.Save(wf); err != nil {
			t.Fatal(err)
		}
		got, ok := s.Get(wf.ID)
		if !ok || got.Defaults == nil || got.Defaults.Sandbox == nil || got.Defaults.Sandbox.Mode != mode {
			t.Fatalf("mode %q did not survive save/reload: %+v", mode, got.Defaults)
		}
		if got.Version != 1 || !GraphsEqual(got.Graph, wf.Graph) || !got.Defaults.InheritWorkspace || len(got.Defaults.Variables) != 1 || got.Defaults.Variables[0].Value != "east" {
			t.Fatalf("mode %q unexpectedly changed workflow: %+v", mode, got)
		}
	}
}
