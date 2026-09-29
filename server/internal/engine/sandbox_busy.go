package engine

import (
	"errors"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// ErrSandboxBusy: the node's sandbox is still running an agent turn the
// platform no longer tracks (typically one it gave up on after a timeout), so
// a confirm would queue behind it. Match with errors.Is.
var ErrSandboxBusy = errors.New("sandbox_busy")

// SandboxBusyError carries what the bridge reports as running.
type SandboxBusyError struct {
	RunningOpID string
	Desynced    bool
}

func (e *SandboxBusyError) Error() string {
	return "沙箱内仍有未结束的 Agent 回合，确认会排在它后面无法执行；请中止当前回合后再确认"
}

func (e *SandboxBusyError) Is(target error) bool { return target == ErrSandboxBusy }

// errSandboxAbortFailed is returned when abortRunning was requested but the
// bridge never confirmed the cancel.
var errSandboxAbortFailed = errors.New("中止沙箱内的 Agent 回合失败（沙箱无响应），请稍后重试")

// errConfirmTurnTimeout: the confirm turn itself was cut by a timeout (the
// sandbox watchdog or the platform idle window) before the agent wrapped up.
var errConfirmTurnTimeout = errors.New("确认回合超时（Agent 无响应），本轮已中断，请重试确认")

// ensureSandboxIdleForConfirm guards force confirm against a sandbox whose
// bridge is still busy while the platform FIFO is idle. With abortRunning the
// bridge's running turn is cancelled (and acknowledged) first.
func (e *Engine) ensureSandboxIdleForConfirm(runID, nodeID string, abortRunning bool) error {
	insp, ok := e.provider.(runtime.SessionBridgeInspector)
	if !ok {
		return nil
	}
	st, live := insp.SessionBridgeState(runID, nodeID)
	if !live {
		return nil
	}
	// A finished turn the bridge still echoes is not an orphan in front of confirm.
	st = e.dropCompletedSandboxEcho(runID, nodeID, st)
	if !(st.Busy || st.Desynced) {
		return nil
	}
	if !abortRunning {
		return &SandboxBusyError{RunningOpID: st.RunningOpID, Desynced: st.Desynced}
	}
	log.Warn().Str("run", runID).Str("node", nodeID).Str("running_op", st.RunningOpID).
		Bool("desynced", st.Desynced).Msg("confirm: aborting orphan sandbox turn")
	if !insp.AbortSessionTurn(runID, nodeID) {
		return errSandboxAbortFailed
	}
	return nil
}

// SandboxBridgeState exposes the bridge's view of a parked session to
// handlers (ok=false when there is no live session or no bridge support).
func (e *Engine) SandboxBridgeState(runID, nodeID string) (runtime.BridgeStatus, bool) {
	insp, ok := e.provider.(runtime.SessionBridgeInspector)
	if !ok {
		return runtime.BridgeStatus{}, false
	}
	return insp.SessionBridgeState(runID, nodeID)
}

// attachSandboxState copies the bridge's own busy view onto a review frame so
// the dialogue UI can show an orphan-turn banner when the platform FIFO is idle.
func (e *Engine) attachSandboxState(runID, nodeID string, payload map[string]any) {
	st, ok := e.SandboxBridgeState(runID, nodeID)
	if !ok {
		return
	}
	// plan g1.1: on an idle replay, a normally completed assistant turn whose
	// op already matches the bridge must not be marked still running or
	// interrupted. A different running op stays so it can be aborted.
	if platformQueueIdle(payload) {
		st = e.dropCompletedSandboxEcho(runID, nodeID, st)
	}
	payload["sandboxBusy"] = st.Busy
	payload["sandboxDesynced"] = st.Desynced
	payload["sandboxWaiting"] = st.Waiting
	if st.RunningOpID != "" {
		payload["sandboxRunningOpId"] = st.RunningOpID
	}
}

func platformQueueIdle(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if busy, _ := payload["busy"].(bool); busy {
		return false
	}
	if payload["activeItem"] != nil {
		return false
	}
	waiting, ok := payload["waiting"]
	if !ok {
		return false
	}
	switch w := waiting.(type) {
	case int:
		return w == 0
	case int64:
		return w == 0
	case float64:
		return w == 0
	default:
		return false
	}
}

// dropCompletedSandboxEcho clears busy/desync when the bridge op is the
// assistant turn already persisted as a normal completion (plan g1.1).
func (e *Engine) dropCompletedSandboxEcho(runID, nodeID string, st runtime.BridgeStatus) runtime.BridgeStatus {
	completedOp, completed := e.latestNormallyCompletedAgentOp(runID, nodeID)
	if !sandboxEchoesCompletedTurn(st, completedOp, completed) {
		return st
	}
	st.Busy = false
	st.Desynced = false
	st.RunningOpID = ""
	return st
}

// latestNormallyCompletedAgentOp reports the newest persisted assistant reply
// when it finished normally. ok is false when the tail is a human, an
// interrupt, a handoff-only row, or empty. opID may be empty on older rows.
func (e *Engine) latestNormallyCompletedAgentOp(runID, nodeID string) (opID string, ok bool) {
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return "", false
	}
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		m := conv.Messages[i]
		if m.Role != "agent" {
			return "", false
		}
		if m.Handoff {
			continue
		}
		if m.Interrupted || strings.TrimSpace(m.Text) == "" {
			return "", false
		}
		return strings.TrimSpace(m.OpID), true
	}
	return "", false
}

func sandboxEchoesCompletedTurn(st runtime.BridgeStatus, completedOp string, completed bool) bool {
	if !completed || !(st.Busy || st.Desynced) {
		return false
	}
	running := strings.TrimSpace(st.RunningOpID)
	done := strings.TrimSpace(st.LastDoneOpID)
	completedOp = strings.TrimSpace(completedOp)
	if running == "" {
		return done != "" && (completedOp == "" || completedOp == done)
	}
	if completedOp != "" && running == completedOp {
		return true
	}
	return done != "" && running == done && (completedOp == "" || completedOp == done)
}
