package engine

// Regression tests for the GraspReview round on feature/node-takeover:
// B1 (cancel while stopping), S1 (concurrent duplicate takeover), S5
// (no Review=true leak into the stored graph), S6 (CAS lost to a cancel),
// S7 (settle on provider panic, no activity stamp on finished runs).

import (
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

// TestTakeoverConcurrentDuplicateIsIdempotent (S1, GraspReview R1): a burst
// of takeovers of the same visit (double click / two tabs) yields exactly one
// taken_over and 200 already_manual for the rest — never node_finishing.
func TestTakeoverConcurrentDuplicateIsIdempotent(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	env.provider.runIgnoreCancel = true
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, env.provider)
	const n = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[string]int{}
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := env.eng.TakeoverNode(run.ID, "impl", 0)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if te, ok := err.(*TakeoverError); ok {
					codes["409:"+te.Code]++
					return
				}
				codes["err"]++
				return
			}
			codes[res.Status]++
		}()
	}
	close(start)
	wg.Wait()
	if codes[TakeoverStatusTakenOver] != 1 || codes[TakeoverStatusAlreadyManual] != n-1 {
		t.Fatalf("want 1 taken_over + %d already_manual, got %v", n-1, codes)
	}
	env.provider.releaseHold("impl")
	waitRunStatus(t, env.db, run.ID, "waiting_human")
}

// TestTakeoverVsSettleStress (GraspReview R2): settle()==true iff the
// takeover returned taken_over, and a lost takeover never flips the row.
func TestTakeoverVsSettleStress(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	for i := 0; i < 60; i++ {
		run := models.Run{ID: "run-stress-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			WorkflowID: "wf", Status: "running", Graph: takeoverPlainGraph(capsPlain), StartedAt: time.Now()}
		if err := env.db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if err := env.db.Create(&models.StateRun{RunID: run.ID, NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "running", Mode: "auto"}).Error; err != nil {
			t.Fatal(err)
		}
		c, err := env.eng.loadCtx(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, ar := env.eng.beginAutoAgent(c, c.graph.FindNode("impl"))
		var settled bool
		var tres TakeoverResult
		var terr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); settled = env.eng.settleAutoAgent(run.ID, "impl", ar) }()
		go func() { defer wg.Done(); tres, terr = env.eng.TakeoverNode(run.ID, "impl", 0) }()
		wg.Wait()
		won := terr == nil && tres.Status == TakeoverStatusTakenOver
		if settled != won {
			t.Fatalf("iter %d: settle=%v takeoverWon=%v (res=%+v err=%v)", i, settled, won, tres, terr)
		}
		sr := latestVisit(t, env.db, run.ID, "impl")
		if won != sr.IsManual() {
			t.Fatalf("iter %d: won=%v but row manual=%v", i, won, sr.IsManual())
		}
	}
}

// cancelDuringStopping takes over a held auto visit, cancels the run while
// the agent is still unwinding, then lets RunAgent return (GraspReview R3).
func cancelDuringStopping(t *testing.T, env takeoverEnv) models.Run {
	t.Helper()
	env.provider.runIgnoreCancel = true
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatal(err)
	}
	if err := env.eng.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	env.provider.releaseHold("impl")
	waitRunStatus(t, env.db, run.ID, "cancelled")
	// enterTakeover runs on the driver goroutine after RunAgent returned.
	time.Sleep(300 * time.Millisecond)
	return *run
}

// TestCancelDuringTakeoverStoppingLeavesNoSession (B1, GraspReview R3): the
// late enterTakeover of a cancelled run neither re-arms the MCP review phase
// nor leaves an open Takeover conversation behind.
func TestCancelDuringTakeoverStoppingLeavesNoSession(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := cancelDuringStopping(t, env)
	if env.host.InReviewPhase(run.ID) {
		t.Errorf("host activeReview still true for cancelled run %s", run.ID)
	}
	var open int64
	env.db.Model(&models.ReactConversation{}).Where("run_id = ? AND takeover = ? AND done = ?", run.ID, true, false).Count(&open)
	if open != 0 {
		t.Errorf("dangling open Takeover conversation on cancelled run (%d)", open)
	}
}

