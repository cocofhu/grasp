package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

func takeoverPlainGraph(caps *models.AgentCapabilities) models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "impl", Type: "agent", Caps: caps, Label: "实现", Config: map[string]any{"agent_profile": "dev", "prompt": "实现"}},
			{ID: "output", Type: "output", Label: "输出"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "impl"},
			{ID: "e2", Source: "impl", Target: "output"},
		},
	}
}

func takeoverLoopGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "impl", Type: "agent", Caps: capsPlain, Label: "实现", Config: map[string]any{"agent_profile": "dev", "prompt": "实现"}},
			{ID: "gate", Type: "human_gate", Config: map[string]any{"title": "评审", "output_var": "action",
				"actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
					map[string]any{"id": "revise", "label": "退回修改"},
				}}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "impl"},
			{ID: "e2", Source: "impl", Target: "gate"},
			{ID: "e3", Source: "gate", Target: "output", SourceHandle: "approve", Kind: models.EdgeSuccess},
			{ID: "e4", Source: "gate", Target: "impl", SourceHandle: "revise"},
		},
	}
}

type takeoverEnv struct {
	eng      *Engine
	db       *gorm.DB
	provider *fakeProvider
	host     *mcp.Host
	arts     *services.ArtifactService
}

func setupTakeoverEngine(t *testing.T, g models.Graph) takeoverEnv {
	t.Helper()
	db, err := database.OpenSQLiteTest(t.TempDir() + "/takeover.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	wf := models.WorkflowDef{ID: "wf", Name: "wf", Version: 1, PublishedVersion: 1, Graph: g}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: g}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	provider := &fakeProvider{host: host,
		runHold:    map[string]chan struct{}{"impl": make(chan struct{})},
		runStarted: make(chan string, 4)}
	eng := New(db, provider, host, arts, 5)
	cleanupEngineDB(t, eng, db)
	t.Cleanup(func() { provider.releaseHold("impl") })
	return takeoverEnv{eng: eng, db: db, provider: provider, host: host, arts: arts}
}

func (f *fakeProvider) releaseHold(nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.runHold[nodeID]; ok && ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
}

func waitStarted(t *testing.T, f *fakeProvider) {
	t.Helper()
	select {
	case <-f.runStarted:
	case <-time.After(waitPollTimeout):
		t.Fatal("RunAgent never started")
	}
}

func latestVisit(t *testing.T, db *gorm.DB, runID, nodeID string) models.StateRun {
	t.Helper()
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", runID, nodeID).Order("iteration desc, id desc").First(&sr).Error; err != nil {
		t.Fatalf("load state run: %v", err)
	}
	return sr
}

func takeoverCode(t *testing.T, err error) string {
	t.Helper()
	var te *TakeoverError
	if !errors.As(err, &te) {
		t.Fatalf("want *TakeoverError, got %v", err)
	}
	return te.Code
}

// collectFrames records every frame of a run's broker topic.
func collectFrames(eng *Engine, runID string) (get func() []map[string]any, stop func()) {
	ch, unsub := eng.Broker().Subscribe(runID)
	var mu sync.Mutex
	var frames []map[string]any
	done := make(chan struct{})
	go func() {
		for {
			select {
			case b, ok := <-ch:
				if !ok {
					return
				}
				var m map[string]any
				if json.Unmarshal(b, &m) == nil {
					mu.Lock()
					frames = append(frames, m)
					mu.Unlock()
				}
			case <-done:
				return
			}
		}
	}()
	return func() []map[string]any {
			mu.Lock()
			defer mu.Unlock()
			return append([]map[string]any(nil), frames...)
		}, func() {
			close(done)
			unsub()
		}
}

func takeoverFrames(frames []map[string]any) []map[string]any {
	var out []map[string]any
	for _, f := range frames {
		if f["type"] == "node_takeover" {
			out = append(out, f)
		}
	}
	return out
}

