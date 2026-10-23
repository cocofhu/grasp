package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestRunSandboxMode(t *testing.T) {
	defer sandbox.SetSharedRunEnabled(false)
	shared := models.WorkflowDef{Defaults: &models.WorkflowDefaults{
		Sandbox: &models.WorkflowSandboxPolicy{Mode: models.SandboxModeShared}}}
	perNode := models.WorkflowDef{Defaults: &models.WorkflowDefaults{
		Sandbox: &models.WorkflowSandboxPolicy{Mode: models.SandboxModePerNode}}}

	sandbox.SetSharedRunEnabled(false)
	if got := runSandboxMode(shared); got != "" {
		t.Fatalf("platform off: %q", got)
	}
	sandbox.SetSharedRunEnabled(true)
	if got := runSandboxMode(shared); got != models.SandboxModeShared {
		t.Fatalf("shared: %q", got)
	}
	for _, def := range []models.WorkflowDef{perNode, {}, {Defaults: &models.WorkflowDefaults{}}} {
		if got := runSandboxMode(def); got != "" {
			t.Fatalf("per-node %+v: %q", def.Defaults, got)
		}
	}
}

func TestStartRunSnapshotsSandboxMode(t *testing.T) {
	defer sandbox.SetSharedRunEnabled(false)
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "agent", Type: "agent", Caps: capsPlain, Config: map[string]any{"agent_profile": "r", "prompt": "go"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "agent"},
			{ID: "e2", Source: "agent", Target: "output"},
		},
	}
	eng, db := setupEngineGraph(t, g)
	var wf models.WorkflowDef
	db.First(&wf, "id = ?", "wf")
	wf.Defaults = &models.WorkflowDefaults{Sandbox: &models.WorkflowSandboxPolicy{Mode: models.SandboxModeShared}}
	if err := db.Save(&wf).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		enabled bool
		want    string
	}{{false, ""}, {true, models.SandboxModeShared}} {
		sandbox.SetSharedRunEnabled(tc.enabled)
		run, err := eng.StartRun("wf", map[string]any{}, "test")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		var stored models.Run
		db.First(&stored, "id = ?", run.ID)
		if stored.SandboxMode != tc.want {
			t.Fatalf("enabled=%v: stored mode %q, want %q", tc.enabled, stored.SandboxMode, tc.want)
		}
		c, err := eng.loadCtx(run.ID)
		if err != nil {
			t.Fatalf("loadCtx: %v", err)
		}
		node := g.Nodes[1]
		if got := eng.nodeReq(c, &node).SandboxMode; got != tc.want {
			t.Fatalf("enabled=%v: NodeReq mode %q, want %q", tc.enabled, got, tc.want)
		}
	}
}
