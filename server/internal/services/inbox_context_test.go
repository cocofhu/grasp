package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestInboxContextKind(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	now := time.Now().UTC()

	db.Create(&models.Run{
		ID: "run-gate", WorkflowID: "wf1", Status: "waiting_human",
		StartedAt: now, Graph: validGraph(),
	})
	db.Create(&models.Gate{
		RunID: "run-gate", NodeID: "gate-approve", Iteration: 2,
		Resolved: false, RequestedAt: now,
	})
	db.Create(&models.StateRun{
		RunID: "run-gate", NodeID: "gate-approve", Iteration: 2, Status: "waiting_human",
	})

	kind, ok := s.InboxContextKind("run-gate", "gate-approve", 2)
	if !ok || kind != "gate" {
		t.Fatalf("gate pending: got %q %v", kind, ok)
	}
	if kind, ok := s.InboxContextKind("run-gate", "gate-approve", 1); ok {
		t.Fatalf("wrong iteration should not match: %q", kind)
	}
	if _, ok := s.InboxContextKind("run-gate", "missing", 2); ok {
		t.Fatal("unknown node should not match")
	}
}

func TestInboxContextKindClarify(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	now := time.Now().UTC()

	db.Create(&models.Run{
		ID: "run-clarify", WorkflowID: "wf2", Status: "waiting_human",
		StartedAt: now, Graph: reactGraph(),
	})
	db.Create(&models.ReactConversation{
		RunID: "run-clarify", NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "hi", At: now.Format(time.RFC3339)}},
	})
	db.Create(&models.StateRun{RunID: "run-clarify", NodeID: "react", Iteration: 1, Status: "waiting_human"})

	kind, ok := s.InboxContextKind("run-clarify", "react", 1)
	if !ok || kind != "clarify" {
		t.Fatalf("clarify pending: got %q %v", kind, ok)
	}
}

func TestGateUpstreamNodeIDs(t *testing.T) {
	gate := &models.Node{
		ID: "gate", Type: "human_gate",
		Config: map[string]any{"body_template": "see {{nodes.visual.outputs.page}}"},
	}
	ids := GateUpstreamNodeIDs(gate)
	if len(ids) != 1 || ids[0] != "visual" {
		t.Fatalf("body_template refs: %v", ids)
	}
}

func TestClarifySlimNodeIDs(t *testing.T) {
	// research post-run review: no template refs → current node only
	research := &models.Node{ID: "research_1", Type: "agent", Caps: testReviewCaps, Config: map[string]any{}}
	ids := ClarifySlimNodeIDs(research, "research_1", nil)
	if len(ids) != 1 || ids[0] != "research_1" {
		t.Fatalf("research only current: %v", ids)
	}

	// react with upstream refs in prompt/config
	react := &models.Node{
		ID: "react", Type: "agent", Caps: testClarifyCaps,
		Config: map[string]any{
			"prompt": `see {{nodes.research.outputs.research}} and artifact("plan.json")`,
		},
	}
	arts := []models.Artifact{{Name: "plan.json", NodeID: "plan"}}
	ids = ClarifySlimNodeIDs(react, "react", arts)
	want := map[string]bool{"react": true, "research": true, "plan": true}
	if len(ids) != 3 {
		t.Fatalf("want 3 ids, got %v", ids)
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("unexpected id %q in %v", id, ids)
		}
	}

	// nil node still includes current
	ids = ClarifySlimNodeIDs(nil, "orphan", nil)
	if len(ids) != 1 || ids[0] != "orphan" {
		t.Fatalf("nil node: %v", ids)
	}
}

func TestSlimNodeExecutions(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	db.Create(&models.StateRun{
		RunID: "r1", NodeID: "up", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"page": "<html/>"},
		Events:  []models.AcpEvent{{Kind: "message", Text: "big"}},
	})
	db.Create(&models.StateRun{
		RunID: "r1", NodeID: "up", Iteration: 2, Status: "failed",
		Outputs: map[string]any{"page": "<html v2/>"},
	})

	execs := s.SlimNodeExecutions("r1", []string{"up"})
	list := execs["up"]
	if len(list) != 2 {
		t.Fatalf("want 2 executions, got %d", len(list))
	}
	if _, hasEvents := list[0]["events"]; hasEvents {
		t.Fatal("slim execution must not include events")
	}
	if list[0]["status"] != "completed" {
		t.Fatalf("slim must include status, got %v", list[0]["status"])
	}
	if list[1]["status"] != "failed" {
		t.Fatalf("iter 2 status: %v", list[1]["status"])
	}
	if list[1]["outputs"].(map[string]any)["page"] != "<html v2/>" {
		t.Fatal("iteration 2 outputs missing")
	}
}

func TestSlimNodeExecutionsOmitsLargeJSONSnapshots(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	db.Create(&models.StateRun{
		RunID: "r-slim-json", NodeID: "up", Iteration: 1, Status: "completed",
		Outputs: map[string]any{
			"page":                       "<html/>",
			"clarified_requirement":      "md",
			"clarified_requirement_json": `{"title":"big"}`,
			"research_json":              `{"summary":"x"}`,
		},
	})
	execs := s.SlimNodeExecutions("r-slim-json", []string{"up"})
	outs := execs["up"][0]["outputs"].(map[string]any)
	if _, ok := outs["clarified_requirement_json"]; ok {
		t.Fatal("*_json snapshots must be omitted from SlimNodeExecutions")
	}
	if _, ok := outs["research_json"]; ok {
		t.Fatal("research_json must be omitted")
	}
	if outs["page"] != "<html/>" {
		t.Fatalf("page snapshot should remain: %+v", outs)
	}
	if outs["clarified_requirement"] != "md" {
		t.Fatalf("rendered markdown key should remain: %+v", outs)
	}
}

func TestOmitLargeJSONSnapshots(t *testing.T) {
	got := OmitLargeJSONSnapshots(map[string]any{
		"page": "<html/>", "plan_json": `{}`, "plan": "md",
	})
	if _, ok := got["plan_json"]; ok {
		t.Fatal("plan_json should be omitted")
	}
	if got["page"] != "<html/>" || got["plan"] != "md" {
		t.Fatalf("got %+v", got)
	}
	if OmitLargeJSONSnapshots(nil) != nil {
		t.Fatal("nil in → nil out")
	}
}