// TestTakeoverRunningAutoNodeBecomesManual: interrupting the running auto
// Agent parks the visit as a manual session (waiting_human, Takeover conv),
// with no failure / auto-retry, two node_takeover frames, and confirm-and-
// advance finishes the node through the review path.
func TestTakeoverRunningAutoNodeBecomesManual(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	get, stop := collectFrames(env.eng, run.ID)
	defer stop()
	waitStarted(t, env.provider)

	res, err := env.eng.TakeoverNode(run.ID, "impl", 0)
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if res.Status != TakeoverStatusTakenOver || res.State != NodeStateStopping || res.Mode != "manual" || res.Iteration != 1 {
		t.Fatalf("unexpected result %+v", res)
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")

	sr := latestVisit(t, env.db, run.ID, "impl")
	if !sr.IsManual() || sr.TakenOverAt == nil || sr.Status != "waiting_human" {
		t.Fatalf("visit not parked manual: mode=%q status=%q takenOverAt=%v", sr.Mode, sr.Status, sr.TakenOverAt)
	}
	var conv models.ReactConversation
	if err := env.db.Where("run_id = ? AND node_id = ?", run.ID, "impl").First(&conv).Error; err != nil {
		t.Fatalf("takeover conversation: %v", err)
	}
	// With the auto timeline attached, the narration is not repeated in
	// the text (the chat renders the events); the auto phase's span is set.
	if !conv.Takeover || conv.Done || len(conv.Messages) == 0 || conv.Messages[0].Text != takeoverOpening ||
		strings.Contains(conv.Messages[0].Text, "partial narration") ||
		conv.Messages[0].StartedAt == nil || conv.Messages[0].EndedAt == nil ||
		conv.Messages[0].EndedAt.Before(*conv.Messages[0].StartedAt) {
		t.Fatalf("bad takeover conversation: %+v", conv)
	}
	// The opening turn continues the auto phase's timeline.
	if evs := conv.Messages[0].Events; len(evs) != 3 || evs[0].Kind != "thought" || evs[1].Kind != "tool_call" {
		t.Fatalf("opening turn must carry the auto-phase events: %+v", evs)
	}
	env.provider.mu.Lock()
	parks, calls := env.provider.takeoverParks, env.provider.runAgentCalls["impl"]
	env.provider.mu.Unlock()
	if parks != 1 || calls != 1 {
		t.Fatalf("parks=%d runAgentCalls=%d, want 1/1 (no retry)", parks, calls)
	}
	var failed int64
	env.db.Model(&models.StateRun{}).Where("run_id = ? AND status = ?", run.ID, "failed").Count(&failed)
	if failed != 0 {
		t.Fatalf("takeover must not record a failure, got %d failed rows", failed)
	}

	deadline := time.Now().Add(waitPollTimeout)
	for len(takeoverFrames(get())) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	fr := takeoverFrames(get())
	if len(fr) < 2 || fr[0]["state"] != "stopping" || fr[1]["state"] != "waiting_human" {
		t.Fatalf("want node_takeover stopping then waiting_human, got %v", fr)
	}
	for _, f := range fr {
		if f["runId"] != run.ID || f["nodeId"] != "impl" || f["mode"] != "manual" || f["iteration"] != float64(1) {
			t.Fatalf("bad node_takeover payload %v", f)
		}
	}

	if err := env.eng.ReactReply(run.ID, "impl", "确认", nil, nil, true); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "completed")
	if !env.provider.retired[env.provider.parkKey(run.ID, "impl")] {
		t.Fatal("confirm must retire the parked takeover session")
	}
	env.db.Where("run_id = ? AND node_id = ?", run.ID, "impl").First(&conv)
	if !conv.Done {
		t.Fatal("takeover conversation should be done after confirm")
	}
	if sr := latestVisit(t, env.db, run.ID, "impl"); !sr.IsManual() || sr.Status != "completed" {
		t.Fatalf("visit must stay manual after confirm: %+v", sr)
	}
}

