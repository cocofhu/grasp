package engine

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

func reattachGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain, Config: map[string]any{"agent_profile": "dev-agent", "prompt": "work"}},
			{ID: "ask", Type: "agent", Caps: capsClarify, Config: map[string]any{"agent_profile": "pm-agent", "prompt": "ask"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "work"},
			{ID: "e2", Source: "work", Target: "output"},
		},
	}
}

// bareEngine builds an engine without starting the dispatcher so the startup
// reconciliation result can be inspected before anything re-admits runs.
func bareEngine(t *testing.T) (*Engine, *gorm.DB) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	e := &Engine{
		db: db, provider: &fakeProvider{host: host}, host: host, store: arts,
		broker: NewBroker(), sem: newSema(1),
		wake: make(chan struct{}, 1), stop: make(chan struct{}),
		tokens: map[string]string{}, resumeLocks: map[string]*sync.Mutex{},
		execRuns: map[string]bool{}, execGen: map[string]uint64{},
		adoptedSandboxes: map[string]bool{},
	}
	return e, db
}

func TestReconcileInterruptedAdoptsRequeuesAndFails(t *testing.T) {
	e, db := bareEngine(t)
	now := time.Now()
	g := reattachGraph()

	// Auto agent mid-flight with a live run sandbox: adopted.
	db.Create(&models.Run{ID: "adopt", WorkflowID: "w", Status: "running", Graph: g})
	db.Create(&models.StateRun{RunID: "adopt", NodeID: "work", NodeType: "agent", Iteration: 2, Status: "running", StartedAt: &now})
	db.Create(&models.Sandbox{Name: "sb-adopt", Purpose: "run", Status: "running", RunID: "adopt", NodeID: "work"})
	// Auto agent whose sandbox row is gone: re-enters the node.
	db.Create(&models.Run{ID: "nosb", WorkflowID: "w", Status: "running", Graph: g})
	db.Create(&models.StateRun{RunID: "nosb", NodeID: "work", NodeType: "agent", Iteration: 1, Status: "running", StartedAt: &now})
	// Clarify agent: re-entered even with a sandbox.
	db.Create(&models.Run{ID: "clarify", WorkflowID: "w", Status: "running", Graph: g})
	db.Create(&models.StateRun{RunID: "clarify", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "running", StartedAt: &now})
	db.Create(&models.Sandbox{Name: "sb-clarify", Purpose: "run", Status: "running", RunID: "clarify", NodeID: "ask"})
	// Running run with no in-flight node: no known position, fails.
	db.Create(&models.Run{ID: "nopos", WorkflowID: "w", Status: "running", Graph: g})
	// Paused and queued runs are untouched.
	db.Create(&models.Run{ID: "paused", WorkflowID: "w", Status: "waiting_human", Graph: g})
	db.Create(&models.Run{ID: "queued", WorkflowID: "w", Status: "queued", Graph: g})

	e.reconcileInterrupted()

	status := func(id string) string {
		var r models.Run
		db.First(&r, "id = ?", id)
		return r.Status
	}
	stateStatus := func(runID string) string {
		var sr models.StateRun
		db.Where("run_id = ?", runID).Order("id desc").First(&sr)
		return sr.Status
	}

	if got := status("adopt"); got != "running" {
		t.Errorf("adopted run status = %q, want running", got)
	}
	if got := stateStatus("adopt"); got != "running" {
		t.Errorf("adopted state = %q, want running", got)
	}
	if !e.AdoptedSandboxes()["sb-adopt"] {
		t.Error("sb-adopt should be adopted")
	}
	if len(e.pendingReattach) != 1 || e.pendingReattach[0] != (reattachInfo{runID: "adopt", nodeID: "work", iteration: 2, sandbox: "sb-adopt"}) {
		t.Errorf("pendingReattach = %+v", e.pendingReattach)
	}
	if e.sem.TryAcquire() {
		t.Error("adopted run should hold the only slot")
	}

	for _, id := range []string{"nosb", "clarify"} {
		if got := status(id); got != "queued" {
			t.Errorf("%s run status = %q, want queued", id, got)
		}
		if got := stateStatus(id); got != "cancelled" {
			t.Errorf("%s state = %q, want cancelled", id, got)
		}
	}
	var nosb models.Run
	db.First(&nosb, "id = ?", "nosb")
	if from, _ := nosb.Checkpoints[admissionFromCheckpoint]["node"].(string); from != "work" {
		t.Errorf("nosb admission node = %q, want work", from)
	}
	if e.AdoptedSandboxes()["sb-clarify"] {
		t.Error("clarify sandbox must not be adopted")
	}

	if got := status("nopos"); got != "failed" {
		t.Errorf("nopos status = %q, want failed", got)
	}
	if got := status("paused"); got != "waiting_human" {
		t.Errorf("paused status = %q", got)
	}
	if got := status("queued"); got != "queued" {
		t.Errorf("queued status = %q", got)
	}
}

func TestResumeInterruptedContinuesSameVisit(t *testing.T) {
	e, db := bareEngine(t)
	now := time.Now()
	db.Create(&models.Run{ID: "r", WorkflowID: "w", Status: "running", Graph: reattachGraph(), StartedAt: now, McpToken: "tok-r"})
	db.Create(&models.StateRun{RunID: "r", NodeID: "input", NodeType: "input", Iteration: 1, Status: "completed", StartedAt: &now})
	db.Create(&models.StateRun{RunID: "r", NodeID: "work", NodeType: "agent", Iteration: 1, Status: "running", StartedAt: &now,
		ArtifactBaseSet: true})
	db.Create(&models.Sandbox{Name: "sb-r", Purpose: "run", Status: "running", RunID: "r", NodeID: "work"})

	e.reconcileInterrupted()
	e.ResumeInterrupted()
	waitRunStatus(t, db, "r", "completed")

	var states []models.StateRun
	db.Where("run_id = ? AND node_id = ?", "r", "work").Find(&states)
	if len(states) != 1 || states[0].Iteration != 1 || states[0].Status != "completed" {
		t.Fatalf("work visits = %+v, want one completed iteration 1", states)
	}
	fp := e.provider.(*fakeProvider)
	fp.mu.Lock()
	got := fp.reattached["work"]
	fp.mu.Unlock()
	if got != "sb-r" {
		t.Errorf("RunAgent ReattachSandbox = %q, want sb-r", got)
	}
	var run models.Run
	db.First(&run, "id = ?", "r")
	found := false
	for _, tr := range run.Trace {
		if tr.Event == "reattach" && tr.NodeID == "work" {
			found = true
		}
		if tr.Event == "enter" && tr.NodeID == "work" {
			t.Error("reattached node must not open a new visit")
		}
	}
	if !found {
		t.Error("trace should record the reattach")
	}
	if !e.sem.TryAcquire() {
		t.Error("slot should be released after the run finished")
	}
}

func TestExecuteRequeuesWhenHalted(t *testing.T) {
	e, db := bareEngine(t)
	db.Create(&models.Run{ID: "h", WorkflowID: "w", Status: "running", Graph: reattachGraph(), StartedAt: time.Now()})
	e.Halt()
	e.execute("h", "work")

	var run models.Run
	db.First(&run, "id = ?", "h")
	if run.Status != "queued" {
		t.Fatalf("status = %q, want queued", run.Status)
	}
	if from, _ := run.Checkpoints[admissionFromCheckpoint]["node"].(string); from != "work" {
		t.Errorf("admission node = %q, want work", from)
	}
	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ?", "h").Count(&n)
	if n != 0 {
		t.Errorf("halted driver must not start a node, got %d visits", n)
	}
}
