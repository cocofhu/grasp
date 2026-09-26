package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/cocofhu/grasp/internal/models"
)

// stopReasonTimeout is the prompt_done stopReason of a turn the sandbox bridge
// watchdog aborted (idle / total-duration limit).
const stopReasonTimeout = "timeout"

// ErrSandboxTurnTimeout marks a turn the sandbox bridge itself aborted. It wraps
// ErrChatIdle so callers that retry stuck turns treat both the same.
var ErrSandboxTurnTimeout = fmt.Errorf("sandbox turn timeout: %w", ErrChatIdle)

// cancelAckWait bounds how long an aborted turn waits for the bridge to confirm
// the cancel. A var so tests can shrink it.
var cancelAckWait = 15 * time.Second

// BridgeState is the client-side mirror of the bridge's queue_state.
type BridgeState struct {
	// Known is false until the first queue_state frame arrives.
	Known       bool
	Busy        bool
	RunningOpID string
	Waiting     int
	// Desynced is set when a cancel this client sent was never acknowledged;
	// the bridge may still be running a turn this client gave up on. Cleared
	// as soon as the bridge reports it is idle.
	Desynced bool
}

// BridgeState returns the latest mirrored queue_state. A running turn this
// client already saw finish is reported idle: the bridge sends prompt_done
// before the matching queue_state, so the mirror briefly lags.
func (c *ACPClient) BridgeState() BridgeState {
	c.stateMu.Lock()
	st := c.bridge
	c.stateMu.Unlock()
	if st.Busy && st.Waiting == 0 && st.RunningOpID != "" && st.RunningOpID == loadOpID(&c.lastDoneOpID) {
		st.Busy = false
		st.RunningOpID = ""
	}
	return st
}

// TurnInFlight reports whether a chat is currently running on this client.
func (c *ACPClient) TurnInFlight() bool {
	return loadOpID(&c.turnOpID) != ""
}

func loadOpID(v *atomic.Value) string {
	s, _ := v.Load().(string)
	return s
}

// AbortRunning cancels whatever turn the bridge is running and waits for the
// confirmation. When a chat of this client is in flight, its own loop owns the
// event stream and finishes the turn, so only the cancel is sent.
func (c *ACPClient) AbortRunning(wait time.Duration) bool {
	if op := loadOpID(&c.turnOpID); op != "" {
		return c.send(map[string]any{"op": "cancel", "opId": op}) == nil
	}
	if !c.CancelTurnAndWait(c.BridgeState().RunningOpID, wait) {
		return false
	}
	c.stateMu.Lock()
	c.bridge.Desynced = false
	c.stateMu.Unlock()
	return true
}

func (c *ACPClient) setDesynced() {
	c.stateMu.Lock()
	c.bridge.Desynced = true
	c.stateMu.Unlock()
}