// TestTakeoverConfirmFailsOnMissingProductsStaysManual: the interrupted agent
// never wrote its required product, so confirm fails and the node stays
// manual; a manual turn completes the product and confirm then advances.
func TestTakeoverConfirmFailsOnMissingProductsStaysManual(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsImplement))
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")

	if err := env.eng.ReactReply(run.ID, "impl", "确认", nil, nil, true); err == nil {
		t.Fatal("confirm must fail while the required product is missing")
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")
	if sr := latestVisit(t, env.db, run.ID, "impl"); !sr.IsManual() || sr.Status != "waiting_human" {
		t.Fatalf("node must stay manual waiting_human: %+v", sr)
	}

	if err := env.eng.ReactReply(run.ID, "impl", "把产物补齐", nil, nil, false); err != nil {
		t.Fatalf("manual turn: %v", err)
	}
	if err := env.eng.waitReviewReadyForTest(run.ID, "impl", 10*time.Second); err != nil {
		t.Fatalf("wait manual turn: %v", err)
	}
	if env.provider.reviseCalls["impl"] != 1 {
		t.Fatalf("manual turn should run through ReviseInPlace, got %d", env.provider.reviseCalls["impl"])
	}
	if err := env.eng.ReactReply(run.ID, "impl", "确认", nil, nil, true); err != nil {
		t.Fatalf("confirm after fix: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "completed")
}

// TestTakeoverNextVisitIsAuto: manual applies to the taken-over visit only; a
// loop-back onto the same node opens a new visit that runs in auto again.
func TestTakeoverNextVisitIsAuto(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverLoopGraph())
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")
	if err := env.eng.ReactReply(run.ID, "impl", "确认", nil, nil, true); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	waitGateIteration(t, env.db, run.ID, "gate", 1)

	env.provider.releaseHold("impl")
	if err := env.eng.ResumeGate(run.ID, "gate", "revise", nil); err != nil {
		t.Fatalf("revise: %v", err)
	}
	waitGateIteration(t, env.db, run.ID, "gate", 2)

	sr := latestVisit(t, env.db, run.ID, "impl")
	if sr.Iteration != 2 || sr.IsManual() || sr.Status != "completed" {
		t.Fatalf("second visit must run in auto: iter=%d mode=%q status=%q", sr.Iteration, sr.Mode, sr.Status)
	}
	var convs int64
	env.db.Model(&models.ReactConversation{}).Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "impl", 2).Count(&convs)
	if convs != 0 {
		t.Fatalf("second visit must not open a manual conversation, got %d", convs)
	}
	var first models.StateRun
	env.db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "impl", 1).First(&first)
	if !first.IsManual() {
		t.Fatal("first visit must stay manual (never back to auto)")
	}
	env.provider.mu.Lock()
	calls := env.provider.runAgentCalls["impl"]
	env.provider.mu.Unlock()
	if calls != 2 {
		t.Fatalf("runAgentCalls=%d, want 2", calls)
	}
}

// insertRunningVisit creates a running run whose impl visit is in flight
// without a driver, for deterministic CAS tests.
func insertRunningVisit(t *testing.T, env takeoverEnv, mode string) models.Run {
	t.Helper()
	run := models.Run{ID: "run-cas", WorkflowID: "wf", Status: "running", Graph: takeoverPlainGraph(capsPlain), StartedAt: time.Now()}
	if err := env.db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := env.db.Create(&models.StateRun{RunID: run.ID, NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "running", Mode: mode}).Error; err != nil {
		t.Fatalf("create state run: %v", err)
	}
	return run
}

// TestTakeoverRaceCompletionWins: RunAgent returned and settled first, so a
// late takeover is refused with node_finishing and the visit stays auto.
func TestTakeoverRaceCompletionWins(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, "auto")
	c, err := env.eng.loadCtx(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	node := c.graph.FindNode("impl")
	ctx, ar := env.eng.beginAutoAgent(c, node)
	if env.eng.settleAutoAgent(run.ID, "impl", ar) {
		t.Fatal("no takeover happened; settle must report false")
	}
	if ctx.Err() == nil {
		t.Fatal("settle must release the RunAgent ctx")
	}
	_, err = env.eng.TakeoverNode(run.ID, "impl", 0)
	if code := takeoverCode(t, err); code != TakeoverNodeFinishing {
		t.Fatalf("code=%s, want node_finishing", code)
	}
	if sr := latestVisit(t, env.db, run.ID, "impl"); sr.IsManual() {
		t.Fatal("a refused takeover must not flip the visit to manual")
	}
}

// TestTakeoverRaceTakeoverWinsBeforeSettle: the takeover CAS lands first, so
// settle reports true and the cancel cause is ErrNodeTakenOver.
func TestTakeoverRaceTakeoverWinsBeforeSettle(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, "auto")
	c, _ := env.eng.loadCtx(run.ID)
	ctx, ar := env.eng.beginAutoAgent(c, c.graph.FindNode("impl"))
	res, err := env.eng.TakeoverNode(run.ID, "impl", 1)
	if err != nil || res.Status != TakeoverStatusTakenOver {
		t.Fatalf("takeover: %+v %v", res, err)
	}
	if ctx.Err() == nil {
		t.Fatal("takeover must cancel the RunAgent ctx")
	}
	if !env.eng.settleAutoAgent(run.ID, "impl", ar) {
		t.Fatal("settle after a won takeover must report true")
	}
	if sr := latestVisit(t, env.db, run.ID, "impl"); !sr.IsManual() || sr.TakenOverAt == nil {
		t.Fatalf("visit must be manual: %+v", sr)
	}
}

