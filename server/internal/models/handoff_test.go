package models

import "testing"

func TestHandoffTargetsLooksThroughPassThrough(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "impl", Type: "agent", Label: "实现"},
			{ID: "route", Type: "branch"},
			{ID: "test", Type: "agent", Label: "测试评审"},
			{ID: "gate", Type: "human_gate", Label: "人工审批"},
			{ID: "output", Type: "output"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "impl", Target: "route"},
			{ID: "e2", Source: "route", Target: "test", SourceHandle: "a"},
			{ID: "e3", Source: "route", Target: "output", SourceHandle: "b"},
			{ID: "e4", Source: "impl", Target: "gate", Kind: EdgeFailure},
			{ID: "e5", Source: "test", Target: "output"},
		},
	}
	got := g.HandoffTargets(g.FindNode("impl"))
	if len(got) != 2 {
		t.Fatalf("targets = %+v", got)
	}
	if got[0].NodeID != "test" || got[0].Outlet != "success" || got[0].Label != "测试评审" {
		t.Errorf("through branch = %+v", got[0])
	}
	if got[1].NodeID != "gate" || got[1].Outlet != "failure" {
		t.Errorf("failure edge = %+v", got[1])
	}
	if out := g.HandoffTargets(g.FindNode("test")); len(out) != 0 {
		t.Errorf("output sink is not a handoff consumer: %+v", out)
	}
}

func TestHandoffTargetsPassThroughGate(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "impl", Type: "agent"},
			{ID: "gate", Type: "human_gate"},
			{ID: "test", Type: "agent"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "impl", Target: "gate"},
			{ID: "e2", Source: "gate", SourceHandle: "approve", Target: "test"},
			{ID: "e3", Source: "gate", SourceHandle: "reject", Target: "impl"},
		},
	}
	got := g.HandoffTargets(g.FindNode("impl"))
	if len(got) != 2 || got[0].NodeID != "gate" || got[1].NodeID != "test" || got[1].Outlet != "success" {
		t.Fatalf("targets = %+v", got)
	}
}

func TestHandoffTargetsNilNode(t *testing.T) {
	if got := (Graph{}).HandoffTargets(nil); got != nil {
		t.Fatalf("nil node targets = %+v", got)
	}
}