// TestResumeAfterCancelledTakeoverStartsAuto (B1, GraspReview R4): resuming
// the node after R3 opens a fresh auto visit outside any review phase.
func TestResumeAfterCancelledTakeoverStartsAuto(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := cancelDuringStopping(t, env)
	env.provider.mu.Lock()
	env.provider.runHold["impl"] = make(chan struct{})
	env.provider.runIgnoreCancel = false
	env.provider.mu.Unlock()
	if err := env.eng.ResumeFrom(run.ID, "impl"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitStarted(t, env.provider)
	if sr := latestVisit(t, env.db, run.ID, "impl"); sr.IsManual() {
		t.Errorf("resumed visit must be auto: %+v", sr)
	}
	if env.host.InReviewPhase(run.ID) {
		t.Errorf("auto visit after ResumeFrom runs with InReviewPhase=true")
	}
	env.provider.releaseHold("impl")
	waitRunStatus(t, env.db, run.ID, "completed")
}

// TestRestartAfterParkedTakeoverThenConfirm (GraspReview R5): a new engine
// re-applies the in-memory review caps from the manual row, a manual turn and
// confirm work, and the stored graph never carries Review=true.
func TestRestartAfterParkedTakeoverThenConfirm(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsImplement))
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")

	eng2 := New(env.db, env.provider, env.host, env.arts, 5)
	t.Cleanup(eng2.Close)
	c, err := eng2.loadCtx(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.graph.FindNode("impl").Caps.ReviewEnabled() {
		t.Fatal("restart: in-memory caps must be Review=true for the manual visit")
	}
	if err := eng2.ReactReply(run.ID, "impl", "补齐产物", nil, nil, false); err != nil {
		t.Fatalf("manual turn after restart: %v", err)
	}
	if err := eng2.waitReviewReadyForTest(run.ID, "impl", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := eng2.ReactReply(run.ID, "impl", "确认", nil, nil, true); err != nil {
		t.Fatalf("confirm after restart: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "completed")
	var r models.Run
	env.db.First(&r, "id = ?", run.ID)
	if n := r.Graph.FindNode("impl"); n == nil || n.Caps == nil || n.Caps.Review {
		t.Fatalf("persisted graph leaked Review=true: %+v", n)
	}
}

type fakeSkills map[string]services.Agent

func (f fakeSkills) Get(name string) (services.Agent, bool) { a, ok := f[name]; return a, ok }

// TestRefreshReviewFlagNeverPersistsTakeoverOverlay (S5): refreshing another
// node's review flag writes only that node into the stored graph; the
// Review=true clone of a taken-over node stays in memory.
func TestRefreshReviewFlagNeverPersistsTakeoverOverlay(t *testing.T) {
	g := takeoverPlainGraph(capsPlain)
	g.Nodes = append(g.Nodes, models.Node{ID: "rev", Type: "agent", Caps: &models.AgentCapabilities{},
		Config: map[string]any{"agent_profile": "rv", "prompt": "r"}})
	env := setupTakeoverEngine(t, g)
	run := models.Run{ID: "run-flag", WorkflowID: "wf", Status: "running", Graph: g, StartedAt: time.Now()}
	if err := env.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Create(&models.StateRun{RunID: run.ID, NodeID: "impl", NodeType: "agent", Iteration: 1,
		Status: "waiting_human", Mode: models.NodeModeManual}).Error; err != nil {
		t.Fatal(err)
	}
	c, err := env.eng.loadCtx(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.graph.FindNode("impl").Caps.ReviewEnabled() {
		t.Fatal("manual visit must get the in-memory review overlay")
	}
	env.eng.SetAgents(fakeSkills{
		"dev": {Name: "dev", Capabilities: &models.AgentCapabilities{}},
		"rv":  {Name: "rv", Capabilities: &models.AgentCapabilities{Review: true}},
	})
	env.eng.refreshReviewFlag(c, c.graph.FindNode("rev"))
	var stored models.Run
	env.db.First(&stored, "id = ?", run.ID)
	if n := stored.Graph.FindNode("impl"); n.Caps == nil || n.Caps.Review {
		t.Fatalf("takeover overlay leaked into stored graph: %+v", n.Caps)
	}
	if n := stored.Graph.FindNode("rev"); n.Caps == nil || !n.Caps.Review {
		t.Fatalf("refreshed node's flag not persisted: %+v", n.Caps)
	}
	if !c.graph.FindNode("impl").Caps.ReviewEnabled() {
		t.Fatal("in-memory overlay must survive the refresh")
	}
}

// TestEffectiveNodeTakeoverReadsAsReview (S5): the shared manual-aware
// accessor (share links, embed, MCP fallback) sees a live takeover visit as a
// review node, and an old / finished manual visit as its stored caps.
func TestEffectiveNodeTakeoverReadsAsReview(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, models.NodeModeManual)
	n := services.EffectiveNode(env.db, run, "impl")
	if n == nil || !n.Caps.ReviewEnabled() || !services.IsShareableReviewSession(n) {
		t.Fatalf("live takeover must read as a shareable review node: %+v", n)
	}
	if run.Graph.FindNode("impl").Caps.Review {
		t.Fatal("accessor must not mutate the graph's caps")
	}
	env.db.Model(&models.StateRun{}).Where("run_id = ?", run.ID).Update("status", "completed")
	if n := services.EffectiveNode(env.db, run, "impl"); n.Caps.ReviewEnabled() {
		t.Fatal("a finished manual visit must not read as a live review")
	}
}

// TestTakeoverCASLostToCancelIsRunNotRunning (S6): when the run is cancelled
// between the pre-check and the CAS, the refusal is run_not_running.
func TestTakeoverCASLostToCancelIsRunNotRunning(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, "auto")
	c, _ := env.eng.loadCtx(run.ID)
	_, ar := env.eng.beginAutoAgent(c, c.graph.FindNode("impl"))
	defer env.eng.settleAutoAgent(run.ID, "impl", ar)
	fired := false
	cb := "test:cancel_before_takeover_cas"
	if err := env.db.Callback().Update().Before("gorm:update").Register(cb, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "state_runs" {
			return
		}
		if m, ok := tx.Statement.Dest.(map[string]any); !ok || m["mode"] != models.NodeModeManual {
			return
		}
		fired = true
		s := tx.Session(&gorm.Session{NewDB: true})
		s.Exec("UPDATE state_runs SET status = 'cancelled' WHERE run_id = ?", run.ID)
		s.Exec("UPDATE runs SET status = 'cancelled' WHERE id = ?", run.ID)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.db.Callback().Update().Remove(cb) })
	_, err := env.eng.TakeoverNode(run.ID, "impl", 0)
	if !fired {
		t.Fatal("hook never fired")
	}
	if code := takeoverCode(t, err); code != TakeoverRunNotRunning {
		t.Fatalf("code=%s, want run_not_running", code)
	}
	if err.(*TakeoverError).Current.RunStatus != "cancelled" {
		t.Fatalf("current=%+v", err.(*TakeoverError).Current)
	}
}

// TestRunAutoAgentPanicSettles (S7): a panicking provider does not leave the
// visit registered as takeover-able.
func TestRunAutoAgentPanicSettles(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	env.provider.runPanic = map[string]bool{"impl": true}
	run := insertRunningVisit(t, env, "auto")
	c, _ := env.eng.loadCtx(run.ID)
	c.iter["impl"] = 1
	func() {
		defer func() { _ = recover() }()
		env.eng.runAutoAgent(c, c.graph.FindNode("impl"))
	}()
	env.eng.autoMu.Lock()
	left := len(env.eng.autoRunsMap())
	env.eng.autoMu.Unlock()
	if left != 0 {
		t.Fatalf("autoRuns still holds %d entries after a provider panic", left)
	}
	if code := takeoverCode(t, firstErr(env.eng.TakeoverNode(run.ID, "impl", 0))); code != TakeoverNodeFinishing {
		t.Fatalf("code=%s, want node_finishing", code)
	}
}

func firstErr(_ TakeoverResult, err error) error { return err }

// TestTouchActivitySkipsFinishedRun (S7): late frames after finish() neither
// stamp a finished run nor re-grow the debounce map.
func TestTouchActivitySkipsFinishedRun(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := models.Run{ID: "run-done", WorkflowID: "wf", Status: "completed", Graph: takeoverPlainGraph(capsPlain), StartedAt: time.Now()}
	if err := env.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	env.eng.touchActivity(run.ID, false)
	env.eng.touchActivity(run.ID, true)
	var got models.Run
	env.db.First(&got, "id = ?", run.ID)
	if got.LastActivityAt != nil {
		t.Fatalf("finished run was stamped: %v", got.LastActivityAt)
	}
	a := env.eng.activity()
	a.mu.Lock()
	_, kept := a.last[run.ID]
	a.mu.Unlock()
	if kept {
		t.Fatal("debounce map re-grew for a finished run")
	}
}

func TestAutoPhaseEventsDropsPrompt(t *testing.T) {
	got := autoPhaseEvents([]models.AcpEvent{{Kind: "prompt", Text: "system"}, {Kind: "thought"}, {Kind: "message"}})
	if len(got) != 2 || got[0].Kind != "thought" {
		t.Fatalf("got %+v", got)
	}
	if autoPhaseEvents([]models.AcpEvent{{Kind: "prompt"}}) != nil {
		t.Fatal("prompt-only timeline must yield nil (omitted)")
	}
}
