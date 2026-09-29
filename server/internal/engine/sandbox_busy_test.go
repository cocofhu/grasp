package engine

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// inflightEchoProvider sends the confirm gate through a real ACPClient.
// The fake that started the run still serves every other provider method.
type inflightEchoProvider struct {
	*fakeProvider
	acp *sandbox.ACPClient
}

func (p *inflightEchoProvider) SessionBridgeState(runID, nodeID string) (runtime.BridgeStatus, bool) {
	return runtime.ClientBridgeStatus(p.acp)
}

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

func TestEnsureSandboxIdleRejectsInFlightAfterCompletedEcho(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	appendCompletedAgent(t, db, run.ID, models.ReactMessage{
		Role: "agent", Text: "最新回复已落盘", OpID: "g-00fccbf7-0f2",
	})
	acp := sandbox.NewACPClient("127.0.0.1", 1)
	// Bridge still echoes the finished op; the next chat has already entered runTurn.
	acp.SeedBridgeForTest(sandbox.BridgeState{
		Known: true, Busy: true, Desynced: true,
		RunningOpID: "g-00fccbf7-0f2", Waiting: 0,
	}, "g-00fccbf7-0f2", "g-next")
	raw := acp.BridgeState()
	if raw.Busy || raw.RunningOpID != "" || raw.Desynced {
		t.Fatalf("plan g1.1: lastDone echo should look idle before the in-flight overlay: %+v", raw)
	}
	eng.provider = &inflightEchoProvider{fakeProvider: provider, acp: acp}

	err = eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, false)
	if !errors.Is(err, ErrSandboxBusy) {
		t.Fatalf("plan g1.1: in-flight turn must still block confirm, got %v", err)
	}
	var busy *SandboxBusyError
	if !errors.As(err, &busy) || busy.RunningOpID != "g-next" {
		t.Fatalf("plan g1.1: want in-flight op g-next, got %+v", err)
	}
	provider.mu.Lock()
	calls := provider.reactReplyCalls["clarify"]
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("plan g1.1: confirm must not start while the next turn is in flight; reactReplyCalls=%d", calls)
	}
}

func TestEnsureSandboxIdleKeepsUnnamedDesync(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	appendCompletedAgent(t, db, run.ID, models.ReactMessage{
		Role: "agent", Text: "最新回复已落盘", OpID: "g-00fccbf7-0f2",
	})
	// review v1/v2: finishTurn already wrote lastDone. An empty running op
	// with that same completed id is unnamed busy/desync, not a finished echo.
	provider.sandboxBusy = true
	provider.sandboxDesynced = true
	provider.sandboxLastDone = "g-00fccbf7-0f2"

	payload := map[string]any{"waiting": 0, "items": []any{}, "busy": false}
	eng.attachSandboxState(run.ID, "clarify", payload)
	if busy, _ := payload["sandboxBusy"].(bool); !busy {
		t.Fatalf("plan g1.2 review v1: unnamed desync with lastDone set must stay sandboxBusy: %#v", payload)
	}
	if des, _ := payload["sandboxDesynced"].(bool); !des {
		t.Fatalf("plan g1.2 review v1: unnamed desync must stay desynced: %#v", payload)
	}
	if _, ok := payload["sandboxRunningOpId"]; ok {
		t.Fatalf("plan g1.2: unnamed desync has no op to point at: %#v", payload)
	}
	if err := eng.ensureSandboxIdleForConfirm(run.ID, "clarify", false); !errors.Is(err, ErrSandboxBusy) {
		t.Fatalf("plan g1.2 review v1: unnamed desync with lastDone set must still block confirm, got %v", err)
	}
}

func TestAttachSandboxStateKeepsBusyWhileBridgeWaiting(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	appendCompletedAgent(t, db, run.ID, models.ReactMessage{
		Role: "agent", Text: "最新回复已落盘", OpID: "g-00fccbf7-0f2",
	})
	// review v1: Waiting > 0 means a turn is still queued. Matching the
	// completed op must not drop the busy flag.
	provider.sandboxBusy = true
	provider.sandboxDesynced = true
	provider.sandboxRunningOp = "g-00fccbf7-0f2"
	provider.sandboxLastDone = "g-00fccbf7-0f2"
	provider.sandboxWaiting = 1

	payload := map[string]any{"waiting": 0, "items": []any{}, "busy": false}
	eng.attachSandboxState(run.ID, "clarify", payload)
	if busy, _ := payload["sandboxBusy"].(bool); !busy {
		t.Fatalf("review v1: waiting sandbox queue must stay sandboxBusy: %#v", payload)
	}
	if payload["sandboxRunningOpId"] != "g-00fccbf7-0f2" {
		t.Fatalf("review v1: waiting turn op = %#v", payload["sandboxRunningOpId"])
	}
	if w, _ := payload["sandboxWaiting"].(int); w != 1 {
		t.Fatalf("review v1: sandboxWaiting = %#v", payload["sandboxWaiting"])
	}
	if err := eng.ensureSandboxIdleForConfirm(run.ID, "clarify", false); !errors.Is(err, ErrSandboxBusy) {
		t.Fatalf("review v1: waiting sandbox queue must still block confirm, got %v", err)
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
