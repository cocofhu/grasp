package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TestPendingInboxTakeoverAsReview: a taken-over auto node parked at
// waiting_human shows in /api/pending-nodes as kind=review with takeover=true, and
// its state follows the review rule (replying while its session is busy).
func TestPendingInboxTakeoverAsReview(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	now := time.Now().UTC().Truncate(time.Second)
	graph := models.Graph{Nodes: []models.Node{{ID: "impl", Type: "agent", Label: "实现", Caps: testAutoCaps}}}
	db.Create(&models.Run{ID: "run-to", WorkflowID: "wf", WorkflowName: "WF", Status: "waiting_human", StartedAt: now, Graph: graph})
	db.Create(&models.StateRun{RunID: "run-to", NodeID: "impl", Iteration: 1, Status: "waiting_human", Mode: models.NodeModeManual})
	db.Create(&models.ReactConversation{RunID: "run-to", NodeID: "impl", Iteration: 1, Takeover: true,
		Messages: []models.ReactMessage{{Role: "agent", Text: "已中断", At: now.Format(time.RFC3339)}}})

	items := s.AllPendingInboxItems()
	if len(items) != 1 {
		t.Fatalf("want 1 inbox item, got %d", len(items))
	}
	it, ok := items[0].(ClarifyInboxItem)
	if !ok || it.Type != "clarify" || it.Kind != "review" || !it.Takeover || it.State != "" || it.Label != "实现" {
		t.Fatalf("takeover inbox item: %+v", items[0])
	}
	AttachInboxReplyingState(items, func(runID, nodeID string) bool { return runID == "run-to" && nodeID == "impl" })
	if got := items[0].(ClarifyInboxItem).State; got != "replying" {
		t.Fatalf("busy takeover session must be replying, got %q", got)
	}
	AttachInboxReplyingState(items, func(string, string) bool { return false })
	if got := items[0].(ClarifyInboxItem).State; got != "" {
		t.Fatalf("idle takeover session state=%q", got)
	}
}

// TestCurrentNodesKindAndMode: kind/mode classification for every current
// node shape; mode is auto only for a non-taken-over auto agent.
func TestCurrentNodesKindAndMode(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	review := &models.AgentCapabilities{Interaction: models.InteractionAuto, Review: true, Reads: []string{"*"}}
	clarify := &models.AgentCapabilities{Interaction: models.InteractionClarify, Reads: []string{"*"}}
	graph := models.Graph{Nodes: []models.Node{
		{ID: "auto", Type: "agent", Label: "自动", Caps: testAutoCaps},
		{ID: "rev", Type: "agent", Label: "复审", Caps: review},
		{ID: "cl", Type: "agent", Label: "澄清", Caps: clarify},
		{ID: "gate", Type: "human_gate", Label: "门禁"},
	}}
	mk := func(id, status string) models.Run {
		r := models.Run{ID: id, WorkflowID: "wf", Status: status, Graph: graph}
		db.Create(&r)
		return r
	}
	runs := []models.Run{
		mk("r-auto", "running"), mk("r-stopping", "running"), mk("r-takeover", "waiting_human"),
		mk("r-review", "waiting_human"), mk("r-review-auto", "running"), mk("r-clarify", "waiting_human"),
		mk("r-clarify-open", "running"), mk("r-gate", "waiting_human"),
		mk("r-moved", "waiting_human"), mk("r-older-run", "waiting_human"),
	}
	db.Create(&models.StateRun{RunID: "r-auto", NodeID: "auto", Iteration: 1, Status: "running"})
	db.Create(&models.StateRun{RunID: "r-stopping", NodeID: "auto", Iteration: 1, Status: "running", Mode: "manual"})
	db.Create(&models.StateRun{RunID: "r-takeover", NodeID: "auto", Iteration: 2, Status: "waiting_human", Mode: "manual"})
	db.Create(&models.StateRun{RunID: "r-review", NodeID: "rev", Iteration: 1, Status: "waiting_human"})
	db.Create(&models.StateRun{RunID: "r-review-auto", NodeID: "rev", Iteration: 1, Status: "running"})
	db.Create(&models.StateRun{RunID: "r-clarify", NodeID: "cl", Iteration: 1, Status: "waiting_human"})
	db.Create(&models.StateRun{RunID: "r-clarify-open", NodeID: "cl", Iteration: 1, Status: "running"})
	db.Create(&models.Gate{RunID: "r-gate", NodeID: "gate", Iteration: 1, Title: "门禁"})
	// Newer running visit wins over the clarify row the confirm just left.
	db.Create(&models.StateRun{RunID: "r-moved", NodeID: "cl", Iteration: 1, Status: "waiting_human"})
	db.Create(&models.StateRun{RunID: "r-moved", NodeID: "auto", Iteration: 1, Status: "running"})
	// An older running visit must not hide a later human wait.
	db.Create(&models.StateRun{RunID: "r-older-run", NodeID: "auto", Iteration: 1, Status: "running"})
	db.Create(&models.StateRun{RunID: "r-older-run", NodeID: "cl", Iteration: 1, Status: "waiting_human"})

	got := s.CurrentNodes(runs)
	want := map[string][3]string{ // kind, mode, state
		"r-auto":         {"agent", "auto", "running"},
		"r-stopping":     {"takeover", "manual", "stopping"},
		"r-takeover":     {"takeover", "manual", "waiting_human"},
		"r-review":       {"review", "manual", "waiting_human"},
		"r-review-auto":  {"agent", "auto", "running"},
		"r-clarify":      {"clarify", "manual", "waiting_human"},
		"r-clarify-open": {"clarify", "manual", "running"},
		"r-gate":         {"gate", "manual", "waiting_human"},
		"r-moved":        {"agent", "auto", "running"},
		"r-older-run":    {"clarify", "manual", "waiting_human"},
	}
	for id, w := range want {
		cn, ok := got[id]
		if !ok || cn.Kind != w[0] || cn.Mode != w[1] || cn.State != w[2] || cn.Name == "" {
			t.Fatalf("%s: got %+v (ok=%v), want kind/mode/state %v", id, cn, ok, w)
		}
	}
	if cn := got["r-moved"]; cn.ID != "auto" || cn.Name != "自动" {
		t.Fatalf("newer running visit must replace the clarify wait: %+v", cn)
	}
	if cn := got["r-older-run"]; cn.ID != "cl" || cn.Name != "澄清" {
		t.Fatalf("older running visit must not hide the later human wait: %+v", cn)
	}
}

// TestRunListOrderByLastActivity: the last_activity sort is whitelisted in
// both directions and falls back for legacy rows.
func TestRunListOrderByLastActivity(t *testing.T) {
	for dir, want := range map[string]string{"desc": "DESC", "asc": "ASC"} {
		got := runListOrderBy("last_activity", dir)
		if got != "COALESCE(last_activity_at, CASE WHEN status = 'queued' THEN created_at ELSE started_at END) "+want+", id DESC" {
			t.Fatalf("%s: %q", dir, got)
		}
	}
	if got := runListOrderBy("last_activity", "sideways"); got != defaultRunListOrder {
		t.Fatalf("bad order must fall back, got %q", got)
	}
}