// TestTakeoverWinsEvenIfAgentSucceeded: the agent's turn completes at the
// same instant as the takeover (RunAgent returns success); the visit is
// still manual — the user's intervention is honoured.
func TestTakeoverWinsEvenIfAgentSucceeded(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	env.provider.runIgnoreCancel = true
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	env.provider.releaseHold("impl")
	waitRunStatus(t, env.db, run.ID, "waiting_human")
	if sr := latestVisit(t, env.db, run.ID, "impl"); !sr.IsManual() || sr.Status != "waiting_human" {
		t.Fatalf("visit must be manual waiting_human: %+v", sr)
	}
	var conv models.ReactConversation
	if err := env.db.Where("run_id = ? AND node_id = ?", run.ID, "impl").First(&conv).Error; err != nil || !conv.Takeover {
		t.Fatalf("takeover conversation missing: %v %+v", err, conv)
	}
}

// TestTakeoverIdempotent: repeating the takeover returns already_manual and
// cancels only once; a stale iteration is refused.
func TestTakeoverIdempotent(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	env.provider.runIgnoreCancel = true // keep it in "stopping"
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	first, err := env.eng.TakeoverNode(run.ID, "impl", 0)
	if err != nil || first.Status != TakeoverStatusTakenOver {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := env.eng.TakeoverNode(run.ID, "impl", 1)
	if err != nil || second.Status != TakeoverStatusAlreadyManual || second.State != NodeStateStopping {
		t.Fatalf("second: %+v %v", second, err)
	}
	env.provider.releaseHold("impl")
	waitRunStatus(t, env.db, run.ID, "waiting_human")
	third, err := env.eng.TakeoverNode(run.ID, "impl", 0)
	if err != nil || third.Status != TakeoverStatusAlreadyManual || third.State != NodeStateWaitingHuman {
		t.Fatalf("third: %+v %v", third, err)
	}
	_, err = env.eng.TakeoverNode(run.ID, "impl", 7)
	if code := takeoverCode(t, err); code != TakeoverNodeNotCurrent {
		t.Fatalf("stale iteration code=%s", code)
	}
	var audits int64
	env.db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "impl").Count(&audits)
	if audits != 1 {
		t.Fatalf("takeover must not open extra visits, got %d", audits)
	}
}

// TestTakeoverAfterCancelRefused: a cancelled run cannot be taken over.
func TestTakeoverAfterCancelRefused(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	if err := env.eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	_, err = env.eng.TakeoverNode(run.ID, "impl", 0)
	if code := takeoverCode(t, err); code != TakeoverRunNotRunning {
		t.Fatalf("code=%s, want run_not_running", code)
	}
}

// TestCancelWhileTakeoverStopping: the run is cancelled while the taken-over
// agent is still unwinding; the late outcome is dropped and the run stays
// cancelled (never resurrected to waiting_human).
func TestCancelWhileTakeoverStopping(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	env.provider.runIgnoreCancel = true
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if err := env.eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	env.provider.releaseHold("impl")
	waitRunStatus(t, env.db, run.ID, "cancelled")
	time.Sleep(200 * time.Millisecond)
	var r models.Run
	env.db.First(&r, "id = ?", run.ID)
	if r.Status != "cancelled" {
		t.Fatalf("run resurrected to %q", r.Status)
	}
	if sr := latestVisit(t, env.db, run.ID, "impl"); sr.Status == "waiting_human" {
		t.Fatalf("cancelled run's visit must not park: %+v", sr)
	}
}

