package engine

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// startHeldFirstMessage starts a clarify run whose opening-message turn is
// parked inside the provider, and returns once that turn is in flight.
func startHeldFirstMessage(t *testing.T) (*Engine, *models.Run, chan struct{}, func() bool) {
	t.Helper()
	eng, db, p := setupEngineGraphP(t, clarifyOnlyGraph())
	hold := make(chan struct{})
	p.mu.Lock()
	p.reactHold = hold
	p.mu.Unlock()
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", &models.CompositeText{Text: "把登录做清楚"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	busy := func() bool {
		s, ok := eng.ReviewSessionSnapshotFor(run.ID, "predev")
		return ok && s.Busy
	}
	// The human turn is persisted right before the provider call, so once it
	// is visible the turn is parked inside the provider on hold.
	waitFirstHumanTurn(t, db, run.ID, "predev")
	if !busy() {
		t.Fatal("first-message turn should be in flight")
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	return eng, run, hold, busy
}

func waitNotBusy(t *testing.T, busy func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for busy() {
		if time.Now().After(deadline) {
			t.Fatal("clarify turn never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Regression (flaky TestClarifyFirstMessageRedeliveredAfterCancelResume): a
// clarify turn in flight when the run is cancelled must not finish the
// cancelled visit and overwrite the run status with failed.
func TestClarifyTurnInFlightAtCancelKeepsRunCancelled(t *testing.T) {
	eng, run, hold, busy := startHeldFirstMessage(t)
	db := eng.db
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")

	close(hold)
	waitNotBusy(t, busy)

	if st := eng.runStatus(run.ID); st != "cancelled" {
		t.Fatalf("run status after the stale turn = %q, want cancelled", st)
	}
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("id desc").First(&sr).Error; err != nil {
		t.Fatalf("state run: %v", err)
	}
	if sr.Status != "cancelled" {
		t.Fatalf("visit status = %q, want cancelled", sr.Status)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	if conv.Done {
		t.Fatal("stale turn must not close the cancelled visit's conversation")
	}
	if n := len(conv.Messages); n == 0 || conv.Messages[n-1].Role != "agent" || !conv.Messages[n-1].Interrupted {
		t.Fatalf("stale reply should stay in the transcript marked interrupted: %+v", conv.Messages)
	}
}

// Same interleaving as the flaky CI test: cancel, ResumeFrom (the new visit
// parks), then the first visit's turn returns. The stale turn must neither
// fail the resumed run nor charge its tokens to the new visit.
func TestClarifyTurnInFlightAtCancelDoesNotFailResumedRun(t *testing.T) {
	eng, run, hold, busy := startHeldFirstMessage(t)
	db := eng.db
	p := eng.provider.(*fakeProvider)
	p.mu.Lock()
	p.reactUsage = &models.TokenUsage{OutputTokens: 7}
	p.mu.Unlock()
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	if err := eng.ResumeFrom(run.ID, "predev"); err != nil {
		t.Fatalf("ResumeFrom: %v", err)
	}
	// The new visit parks while the first visit's turn is still held.
	waitRunStatus(t, db, run.ID, "waiting_human")
	close(hold)

	// Let the stale turn and the redelivered first message both finish
	// before asserting: the stale turn's damage lands after the provider
	// returns, not when the new visit parks.
	var conv models.ReactConversation
	deadline := time.Now().Add(waitPollTimeout)
	for {
		waitNotBusy(t, busy)
		conv = models.ReactConversation{}
		err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&conv).Error
		if err == nil && conv.Iteration >= 2 && hasHumanTurn(conv.Messages) && conv.Messages[len(conv.Messages)-1].Role == "agent" && !busy() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first message never answered in the resumed visit (err=%v, conv=%+v)", err, conv)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if st := eng.runStatus(run.ID); st != "waiting_human" {
		t.Fatalf("run status after the stale first-visit turn = %q, want waiting_human", st)
	}
	humans := 0
	for _, m := range conv.Messages {
		if m.Role == "human" {
			humans++
		}
	}
	if humans != 1 {
		t.Fatalf("resumed visit has %d human turns, want exactly 1: %+v", humans, conv.Messages)
	}
	var old models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "predev", 1).First(&old).Error; err != nil {
		t.Fatalf("first visit conv: %v", err)
	}
	if n := len(old.Messages); n == 0 || !old.Messages[n-1].Interrupted || old.Done {
		t.Fatalf("stale reply should stay in the first visit marked interrupted: done=%v %+v", old.Done, old.Messages)
	}
	for iter, want := range map[int]int64{1: 7, 2: 7} {
		var sr models.StateRun
		if err := db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "predev", iter).Order("id desc").First(&sr).Error; err != nil {
			t.Fatalf("state run %d: %v", iter, err)
		}
		var got int64
		if sr.Usage != nil {
			got = sr.Usage.OutputTokens
		}
		if got != want {
			t.Fatalf("visit %d output tokens = %d, want %d (each turn charges its own visit)", iter, got, want)
		}
	}
}

// A Live request still queued when the run is cancelled is dropped when its
// turn starts. It must not stay generating: Live sessions are per run+node, so
// an open one would block the resumed visit's confirm.
func TestClarifyLiveRequestQueuedAtCancelIsSettled(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, liveGraph(capsClarify))
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	hold := make(chan struct{})
	p.mu.Lock()
	p.reactHold = hold
	p.mu.Unlock()
	if err := eng.ReactReplyAs("user:a", run.ID, "preview", "先检查页面", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.ReactLiveAs("user:a", run.ID, "preview", models.LiveEvent{Op: models.LiveOpGenerate, SID: "page01", Scope: "page", Prompt: "给页面三个方案"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := eng.liveSession(run.ID, "preview", "page01"); s == nil || s.State != models.LiveStateGenerating {
		t.Fatalf("live request should be queued as generating: %+v", s)
	}
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	close(hold)
	if err := eng.waitReviewReadyForTest(run.ID, "preview", waitPollTimeout); err != nil {
		t.Fatal(err)
	}

	sess := waitLiveState(t, eng, run.ID, "page01", models.LiveStateFailed)
	if sess.Error == "" {
		t.Fatal("dropped live request should say why it failed")
	}
	if st := eng.runStatus(run.ID); st != "cancelled" {
		t.Fatalf("run status = %q, want cancelled", st)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "preview").Order("iteration desc").First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range conv.Messages {
		if m.Live != nil {
			t.Fatalf("dropped live request must not be written into the ended visit: %+v", conv.Messages)
		}
	}

	// After ResumeFrom the failed request can be dismissed like any request
	// cancelled in the queue, so nothing blocks the new visit's confirm.
	if err := eng.ResumeFrom(run.ID, "preview"); err != nil {
		t.Fatalf("ResumeFrom: %v", err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := eng.waitReviewReadyForTest(run.ID, "preview", waitPollTimeout); err != nil {
		t.Fatal(err)
	}
	if n, err := eng.DiscardAllLiveAs("user:a", run.ID, "preview"); err != nil || n != 1 {
		t.Fatalf("dismiss dropped live request: n=%d err=%v", n, err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "preview", waitPollTimeout); err != nil {
		t.Fatal(err)
	}
	if open := eng.LiveSessions(run.ID, "preview", true); len(open) != 0 {
		t.Fatalf("no live session may stay open once dismissed: %+v", open)
	}
}