// observeQueueState updates the bridge mirror from a queue_state frame. Called
// from readLoop for every frame, so it must stay cheap for everything else.
func (c *ACPClient) observeQueueState(raw []byte) {
	if !bytes.Contains(raw, []byte(`"queue_state"`)) {
		return
	}
	var m struct {
		Op          string `json:"op"`
		Busy        *bool  `json:"busy"`
		QueueLength int    `json:"queue_length"`
		Running     *struct {
			OpID string `json:"opId"`
		} `json:"running"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Op != "queue_state" || m.Busy == nil {
		return
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.bridge.Known = true
	c.bridge.Busy = *m.Busy
	c.bridge.Waiting = m.QueueLength
	c.bridge.RunningOpID = ""
	if m.Running != nil {
		c.bridge.RunningOpID = m.Running.OpID
	}
	if !*m.Busy && m.QueueLength == 0 {
		c.bridge.Desynced = false
	}
}

func newTurnOpID() string {
	return "g-" + uuid.NewString()[:12]
}

type turnFrame struct {
	Op     string          `json:"op"`
	OpID   string          `json:"opId"`
	Status string          `json:"status"`
	Busy   *bool           `json:"busy"`
	Data   json.RawMessage `json:"data"`
}

func parseTurnFrame(raw json.RawMessage) turnFrame {
	var f turnFrame
	_ = json.Unmarshal(raw, &f)
	return f
}

func (f turnFrame) dataType() string {
	var d struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(f.Data, &d)
	return d.Type
}

// belongsTo reports whether f is part of the turn opID. Tagged frames match by
// opId. Untagged event frames only count against a legacy bridge that never
// tags; untagged errors (a rejected chat, sent only to this connection) and
// queue_state (global) always count.
func (c *ACPClient) belongsTo(f turnFrame, opID string) bool {
	if f.OpID != "" {
		c.opIDTagged.Store(true)
		return f.OpID == opID
	}
	switch f.Op {
	case "event":
		return !c.opIDTagged.Load()
	case "error", "queue_state":
		return true
	}
	return false
}

// runTurn sends one {op:chat} tagged with a fresh opId and aggregates the frames
// of that turn into a ChatResult. onEvent (optional) receives the raw frames;
// onProgress (optional) the in-progress aggregate after each frame.
//
// A turn that stops producing frames for the idle window, or whose ctx ends,
// is cancelled on the bridge and the client waits for the acknowledgement so
// both sides agree the turn is over. With partial content the result is
// returned with Interrupted set and the reason in ErrorText.
func (c *ACPClient) runTurn(ctx context.Context, text string, images []models.PromptImage,
	onEvent func(json.RawMessage), onProgress func(*ChatResult)) (*ChatResult, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("%w: not connected", ErrConnClosed)
	}
	var err error
	images, err = c.prepareImages(ctx, images)
	if err != nil {
		return nil, err
	}
	c.drainEvents()
	opID := newTurnOpID()
	c.turnOpID.Store(opID)
	defer c.turnOpID.Store("")
	if err := c.send(chatMessage(text, images, opID)); err != nil {
		return nil, fmt.Errorf("%w: send chat: %v", ErrConnClosed, err)
	}

	result := &ChatResult{OpID: opID}
	idleC, idleReset, idleStop := newIdleWatch(c.idleTimeout)
	defer idleStop()
	for {
		select {
		case raw := <-c.eventCh:
			f := parseTurnFrame(raw)
			if !c.belongsTo(f, opID) {
				continue
			}
			switch f.Op {
			case "event":
				idleReset()
				if onEvent != nil {
					onEvent(raw)
				}
				done := c.dispatchEventData(raw, result)
				if onProgress != nil {
					onProgress(result)
				}
				if done {
					return c.finishTurn(result)
				}
			case "queue_state":
				if f.Busy != nil {
					result.Busy, result.BusySet = *f.Busy, true
				}
				if onEvent != nil {
					onEvent(raw)
				}
				if onProgress != nil && f.Busy != nil {
					onProgress(result)
				}
			case "error":
				idleReset()
				errMsg := parseErrorMessage(raw)
				c.lg.Warn().Str("err", errMsg).Str("op_id", opID).Msg("acp chat error event")
				if onEvent != nil {
					onEvent(raw)
				}
				result.appendErrorText(errMsg)
				if hasContent(result) {
					return result, nil
				}
				return nil, fmt.Errorf("acp error: %s", errMsg)
			}
		case <-idleC:
			c.lg.Warn().Dur("idle", c.idleTimeout).Str("op_id", opID).Msg("acp chat idle timeout")
			return c.abortTurn(result,
				fmt.Sprintf("Agent 连续 %s 没有任何输出，本轮已中断", c.idleTimeout),
				fmt.Errorf("%w after %s", ErrChatIdle, c.idleTimeout))
		case <-ctx.Done():
			c.lg.Warn().Err(ctx.Err()).Int("narration_bytes", len(result.Narration)).Str("op_id", opID).Msg("acp chat ctx done")
			reason := "本轮已中断"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = "本轮超过单轮时限，已中断"
			}
			return c.abortTurn(result, reason, ctx.Err())
		case <-c.done:
			if hasContent(result) {
				result.Interrupted = true
				return result, nil
			}
			return nil, fmt.Errorf("%w during chat", ErrConnClosed)
		}
	}
}

// finishTurn turns a prompt_done into the caller-facing outcome.
func (c *ACPClient) finishTurn(result *ChatResult) (*ChatResult, error) {
	c.lastDoneOpID.Store(result.OpID)
	c.lg.Info().
		Int("narration_bytes", len(result.Narration)).
		Int("tools", len(result.ToolCalls)).
		Str("op_id", result.OpID).
		Str("stop", result.StopReason).
		Msg("acp chat complete")
	if result.StopReason == stopReasonTimeout {
		if result.ErrorText == "" {
			result.appendErrorText("沙箱回合超时，已被终止")
		}
		if !hasContent(result) {
			return nil, fmt.Errorf("%w: %s", ErrSandboxTurnTimeout, result.ErrorText)
		}
	}
	return result, nil
}

// abortTurn cancels the turn on the bridge, waits for the acknowledgement and
// marks the result interrupted. An unacknowledged cancel flags the client as
// desynced so callers stop assuming the sandbox is idle.
func (c *ACPClient) abortTurn(result *ChatResult, reason string, cause error) (*ChatResult, error) {
	if !c.CancelTurnAndWait(result.OpID, cancelAckWait) {
		c.lg.Warn().Str("op_id", result.OpID).Msg("acp cancel not acknowledged; sandbox marked desynced")
		c.setDesynced()
	}
	result.Interrupted = true
	result.appendErrorText(reason)
	if hasContent(result) {
		return result, nil
	}
	return nil, cause
}

// CancelTurnAndWait asks the bridge to cancel turn opID (empty = whatever is
// running, plus the bridge queue) and waits up to wait for it to confirm: the
// turn's prompt_done, a cancel_ack saying it was dequeued / already gone, or
// the bridge reporting it is idle. Returns false when no confirmation arrived.
// It consumes the client's event stream, so it must not run concurrently with
// a chat on the same client.
func (c *ACPClient) CancelTurnAndWait(opID string, wait time.Duration) bool {
	if !c.IsConnected() {
		return false
	}
	msg := map[string]any{"op": "cancel"}
	if opID != "" {
		msg["opId"] = opID
	}
	if err := c.send(msg); err != nil {
		return false
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case raw := <-c.eventCh:
			if c.cancelConfirmed(parseTurnFrame(raw), opID) {
				return true
			}
		case <-timer.C:
			return false
		case <-c.done:
			return false
		}
	}
}

func (c *ACPClient) cancelConfirmed(f turnFrame, opID string) bool {
	switch f.Op {
	case "cancel_ack":
		return f.OpID == opID && (f.Status == "removed" || f.Status == "unknown")
	case "queue_state":
		return f.Busy != nil && !*f.Busy
	case "event":
		if f.dataType() != "prompt_done" {
			return false
		}
		if f.OpID != "" {
			c.opIDTagged.Store(true)
			if opID == "" || f.OpID == opID {
				c.lastDoneOpID.Store(f.OpID)
				return true
			}
			return false
		}
		return opID == "" || !c.opIDTagged.Load()
	}
	return false
}
