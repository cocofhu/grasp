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

// Same interleaving as the flaky CI test: cancel, ResumeFrom, then the first
// visit's turn returns. The resumed run must reach the new visit's pause.
func TestClarifyTurnInFlightAtCancelDoesNotFailResumedRun(t *testing.T) {
	eng, run, hold, _ := startHeldFirstMessage(t)
	db := eng.db
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	if err := eng.ResumeFrom(run.ID, "predev"); err != nil {
		t.Fatalf("ResumeFrom: %v", err)
	}
	close(hold)

	waitRunStatus(t, db, run.ID, "waiting_human")
	deadline := time.Now().Add(waitPollTimeout)
	for {
		var conv models.ReactConversation
		err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&conv).Error
		if err == nil && conv.Iteration >= 2 && hasHumanTurn(conv.Messages) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first message never redelivered into the resumed visit (err=%v)", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := eng.runStatus(run.ID); st == "failed" {
		t.Fatal("stale first-visit turn failed the resumed run")
	}
}
