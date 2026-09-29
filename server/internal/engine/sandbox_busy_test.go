package engine

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
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

func appendCompletedAgent(t *testing.T, db *gorm.DB, runID string, msg models.ReactMessage) {
	t.Helper()
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", runID, "clarify").
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	conv.Messages = append(conv.Messages,
		models.ReactMessage{Role: "human", Text: "继续", At: "t1"},
		msg,
	)
	if err := db.Save(&conv).Error; err != nil {
		t.Fatalf("save conv: %v", err)
	}
}

func TestAttachSandboxStateDropsMatchedCompletedTurn(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	appendCompletedAgent(t, db, run.ID, models.ReactMessage{
		Role: "agent", Text: "最新回复已落盘", OpID: "g-00fccbf7-0f2",
	})
	provider.sandboxBusy = true
	provider.sandboxDesynced = true
	provider.sandboxRunningOp = "g-00fccbf7-0f2"
	provider.sandboxLastDone = "g-00fccbf7-0f2"

	payload := map[string]any{"waiting": 0, "items": []any{}, "busy": false}
	eng.attachSandboxState(run.ID, "clarify", payload)
	if busy, _ := payload["sandboxBusy"].(bool); busy {
		t.Fatalf("plan g1.1: matched completed turn still busy: %#v", payload)
	}
	if des, _ := payload["sandboxDesynced"].(bool); des {
		t.Fatalf("plan g1.1: matched completed turn still desynced: %#v", payload)
	}
	if _, ok := payload["sandboxRunningOpId"]; ok {
		t.Fatalf("plan g1.1: completed op still marked running: %#v", payload)
	}
	if err := eng.ensureSandboxIdleForConfirm(run.ID, "clarify", false); err != nil {
		t.Fatalf("plan g1.1: confirm still sees the finished op: %v", err)
	}
}

func TestAttachSandboxStateKeepsOtherOrphanTurn(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	appendCompletedAgent(t, db, run.ID, models.ReactMessage{
		Role: "agent", Text: "历史已完成", OpID: "g-done",
	})
	provider.sandboxBusy = true
	provider.sandboxRunningOp = "g-other"
	provider.sandboxLastDone = "g-done"

	payload := map[string]any{"waiting": 0, "items": []any{}, "busy": false}
	eng.attachSandboxState(run.ID, "clarify", payload)
	if busy, _ := payload["sandboxBusy"].(bool); !busy {
		t.Fatalf("plan g2.2: other unfinished op must stay busy: %#v", payload)
	}
	if payload["sandboxRunningOpId"] != "g-other" {
		t.Fatalf("plan g2.2: orphan op = %#v", payload["sandboxRunningOpId"])
	}
	if err := eng.ensureSandboxIdleForConfirm(run.ID, "clarify", false); !errors.Is(err, ErrSandboxBusy) {
		t.Fatalf("plan g2.2: other op must still block confirm, got %v", err)
	}
}

func TestAttachSandboxStateKeepsLiveAndInterrupted(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	appendCompletedAgent(t, db, run.ID, models.ReactMessage{
		Role: "agent", Text: "半截", OpID: "g-00fccbf7-0f2", Interrupted: true,
	})
	provider.sandboxBusy = true
	provider.sandboxRunningOp = "g-00fccbf7-0f2"
	provider.sandboxLastDone = "g-00fccbf7-0f2"

	live := map[string]any{"waiting": 0, "busy": true, "activeItem": map[string]any{"text": "继续"}}
	eng.attachSandboxState(run.ID, "clarify", live)
	if busy, _ := live["sandboxBusy"].(bool); !busy {
		t.Fatalf("in-flight replay must keep sandbox busy: %#v", live)
	}

	idle := map[string]any{"waiting": 0, "busy": false}
	eng.attachSandboxState(run.ID, "clarify", idle)
	if busy, _ := idle["sandboxBusy"].(bool); !busy {
		t.Fatalf("interrupted tail must keep the orphan flag: %#v", idle)
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
