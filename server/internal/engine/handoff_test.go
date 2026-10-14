package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestHandoffTargetsLooksThroughPassThrough(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "impl", Type: "agent", Label: "实现"},
			{ID: "route", Type: "branch"},
			{ID: "test", Type: "agent", Label: "测试评审"},
			{ID: "gate", Type: "human_gate", Label: "人工审批"},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "impl", Target: "route"},
			{ID: "e2", Source: "route", Target: "test", SourceHandle: "a"},
			{ID: "e3", Source: "route", Target: "output", SourceHandle: "b"},
			{ID: "e4", Source: "impl", Target: "gate", Kind: models.EdgeFailure},
			{ID: "e5", Source: "test", Target: "output"},
		},
	}
	got := handoffTargets(g, g.FindNode("impl"))
	if len(got) != 2 {
		t.Fatalf("targets = %+v", got)
	}
	if got[0].NodeID != "test" || got[0].Outlet != "success" || got[0].Label != "测试评审" {
		t.Errorf("through branch = %+v", got[0])
	}
	if got[1].NodeID != "gate" || got[1].Outlet != "failure" {
		t.Errorf("failure edge = %+v", got[1])
	}
	if out := handoffTargets(g, g.FindNode("test")); len(out) != 0 {
		t.Errorf("output sink is not a handoff consumer: %+v", out)
	}
}

func handoffGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "impl", Type: "agent", Label: "实现", Caps: capsPlain, Config: map[string]any{"prompt": "x", "produces": "out.md"}},
			{ID: "test", Type: "agent", Label: "测试评审", Caps: capsPlain, Config: map[string]any{"prompt": "y", "produces": "t.md"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "impl"},
			{ID: "e2", Source: "impl", Target: "test", Kind: models.EdgeSuccess},
			{ID: "ef", Source: "impl", Target: "test", Kind: models.EdgeFailure},
			{ID: "e3", Source: "test", Target: "output"},
		},
	}
}

// The agent's node_complete handoff lands on outputs and reaches the next node.
func TestHandoffFlowsToDownstream(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, handoffGraph())
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").First(&sr)
	if sr.Outputs[models.OutputHandoff] != "fake handoff from impl" || sr.Outputs[models.OutputHandoffSource] != models.HandoffSourceAgent {
		t.Fatalf("impl outputs = %+v", sr.Outputs)
	}
	up := p.seenUpstream["test"]
	if up == nil || up.NodeID != "impl" || up.Text != "fake handoff from impl" || up.Label != "实现" {
		t.Fatalf("test node upstream handoff = %+v", up)
	}
	if p.seenUpstream["impl"] != nil {
		t.Errorf("first agent has no upstream handoff: %+v", p.seenUpstream["impl"])
	}
}

// A node that dies before node_complete still hands off: the platform writes
// one from the failure and the fail edge carries it.
func TestHandoffPlatformFallbackOnFailure(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, handoffGraph())
	p.failLeft = map[string]int{"impl": 99}
	p.reason = "sandbox exploded"
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").Order("iteration desc").First(&sr)
	h, _ := sr.Outputs[models.OutputHandoff].(string)
	if sr.Status != "failed" || !strings.Contains(h, "sandbox exploded") || sr.Outputs[models.OutputHandoffSource] != models.HandoffSourcePlatform {
		t.Fatalf("failed impl: status=%s outputs=%+v", sr.Status, sr.Outputs)
	}
	up := p.seenUpstream["test"]
	if up == nil || up.Source != models.HandoffSourcePlatform || up.Status != "failed" {
		t.Fatalf("test node upstream handoff = %+v", up)
	}
}
