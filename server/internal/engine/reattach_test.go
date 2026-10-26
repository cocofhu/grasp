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
	// finish() writes "completed" from inside the driver; the driver releases
	// its slot only when it unwinds afterwards. Wait for that, not just for
	// the status.
	waitDriversIdle(t, e)
	if !e.sem.TryAcquire() {
		t.Error("slot should be released after the run finished")
	}
}

// waitDriversIdle waits until every admitted driver has released its slot.
func waitDriversIdle(t *testing.T, e *Engine) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for e.ActiveDrivers() > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("drivers still active: %d", e.ActiveDrivers())
		}
		time.Sleep(time.Millisecond)
	}
}

// Regression for the TestResumeInterruptedContinuesSameVisit flake: the run
// is visibly "completed" while its driver still holds the slot. The driver is
// parked right after finish() commits the status to make the window
// deterministic.
func TestReattachedRunCompletedBeforeSlotRelease(t *testing.T) {
	e, db := bareEngine(t)
	committed := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:park-after-finish", func(tx *gorm.DB) {
		if tx.Statement.Table != "runs" {
			return
		}
		if m, ok := tx.Statement.Dest.(map[string]any); ok && m["status"] == "completed" {
			once.Do(func() {
				close(committed)
				<-release
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	db.Create(&models.Run{ID: "r", WorkflowID: "w", Status: "running", Graph: reattachGraph(), StartedAt: now, McpToken: "tok-r"})
	db.Create(&models.StateRun{RunID: "r", NodeID: "input", NodeType: "input", Iteration: 1, Status: "completed", StartedAt: &now})
	db.Create(&models.StateRun{RunID: "r", NodeID: "work", NodeType: "agent", Iteration: 1, Status: "running", StartedAt: &now,
		ArtifactBaseSet: true})
	db.Create(&models.Sandbox{Name: "sb-r", Purpose: "run", Status: "running", RunID: "r", NodeID: "work"})

	e.reconcileInterrupted()
	e.ResumeInterrupted()
	<-committed
	waitRunStatus(t, db, "r", "completed")
	// The flaky test checked the slot right here.
	if e.sem.TryAcquire() {
		t.Fatal("slot should still be held by the unwinding driver")
	}
	close(release)
	waitDriversIdle(t, e)
	if !e.sem.TryAcquire() {
		t.Fatal("slot should be released once the driver unwound")
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

func resumeHandoffGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "ask", Type: "agent", Caps: capsClarify, Label: "需求澄清", Config: map[string]any{"agent_profile": "pm-agent", "prompt": "ask"}},
			{ID: "impl", Type: "agent", Caps: capsPlain, Label: "实现", Config: map[string]any{"agent_profile": "dev-agent", "prompt": "impl"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "ask", Target: "impl"},
			{ID: "e2", Source: "impl", Target: "output"},
		},
	}
}

func admissionNode(run models.Run) string {
	if run.Checkpoints == nil {
		return ""
	}
	cp := run.Checkpoints[admissionFromCheckpoint]
	if cp == nil {
		return ""
	}
	node, _ := cp["node"].(string)
	return node
}

func loadRun(t *testing.T, db *gorm.DB, id string) models.Run {
	t.Helper()
	var run models.Run
	if err := db.First(&run, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

// The transition and the resume position are one write. The run is not queued
// while the driver that will enter the node is still in this process.
func TestTransitionPersistsResumeTargetWithoutQueue(t *testing.T) {
	e, db := bareEngine(t)
	g := resumeHandoffGraph()
	db.Create(&models.Run{ID: "r", WorkflowID: "w", Status: "waiting_human", Graph: g})
	c, err := e.loadCtx("r")
	if err != nil {
		t.Fatal(err)
	}
	next := e.routeSuccess(c, c.graph.FindNode("ask"), nodeOutcome{status: "completed"})
	if next != "impl" {
		t.Fatalf("next = %q, want impl", next)
	}
	run := loadRun(t, db, "r")
	if run.Status != "waiting_human" {
		t.Fatalf("status = %q, want waiting_human", run.Status)
	}
	if got := admissionNode(run); got != "impl" {
		t.Fatalf("admission node = %q, want impl", got)
	}
	saw := false
	for _, tr := range run.Trace {
		if tr.Event == "transition" && tr.To == "impl" && tr.NodeID == "ask" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("trace missing the transition to impl")
	}

	db.Create(&models.Run{ID: "end", WorkflowID: "w", Status: "running", Graph: models.Graph{
		Nodes: []models.Node{{ID: "output", Type: "output"}},
	}})
	end, err := e.loadCtx("end")
	if err != nil {
		t.Fatal(err)
	}
	if next := e.routeSuccess(end, end.graph.FindNode("output"), nodeOutcome{status: "completed"}); next != "" {
		t.Fatalf("terminal next = %q, want empty", next)
	}
	endRun := loadRun(t, db, "end")
	if endRun.Status != "running" {
		t.Fatalf("terminal status = %q, want running", endRun.Status)
	}
	if got := admissionNode(endRun); got != "" {
		t.Fatalf("terminal admission node = %q, want empty", got)
	}
}

// Waiting for a person, but the visit already finished and the trace names
// the next node: queue that node. A running run in the same window is not
// failed for lack of an in-flight visit.
func TestReconcileContinuesUnstartedTransition(t *testing.T) {
	e, db := bareEngine(t)
	now := time.Now()
	g := resumeHandoffGraph()
	trace := []models.TraceEntry{
		{NodeID: "ask", Event: "exit"},
		{NodeID: "ask", Event: "transition", To: "impl"},
	}
	db.Create(&models.Run{ID: "paused", WorkflowID: "w", Status: "waiting_human", Graph: g, Trace: trace})
	db.Create(&models.StateRun{RunID: "paused", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now})
	db.Create(&models.Run{ID: "running", WorkflowID: "w", Status: "running", Graph: g, Trace: trace})
	db.Create(&models.StateRun{RunID: "running", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now})

	e.reconcileInterrupted()

	for _, id := range []string{"paused", "running"} {
		run := loadRun(t, db, id)
		if run.Status != "queued" {
			t.Errorf("%s status = %q, want queued", id, run.Status)
		}
		if got := admissionNode(run); got != "impl" {
			t.Errorf("%s admission node = %q, want impl", id, got)
		}
	}
	if e.ActiveDrivers() != 0 {
		t.Errorf("reconciliation must queue, not start a driver; drivers=%d", e.ActiveDrivers())
	}
}

// An older resume checkpoint must not hide a newer transition target.
func TestReconcileTraceBeatsOlderResumeCheckpoint(t *testing.T) {
	e, db := bareEngine(t)
	now := time.Now()
	g := resumeHandoffGraph()
	db.Create(&models.Run{
		ID: "stale", WorkflowID: "w", Status: "waiting_human", Graph: g,
		Checkpoints: map[string]map[string]any{
			admissionFromCheckpoint: {"node": "ask"},
		},
		Trace: []models.TraceEntry{
			{NodeID: "ask", Event: "enter"},
			{NodeID: "ask", Event: "exit"},
			{NodeID: "ask", Event: "transition", To: "impl"},
		},
	})
	db.Create(&models.StateRun{RunID: "stale", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now})

	e.reconcileInterrupted()

	run := loadRun(t, db, "stale")
	if run.Status != "queued" {
		t.Fatalf("status = %q, want queued", run.Status)
	}
	if got := admissionNode(run); got != "impl" {
		t.Fatalf("admission node = %q, want impl", got)
	}
}

// The confirm marked the dialogue done and then the process died before a
// target was written. Reopen the dialogue; do not replay the model wrap-up.
func TestReconcileReopensConfirmWithoutTarget(t *testing.T) {
	e, db := bareEngine(t)
	now := time.Now()
	g := resumeHandoffGraph()
	msg := []models.ReactMessage{{Role: "human", Text: "确认并流转", At: now.Format(time.RFC3339)}}
	db.Create(&models.Run{ID: "half", WorkflowID: "w", Status: "waiting_human", Graph: g})
	db.Create(&models.StateRun{RunID: "half", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "waiting_human", StartedAt: &now})
	db.Create(&models.ReactConversation{RunID: "half", NodeID: "ask", Iteration: 1, Done: true, Messages: msg})
	db.Create(&models.Run{ID: "half-run", WorkflowID: "w", Status: "running", Graph: g})
	db.Create(&models.StateRun{RunID: "half-run", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "waiting_human", StartedAt: &now})
	db.Create(&models.ReactConversation{RunID: "half-run", NodeID: "ask", Iteration: 1, Done: true, Messages: msg})

	e.reconcileInterrupted()

	for _, id := range []string{"half", "half-run"} {
		run := loadRun(t, db, id)
		if run.Status != "waiting_human" {
			t.Errorf("%s status = %q, want waiting_human", id, run.Status)
		}
		var conv models.ReactConversation
		if err := db.Where("run_id = ?", id).First(&conv).Error; err != nil {
			t.Fatal(err)
		}
		if conv.Done {
			t.Errorf("%s conversation still done", id)
		}
		if len(conv.Messages) != 1 || conv.Messages[0].Text != "确认并流转" {
			t.Errorf("%s messages = %+v", id, conv.Messages)
		}
		var sr models.StateRun
		db.Where("run_id = ?", id).First(&sr)
		if sr.Status != "waiting_human" {
			t.Errorf("%s visit = %q, want waiting_human", id, sr.Status)
		}
	}
	fp := e.provider.(*fakeProvider)
	fp.mu.Lock()
	n := fp.reactReplyCalls["ask"]
	fp.mu.Unlock()
	if n != 0 {
		t.Fatalf("startup replayed the model wrap-up %d times", n)
	}
}

// A missing transition target fails the run and keeps the reason. A run that
// never visited a node still fails.
func TestReconcileMissingTransitionTargetFails(t *testing.T) {
	e, db := bareEngine(t)
	now := time.Now()
	g := resumeHandoffGraph()
	db.Create(&models.Run{
		ID: "ghost", WorkflowID: "w", Status: "running", Graph: g,
		Trace: []models.TraceEntry{{NodeID: "ask", Event: "transition", To: "missing"}},
	})
	db.Create(&models.StateRun{RunID: "ghost", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now})
	db.Create(&models.Run{ID: "nopos", WorkflowID: "w", Status: "running", Graph: g})

	e.reconcileInterrupted()

	if got := loadRun(t, db, "ghost").Status; got != "failed" {
		t.Fatalf("ghost status = %q, want failed", got)
	}
	var v models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", "ghost", "last_error").First(&v).Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Value.(string); got == "" {
		t.Fatalf("last_error = %#v, want a reason", v.Value)
	}
	if got := loadRun(t, db, "nopos").Status; got != "failed" {
		t.Fatalf("nopos status = %q, want failed", got)
	}
}

// Unfinished dialogues stay waiting. Queued resumes are admitted by the
// dispatcher once. Terminal runs stay terminal.
func TestReconcileLeavesStableRunsAlone(t *testing.T) {
	e, db := bareEngine(t)
	now := time.Now()
	g := resumeHandoffGraph()
	trace := []models.TraceEntry{{NodeID: "ask", Event: "transition", To: "impl"}}
	db.Create(&models.Run{ID: "open", WorkflowID: "w", Status: "waiting_human", Graph: g})
	db.Create(&models.StateRun{RunID: "open", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "waiting_human", StartedAt: &now})
	db.Create(&models.ReactConversation{RunID: "open", NodeID: "ask", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "human", Text: "还没确认"}}})
	db.Create(&models.Run{
		ID: "queued", WorkflowID: "w", Status: "queued", Graph: g, Trace: trace,
		Checkpoints: map[string]map[string]any{admissionFromCheckpoint: {"node": "impl"}},
	})
	db.Create(&models.StateRun{RunID: "queued", NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now})
	for _, st := range []string{"completed", "failed", "cancelled"} {
		db.Create(&models.Run{ID: st, WorkflowID: "w", Status: st, Graph: g, Trace: trace})
		db.Create(&models.StateRun{RunID: st, NodeID: "ask", NodeType: "agent", Iteration: 1, Status: "completed", StartedAt: &now})
	}

	e.reconcileInterrupted()

	open := loadRun(t, db, "open")
	if open.Status != "waiting_human" {
		t.Fatalf("open status = %q, want waiting_human", open.Status)
	}
	var conv models.ReactConversation
	db.Where("run_id = ?", "open").First(&conv)
	if conv.Done {
		t.Fatal("unfinished dialogue was closed")
	}
	queued := loadRun(t, db, "queued")
	if queued.Status != "queued" {
		t.Fatalf("queued status = %q", queued.Status)
	}
	if got := admissionNode(queued); got != "impl" {
		t.Fatalf("queued admission = %q, want impl", got)
	}
	if e.ActiveDrivers() != 0 {
		t.Fatalf("queued resume started a driver")
	}
	for _, st := range []string{"completed", "failed", "cancelled"} {
		if got := loadRun(t, db, st).Status; got != st {
			t.Errorf("%s status = %q", st, got)
		}
	}
}
