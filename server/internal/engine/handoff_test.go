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

// handoffReviewGraph: input → impl (review on) → test (downstream) → output.
// impl is a review node (caps.Review=true), so confirm goes through the review
// path that #796 left uncovered.
func handoffReviewGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "impl", Type: "agent", Label: "实现", Caps: capsResearch, Config: map[string]any{"prompt": "x"}},
			{ID: "test", Type: "agent", Label: "测试评审", Caps: capsPlain, Config: map[string]any{"prompt": "y", "produces": "t.md"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "impl"},
			{ID: "e2", Source: "impl", Target: "test", Kind: models.EdgeSuccess},
			{ID: "e3", Source: "test", Target: "output"},
		},
	}
}

// review handoff: a review:true node completes node_complete with a handoff,
// parks for review, then confirm must KEEP that handoff on the latest StateRun
// (the review rebuild previously overwrote it) and the downstream node must
// receive it.
func TestReviewConfirmRetainsAgentHandoff(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, handoffReviewGraph())
	run, _ := eng.StartRun("wf", nil, "test")
	waitReactPause(t, db, run.ID, "impl")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("confirm review: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").Order("iteration desc, id desc").First(&sr)
	if sr.Outputs[models.OutputHandoff] != "fake handoff from impl" ||
		sr.Outputs[models.OutputHandoffSource] != models.HandoffSourceAgent {
		t.Fatalf("impl outputs after review confirm = %+v", sr.Outputs)
	}
	up := p.seenUpstream["test"]
	if up == nil || up.NodeID != "impl" || up.Text != "fake handoff from impl" ||
		up.Source != models.HandoffSourceAgent {
		t.Fatalf("test node upstream handoff = %+v", up)
	}
}

// review platform fallback: confirmation still has no handoff but the node has a
// downstream consumer, so the platform writes one (source=platform) — matching
// execute.go's ensureHandoff on the main path.
func TestReviewConfirmPlatformFallbackHandoff(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, handoffReviewGraph())
	run, _ := eng.StartRun("wf", nil, "test")
	waitReactPause(t, db, run.ID, "impl")
	waitRunStatus(t, db, run.ID, "waiting_human")

	// Simulate a pause row that carries no handoff (e.g. agent never handed off).
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").Order("iteration desc, id desc").First(&sr)
	delete(sr.Outputs, models.OutputHandoff)
	delete(sr.Outputs, models.OutputHandoffSource)
	if err := db.Save(&sr).Error; err != nil {
		t.Fatalf("clear handoff: %v", err)
	}

	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("confirm review: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").Order("iteration desc, id desc").First(&sr)
	h, _ := sr.Outputs[models.OutputHandoff].(string)
	if h == "" || sr.Outputs[models.OutputHandoffSource] != models.HandoffSourcePlatform {
		t.Fatalf("platform fallback handoff missing: %+v", sr.Outputs)
	}
	up := p.seenUpstream["test"]
	if up == nil || up.Source != models.HandoffSourcePlatform {
		t.Fatalf("test node upstream handoff = %+v", up)
	}
}

// A review node with only an output sink is not forced to hand off: no agent
// downstream, no platform fallback written.
func TestReviewConfirmNoDownstreamNoHandoff(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "impl", Type: "agent", Label: "实现", Caps: capsResearch, Config: map[string]any{"prompt": "x"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "impl"},
			{ID: "e2", Source: "impl", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitReactPause(t, db, run.ID, "impl")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("confirm review: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").Order("iteration desc, id desc").First(&sr)
	if h, _ := sr.Outputs[models.OutputHandoff].(string); h != "" {
		t.Fatalf("sink-only review node must not carry a handoff: %+v", sr.Outputs)
	}
}

// Loop-back (test fails → impl again): every review confirm keeps the handoff,
// and downstream reads the latest iteration's value.
func TestReviewConfirmRetainsHandoffOnLoopBack(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "impl", Type: "agent", Label: "实现", Caps: capsResearch, Config: map[string]any{"prompt": "x"}},
			{ID: "test", Type: "agent", Label: "测试评审", Caps: capsTest, Config: map[string]any{"prompt": "y"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "impl"},
			{ID: "e2", Source: "impl", Target: "test", Kind: models.EdgeSuccess},
			{ID: "e3", Source: "test", Target: "output", SourceHandle: "pass", Kind: models.EdgeSuccess},
			{ID: "e4", Source: "test", Target: "impl", SourceHandle: "fail", Kind: models.EdgeFailure},
		},
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.structuredBodySeq = map[string][]string{
		"test": {
			`{"summary":"failed","passed":0,"failed":1,"skipped":0}`,
			`{"summary":"passed","passed":2,"failed":0,"skipped":0}`,
		},
	}
	run, _ := eng.StartRun("wf", nil, "test")

	// First confirm → test fails → loops back to impl.
	waitReactPause(t, db, run.ID, "impl")
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	waitReactPause(t, db, run.ID, "impl") // iteration 2 opens
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("second confirm: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var count int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "impl").Count(&count)
	if count < 2 {
		t.Fatalf("expected a second impl iteration, got %d", count)
	}
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").Order("iteration desc, id desc").First(&sr)
	if sr.Iteration < 2 || sr.Outputs[models.OutputHandoff] != "fake handoff from impl" ||
		sr.Outputs[models.OutputHandoffSource] != models.HandoffSourceAgent {
		t.Fatalf("loop-back impl outputs = iter %d %+v", sr.Iteration, sr.Outputs)
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
