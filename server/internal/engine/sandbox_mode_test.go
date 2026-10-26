package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRunSandboxMode(t *testing.T) {
	shared := models.WorkflowDef{Defaults: &models.WorkflowDefaults{
		Sandbox: &models.WorkflowSandboxPolicy{Mode: models.SandboxModeShared}}}
	perNode := models.WorkflowDef{Defaults: &models.WorkflowDefaults{
		Sandbox: &models.WorkflowSandboxPolicy{Mode: models.SandboxModePerNode}}}

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
	cases := []struct {
		mode string
		want string
	}{{models.SandboxModeShared, models.SandboxModeShared}, {models.SandboxModePerNode, ""}, {"", ""}}
	var runs []*models.Run
	for _, tc := range cases {
		wf.Defaults = &models.WorkflowDefaults{Sandbox: &models.WorkflowSandboxPolicy{Mode: tc.mode}}
		if err := db.Save(&wf).Error; err != nil {
			t.Fatal(err)
		}
		run, err := eng.StartRun("wf", map[string]any{}, "test")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		runs = append(runs, run)
	}

	// Later workflow edits must not change the mode of an existing run, even
	// when its execution context is loaded again after the change.
	for i, run := range runs {
		var stored models.Run
		if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.SandboxMode != cases[i].want {
			t.Fatalf("workflow mode=%q: stored mode %q, want %q", cases[i].mode, stored.SandboxMode, cases[i].want)
		}
		c, err := eng.loadCtx(run.ID)
		if err != nil {
			t.Fatalf("loadCtx: %v", err)
		}
		node := g.Nodes[1]
		if got := eng.nodeReq(c, &node).SandboxMode; got != cases[i].want {
			t.Fatalf("workflow mode=%q: NodeReq mode %q, want %q", cases[i].mode, got, cases[i].want)
		}
	}
}