// TestRestartParksTakenOverVisitManual: the process died between the takeover
// CAS and the conversion (row manual + running). Startup reconciliation must
// not adopt or re-run it in auto; it becomes a manual waiting_human session
// (cold sandbox) that can still be confirmed.
func TestRestartParksTakenOverVisitManual(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, "manual")

	eng2 := New(env.db, env.provider, env.host, env.arts, 5)
	t.Cleanup(eng2.Close)

	sr := latestVisit(t, env.db, run.ID, "impl")
	if !sr.IsManual() || sr.Status != "waiting_human" {
		t.Fatalf("restart must park the manual visit: %+v", sr)
	}
	var r models.Run
	env.db.First(&r, "id = ?", run.ID)
	if r.Status != "waiting_human" {
		t.Fatalf("run status=%q, want waiting_human", r.Status)
	}
	var conv models.ReactConversation
	if err := env.db.Where("run_id = ? AND node_id = ?", run.ID, "impl").First(&conv).Error; err != nil || !conv.Takeover || conv.Done {
		t.Fatalf("restart must seed an open takeover conversation: %v %+v", err, conv)
	}
	env.provider.mu.Lock()
	calls := env.provider.runAgentCalls["impl"]
	env.provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("restart must not re-run the visit in auto (RunAgent calls=%d)", calls)
	}
	if err := eng2.ReactReply(run.ID, "impl", "确认", nil, nil, true); err != nil {
		t.Fatalf("confirm after restart: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "completed")
}

// TestTakeoverRefusals covers the typed 404/409 refusals.
func TestTakeoverRefusals(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverLoopGraph())
	run := models.Run{ID: "run-ref", WorkflowID: "wf", Status: "running", Graph: takeoverLoopGraph(), StartedAt: time.Now()}
	env.db.Create(&run)
	env.db.Create(&models.StateRun{RunID: run.ID, NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "completed"})
	done := models.Run{ID: "run-done", WorkflowID: "wf", Status: "completed", Graph: takeoverLoopGraph(), StartedAt: time.Now()}
	env.db.Create(&done)

	cases := []struct {
		run, node, want string
	}{
		{"nope", "impl", TakeoverRunNotFound},
		{run.ID, "nope", TakeoverNodeNotFound},
		{run.ID, "gate", TakeoverNotAutoNode},
		{run.ID, "input", TakeoverNotAutoNode},
		{run.ID, "impl", TakeoverNodeNotCurrent},
		{done.ID, "impl", TakeoverRunNotRunning},
	}
	for _, tc := range cases {
		_, err := env.eng.TakeoverNode(tc.run, tc.node, 0)
		if got := takeoverCode(t, err); got != tc.want {
			t.Fatalf("%s/%s: code=%s want %s", tc.run, tc.node, got, tc.want)
		}
	}
	env.eng.Halt()
	_, err := env.eng.TakeoverNode(run.ID, "impl", 0)
	if got := takeoverCode(t, err); got != TakeoverServerHalting {
		t.Fatalf("halted code=%s", got)
	}
}

// TestTakeoverClarifyNodeRefused: clarify Agents are manual by nature.
func TestTakeoverClarifyNodeRefused(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsClarify))
	run := models.Run{ID: "run-cl", WorkflowID: "wf", Status: "running", Graph: takeoverPlainGraph(capsClarify), StartedAt: time.Now()}
	env.db.Create(&run)
	_, err := env.eng.TakeoverNode(run.ID, "impl", 0)
	if got := takeoverCode(t, err); got != TakeoverNotAutoNode {
		t.Fatalf("code=%s", got)
	}
}

// TestTakeoverReviewNodeInReviewIsAlreadyManual: a review node parked in its
// post-run review is already a human session.
func TestTakeoverReviewNodeInReviewIsAlreadyManual(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	run, err := eng.StartRun("review-wf", map[string]any{"idea": "x"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")
	res, err := eng.TakeoverNode(run.ID, "prop", 0)
	if err != nil || res.Status != TakeoverStatusAlreadyManual || res.State != NodeStateWaitingHuman {
		t.Fatalf("got %+v %v", res, err)
	}
}

// TestTakeoverReviewNodeDuringAutoPhase: a review-enabled node can be taken
// over while its auto phase runs; it parks as a takeover session.
func TestTakeoverReviewNodeDuringAutoPhase(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsResearch))
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")
	var conv models.ReactConversation
	if err := env.db.Where("run_id = ? AND node_id = ?", run.ID, "impl").First(&conv).Error; err != nil || !conv.Takeover {
		t.Fatalf("want takeover conversation: %v %+v", err, conv)
	}
}

// TestStateRunSaveDoesNotClobberManualMode: a StateRun copy loaded before a
// concurrent takeover is saved afterwards (e.g. artifact sync); mode stays
// manual.
func TestStateRunSaveDoesNotClobberManualMode(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, "auto")
	stale := latestVisit(t, env.db, run.ID, "impl")
	env.db.Model(&models.StateRun{}).Where("id = ?", stale.ID).Updates(map[string]any{"mode": "manual", "taken_over_at": time.Now()})
	stale.OutputMd = "synced"
	if err := env.eng.db.Omit(stateRunTakeoverCols...).Save(&stale).Error; err != nil {
		t.Fatal(err)
	}
	got := latestVisit(t, env.db, run.ID, "impl")
	if !got.IsManual() || got.TakenOverAt == nil || got.OutputMd != "synced" {
		t.Fatalf("stale save clobbered takeover: %+v", got)
	}
	// Fresh inserts through the same path still default to auto.
	fresh := models.StateRun{RunID: run.ID, NodeID: "output", Iteration: 1, Status: "running"}
	env.eng.db.Omit(stateRunTakeoverCols...).Save(&fresh)
	if latestVisit(t, env.db, run.ID, "output").IsManual() {
		t.Fatal("new visits must default to auto")
	}
}

