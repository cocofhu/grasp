package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func approveGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "Start"},
			agentNode("ap", "开发前澄清", testClarifyCaps),
			{ID: "out", Type: "output", Label: "End"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "in", Target: "ap"},
			{ID: "e2", Source: "ap", Target: "out"},
		},
	}
}

// A booting approve node has no conversation yet; it must still be listed so an
// approval appears the moment the run starts.
func TestStartingClarifyListedAsStarting(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	now := time.Now().UTC().Truncate(time.Second)
	started := now.Add(-5 * time.Second)

	db.Create(&models.Run{
		ID: "run-start", WorkflowID: "wf-ap", WorkflowName: "自我迭代",
		Title: "把登录做清楚", Status: "running", StartedAt: now.Add(-time.Minute), Graph: approveGraph(),
	})
	db.Create(&models.StateRun{
		RunID: "run-start", NodeID: "ap", NodeType: "agent", Iteration: 1,
		Status: "running", StartedAt: &started,
	})

	items := s.AllPendingInboxItems()
	if len(items) != 1 {
		t.Fatalf("expected 1 starting item, got %d", len(items))
	}
	it, ok := items[0].(ClarifyInboxItem)
	if !ok {
		t.Fatalf("expected ClarifyInboxItem, got %T", items[0])
	}
	if it.State != "starting" {
		t.Fatalf("state = %q, want starting", it.State)
	}
	if it.Type != "clarify" || it.Kind != "clarify" {
		t.Fatalf("type/kind = %q/%q", it.Type, it.Kind)
	}
	if it.NodeID != "ap" || it.Iteration != 1 {
		t.Fatalf("node/iteration = %s/%d", it.NodeID, it.Iteration)
	}
	if it.Label != "开发前澄清" {
		t.Fatalf("label = %q", it.Label)
	}
	if it.RunTitle != "把登录做清楚" {
		t.Fatalf("runTitle = %q", it.RunTitle)
	}
	if !s.IsStartingClarify("run-start", "ap", 1) {
		t.Fatal("IsStartingClarify should be true while booting")
	}
	if kind, ok := s.InboxContextKind("run-start", "ap", 1); !ok || kind != InboxKindClarifyStarting {
		t.Fatalf("InboxContextKind = %q,%v, want %q,true", kind, ok, InboxKindClarifyStarting)
	}
}

// Once the sandbox parks, the same node must appear exactly once and without
// the starting flag (the parked clarify path owns it).
func TestStartingClarifyBecomesParkedWithoutDuplicate(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	now := time.Now().UTC().Truncate(time.Second)

	db.Create(&models.Run{
		ID: "run-parked", WorkflowID: "wf-ap", WorkflowName: "自我迭代",
		Status: "waiting_human", StartedAt: now, Graph: approveGraph(),
	})
	db.Create(&models.StateRun{
		RunID: "run-parked", NodeID: "ap", NodeType: "agent", Iteration: 1, Status: "waiting_human",
	})
	db.Create(&models.ReactConversation{
		RunID: "run-parked", NodeID: "ap", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{},
	})

	items := s.AllPendingInboxItems()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0].(ClarifyInboxItem)
	if it.State != "" {
		t.Fatalf("parked item state = %q, want empty", it.State)
	}
	if s.IsStartingClarify("run-parked", "ap", 1) {
		t.Fatal("a parked approve is not starting")
	}
}

// Failed / terminal runs and non-approve running nodes stay out of the inbox.
func TestStartingClarifyExcludesFailedAndAutoAgents(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	now := time.Now().UTC().Truncate(time.Second)

	db.Create(&models.Run{ID: "run-failed", Status: "failed", StartedAt: now, Graph: approveGraph()})
	db.Create(&models.StateRun{
		RunID: "run-failed", NodeID: "ap", NodeType: "agent", Iteration: 1, Status: "running",
	})

	db.Create(&models.Run{ID: "run-boot-failed", Status: "running", StartedAt: now, Graph: approveGraph()})
	db.Create(&models.StateRun{
		RunID: "run-boot-failed", NodeID: "ap", NodeType: "agent", Iteration: 1, Status: "failed",
		Error: "sandbox setup failed",
	})

	autoGraph := models.Graph{Nodes: []models.Node{agentNode("impl", "实现", testAutoCaps)}}
	db.Create(&models.Run{ID: "run-agent", Status: "running", StartedAt: now, Graph: autoGraph})
	db.Create(&models.StateRun{
		RunID: "run-agent", NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "running",
	})

	if items := s.AllPendingInboxItems(); len(items) != 0 {
		t.Fatalf("expected no inbox items, got %d: %+v", len(items), items)
	}
	if s.IsStartingClarify("run-failed", "ap", 1) {
		t.Error("terminal run must not report starting")
	}
	if s.IsStartingClarify("run-boot-failed", "ap", 1) {
		t.Error("failed node must not report starting")
	}
	if s.IsStartingClarify("run-agent", "impl", 1) {
		t.Error("auto Agent must not report starting")
	}
}

func TestStartingClarifyRespectsWorkflowFilter(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	now := time.Now().UTC().Truncate(time.Second)

	db.Create(&models.WorkflowDef{ID: "wf-a", Name: "A", ProjectID: "proj-1"})
	db.Create(&models.WorkflowDef{ID: "wf-b", Name: "B", ProjectID: "proj-2"})
	db.Create(&models.Run{ID: "run-a", WorkflowID: "wf-a", Status: "running", StartedAt: now, Graph: approveGraph()})
	db.Create(&models.StateRun{RunID: "run-a", NodeID: "ap", NodeType: "agent", Iteration: 1, Status: "running"})
	db.Create(&models.Run{ID: "run-b", WorkflowID: "wf-b", Status: "running", StartedAt: now, Graph: approveGraph()})
	db.Create(&models.StateRun{RunID: "run-b", NodeID: "ap", NodeType: "agent", Iteration: 1, Status: "running"})

	items, total := s.PendingInboxItems("wf-a", "", nil, 0, 0)
	if total != 1 || len(items) != 1 {
		t.Fatalf("workflow filter total=%d len=%d", total, len(items))
	}
	if items[0].(ClarifyInboxItem).RunID != "run-a" {
		t.Fatalf("workflow filter returned %+v", items[0])
	}

	items, total = s.PendingInboxItems("", "proj-2", nil, 0, 0)
	if total != 1 || len(items) != 1 {
		t.Fatalf("project filter total=%d len=%d", total, len(items))
	}
	if items[0].(ClarifyInboxItem).RunID != "run-b" {
		t.Fatalf("project filter returned %+v", items[0])
	}
}
