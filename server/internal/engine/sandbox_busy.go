package engine

import (
	"errors"

	"github.com/rs/zerolog/log"

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
	if !live || !(st.Busy || st.Desynced) {
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
	payload["sandboxBusy"] = st.Busy
	payload["sandboxDesynced"] = st.Desynced
	payload["sandboxWaiting"] = st.Waiting
	if st.RunningOpID != "" {
		payload["sandboxRunningOpId"] = st.RunningOpID
	}
}
