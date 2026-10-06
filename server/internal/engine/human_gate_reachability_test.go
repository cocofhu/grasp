package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestHasRemainingHumanGate_EdgeOnly(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain},
			{ID: "gate", Type: "human_gate"},
			{ID: "out", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "in", Target: "work"},
			{ID: "e2", Source: "work", Target: "gate"},
			{ID: "e3", Source: "gate", Target: "out"},
		},
	}
	if !hasRemainingHumanGate(&g, "in") {
		t.Fatal("want true via OutEdges from in")
	}
	if hasRemainingHumanGate(&g, "out") {
		t.Fatal("want false from out")
	}
}

func TestHasRemainingHumanGate_CycleTerminates(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "a", Type: "agent", Caps: capsPlain},
			{ID: "b", Type: "agent", Caps: capsPlain},
			{ID: "c", Type: "agent", Caps: capsPlain},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "c"},
			{ID: "e3", Source: "c", Target: "a"},
		},
	}
	if hasRemainingHumanGate(&g, "a") {
		t.Fatal("cycle with no human_gate must return false and terminate")
	}
}

func TestHasRemainingHumanGate_StartIsGate(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "gate", Type: "human_gate"},
			{ID: "out", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "gate", Target: "out"},
		},
	}
	if !hasRemainingHumanGate(&g, "gate") {
		t.Fatal("start node that is human_gate must be true")
	}
}

func TestHasRemainingHumanGate_AnyBranchReachable(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input"},
			{ID: "br", Type: "branch", Config: map[string]any{
				"cases": []any{
					map[string]any{"when": "x", "id": "c1"},
					map[string]any{"when": "y", "id": "c2"},
				},
			}},
			{ID: "auto", Type: "agent", Caps: capsPlain},
			{ID: "gate", Type: "human_gate"},
			{ID: "out", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "in", Target: "br"},
			{ID: "e1a", Source: "br", Target: "auto", SourceHandle: "c1"},
			{ID: "e1b", Source: "br", Target: "gate", SourceHandle: "c2"},
			{ID: "e2", Source: "auto", Target: "out"},
			{ID: "e3", Source: "gate", Target: "out"},
		},
	}
	if !hasRemainingHumanGate(&g, "in") {
		t.Fatal("any reachable branch to human_gate must count as remaining")
	}
}

func TestHasRemainingHumanGate_BadGraphFalse(t *testing.T) {
	if hasRemainingHumanGate(nil, "in") {
		t.Fatal("nil graph → false")
	}
	empty := models.Graph{}
	if hasRemainingHumanGate(&empty, "in") {
		t.Fatal("empty graph missing from → false")
	}
	g := models.Graph{Nodes: []models.Node{{ID: "a", Type: "agent", Caps: capsPlain}}}
	if hasRemainingHumanGate(&g, "") {
		t.Fatal("empty from → false")
	}
	if hasRemainingHumanGate(&g, "missing") {
		t.Fatal("missing from node → false")
	}
}

func TestHasRemainingHumanGate_ReactNotGate(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input"},
			{ID: "react", Type: "agent", Caps: capsClarify},
			{ID: "out", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "in", Target: "react"},
			{ID: "e2", Source: "react", Target: "out"},
		},
	}
	if hasRemainingHumanGate(&g, "in") {
		t.Fatal("react must not count as human_gate")
	}
}

func TestContinueFromNodeID(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain},
		},
		Edges: []models.Edge{{ID: "e1", Source: "in", Target: "work"}},
	}
	run := models.Run{Graph: g}
	from, ok := continueFromNodeID(run)
	if !ok || from != "in" {
		t.Fatalf("StartNode continue = %q ok=%v, want in true", from, ok)
	}
	run.Checkpoints = map[string]map[string]any{
		admissionFromCheckpoint: {"node": "work"},
	}
	from, ok = continueFromNodeID(run)
	if !ok || from != "work" {
		t.Fatalf("admission-from continue = %q ok=%v, want work true", from, ok)
	}
}