// TestTouchActivityDebounce: ACP-driven touches write at most once per run
// per activityDebounce; forced touches always write.
func TestTouchActivityDebounce(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	run := insertRunningVisit(t, env, "auto")
	t0 := time.Date(2026, 10, 21, 5, 0, 0, 0, time.UTC)
	now := t0
	tr := env.eng.activity()
	tr.mu.Lock()
	tr.now = func() time.Time { return now }
	tr.mu.Unlock()

	stamp := func() time.Time {
		var r models.Run
		env.db.First(&r, "id = ?", run.ID)
		if r.LastActivityAt == nil {
			return time.Time{}
		}
		return r.LastActivityAt.UTC()
	}
	msg := []models.AcpEvent{{Kind: "message", Text: "hi"}}

	env.eng.publishAcp(run.ID, "impl", msg, true)
	if !stamp().Equal(t0) {
		t.Fatalf("first ACP event must stamp, got %v", stamp())
	}
	now = t0.Add(1 * time.Second)
	env.eng.publishAcp(run.ID, "impl", []models.AcpEvent{{Kind: "tool_call", Title: "go test"}}, true)
	now = t0.Add(4 * time.Second)
	env.eng.publishAcp(run.ID, "impl", msg, true)
	if !stamp().Equal(t0) {
		t.Fatalf("events inside the 5s window must not write, got %v", stamp())
	}
	now = t0.Add(6 * time.Second)
	env.eng.publishAcp(run.ID, "impl", []models.AcpEvent{{Kind: "liveness"}}, true)
	if !stamp().Equal(t0) {
		t.Fatal("non-activity frames (liveness) must not bump lastActivityAt")
	}
	env.eng.publishAcp(run.ID, "impl", msg, true)
	if !stamp().Equal(t0.Add(6 * time.Second)) {
		t.Fatalf("event after the window must write, got %v", stamp())
	}
	now = t0.Add(7 * time.Second)
	env.eng.touchActivity(run.ID, true)
	if !stamp().Equal(t0.Add(7 * time.Second)) {
		t.Fatalf("forced touch must always write, got %v", stamp())
	}
	now = t0.Add(8 * time.Second)
	env.eng.publishAcp(run.ID, "impl", msg, true)
	if !stamp().Equal(t0.Add(7 * time.Second)) {
		t.Fatal("forced touch restarts the debounce window")
	}
}

// TestManualSessionBusyFromBridge: busy reflects a turn the bridge reports.
func TestManualSessionBusyFromBridge(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	if env.eng.ManualSessionBusy("r", "impl") {
		t.Fatal("idle session reported busy")
	}
	env.provider.mu.Lock()
	env.provider.sandboxBusy = true
	env.provider.mu.Unlock()
	if !env.eng.ManualSessionBusy("r", "impl") {
		t.Fatal("bridge-busy session must report busy")
	}
}

// TestManualSessionBusyDuringTakeoverTurn: while a manual turn of a taken-over
// node runs, the session reports busy (currentNode.state=busy); idle after.
func TestManualSessionBusyDuringTakeoverTurn(t *testing.T) {
	env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
	hold := make(chan struct{})
	env.provider.reviseHold = hold
	run, err := env.eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitStarted(t, env.provider)
	if _, err := env.eng.TakeoverNode(run.ID, "impl", 0); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	waitRunStatus(t, env.db, run.ID, "waiting_human")
	if env.eng.ManualSessionBusy(run.ID, "impl") {
		t.Fatal("parked idle session must not be busy")
	}
	if err := env.eng.ReactReply(run.ID, "impl", "改一下", nil, nil, false); err != nil {
		t.Fatalf("manual turn: %v", err)
	}
	deadline := time.Now().Add(waitPollTimeout)
	for !env.eng.ManualSessionBusy(run.ID, "impl") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !env.eng.ManualSessionBusy(run.ID, "impl") {
		t.Fatal("running manual turn must report busy")
	}
	close(hold)
	if err := env.eng.waitReviewReadyForTest(run.ID, "impl", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if env.eng.ManualSessionBusy(run.ID, "impl") {
		t.Fatal("session must be idle after the turn")
	}
}
