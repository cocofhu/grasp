package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestResolveGateUpstreamPointer(t *testing.T) {
	eng, db := setupEngine(t)
	run := &models.Run{ID: "run-ptr", WorkflowID: "w", WorkflowName: "w", Status: "running"}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "visual", Iteration: 1, Status: "completed"})
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "visual", Iteration: 2, Status: "failed"})
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "visual", Iteration: 3, Status: "completed"})

	c := &execCtx{run: run, iter: map[string]int{"visual": 3, "gate": 2}}

	t.Run("page preferred and latest completed", func(t *testing.T) {
		node := &models.Node{ID: "gate", Type: "human_gate", Config: map[string]any{
			"body_template": "{{nodes.research.outputs.research}} {{nodes.visual.outputs.page}}",
		}}
		id, iter := eng.resolveGateUpstreamPointer(c, node)
		if id != "visual" || iter != 3 {
			t.Fatalf("want visual@3, got %s@%d", id, iter)
		}
	})

	t.Run("no page falls back to first upstream", func(t *testing.T) {
		db.Create(&models.StateRun{RunID: run.ID, NodeID: "plan", Iteration: 1, Status: "completed"})
		node := &models.Node{ID: "gate", Type: "human_gate", Config: map[string]any{
			"body_template": "{{nodes.plan.outputs.plan}}",
		}}
		id, iter := eng.resolveGateUpstreamPointer(c, node)
		if id != "plan" || iter != 1 {
			t.Fatalf("want plan@1, got %s@%d", id, iter)
		}
	})

	t.Run("no refs leaves pointer empty", func(t *testing.T) {
		node := &models.Node{ID: "gate", Type: "human_gate", Config: map[string]any{
			"body_template": "nothing here",
		}}
		id, iter := eng.resolveGateUpstreamPointer(c, node)
		if id != "" || iter != 0 {
			t.Fatalf("want empty, got %s@%d", id, iter)
		}
	})

	t.Run("falls back to c.iter when no completed", func(t *testing.T) {
		c2 := &execCtx{run: run, iter: map[string]int{"orphan": 4}}
		node := &models.Node{ID: "gate", Type: "human_gate", Config: map[string]any{
			"body_template": "{{nodes.orphan.outputs.page}}",
		}}
		id, iter := eng.resolveGateUpstreamPointer(c2, node)
		if id != "orphan" || iter != 4 {
			t.Fatalf("want orphan@4 via c.iter, got %s@%d", id, iter)
		}
	})
}

func TestExecGatePersistsUpstreamPointer(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "visual", Type: "agent", Caps: capsPageAuto},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":         "审阅视觉",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions":       []any{map[string]any{"id": "approve", "label": "通过"}},
			}},
			{ID: "output", Type: "output", Config: map[string]any{"result": "ok"}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "visual"},
			{ID: "e2", Source: "visual", Target: "gate", Kind: models.EdgeSuccess},
			{ID: "e3", Source: "gate", Target: "output", SourceHandle: "approve", Kind: models.EdgeSuccess},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")

	var gate models.Gate
	if err := db.Where("run_id = ? AND node_id = ? AND resolved = ?", run.ID, "gate", false).First(&gate).Error; err != nil {
		t.Fatal(err)
	}
	if gate.UpstreamNodeID != "visual" {
		t.Fatalf("upstreamNodeId: got %q", gate.UpstreamNodeID)
	}
	if gate.UpstreamIteration < 1 {
		t.Fatalf("upstreamIteration should be set, got %d", gate.UpstreamIteration)
	}
	// Visual completed once before the gate opened.
	if gate.UpstreamIteration != 1 {
		t.Fatalf("want upstreamIteration=1 for first success, got %d", gate.UpstreamIteration)
	}
}
