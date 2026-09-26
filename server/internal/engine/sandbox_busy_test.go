package engine

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestEnsureSandboxIdleRejectsBusyConfirm(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	provider.sandboxBusy = true
	provider.sandboxRunningOp = "oid-stuck"

	err = eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, false)
	if !errors.Is(err, ErrSandboxBusy) {
		t.Fatalf("want ErrSandboxBusy, got %v", err)
	}
	var busy *SandboxBusyError
	if !errors.As(err, &busy) || busy.RunningOpID != "oid-stuck" {
		t.Fatalf("busy payload: %+v", err)
	}
	provider.mu.Lock()
	calls := provider.reactReplyCalls["clarify"]
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("confirm must not start while sandbox busy; reactReplyCalls=%d", calls)
	}
}

func TestEnsureSandboxIdleAbortThenConfirm(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	provider.sandboxBusy = true
	provider.sandboxRunningOp = "oid-stuck"
	provider.abortOK = true

	if err := eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("abort+confirm: %v", err)
	}
	if provider.abortCalls != 1 {
		t.Fatalf("abortCalls=%d", provider.abortCalls)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	if !conv.Done {
		t.Fatalf("expected conversation done after abort+confirm")
	}
}

func TestEnsureSandboxIdleAbortFailed(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	provider.sandboxBusy = true
	provider.abortOK = false

	err = eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, true)
	if !errors.Is(err, errSandboxAbortFailed) {
		t.Fatalf("want abort-failed, got %v", err)
	}
}

func TestConfirmTurnTimeoutMarksInterrupted(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	provider.reactInterrupted = true

	err = eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, false)
	if !errors.Is(err, errConfirmTurnTimeout) {
		t.Fatalf("want timeout copy, got %v", err)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	if conv.Done {
		t.Fatalf("timeout must reopen the conversation")
	}
	var saw bool
	for _, m := range conv.Messages {
		if m.Role == "agent" && m.Interrupted {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected Interrupted agent: %+v", conv.Messages)
	}
}
