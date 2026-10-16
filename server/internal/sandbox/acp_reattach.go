package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// WaitBridgeKnown waits until the bridge has reported its queue state on this
// connection (the bridge broadcasts it right after connect). Returns false when
// none arrived within wait or the connection closed.
func (c *ACPClient) WaitBridgeKnown(ctx context.Context, wait time.Duration) (BridgeState, bool) {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if st := c.BridgeState(); st.Known {
			return st, true
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			return BridgeState{}, false
		case <-ctx.Done():
			return BridgeState{}, false
		case <-c.done:
			return BridgeState{}, false
		}
	}
}

// AwaitTurn waits for turn opID, started by an earlier client of this bridge
// (a control-plane process that has since exited), to finish. It does not
// aggregate the turn: frames from before this client connected are gone, so
// callers rebuild the result from the event log afterwards.
//
// The same silence watchdog as runTurn applies, and when ctx ends or the bridge
// goes silent the turn is cancelled on the bridge before returning.
func (c *ACPClient) AwaitTurn(ctx context.Context, opID string) error {
	if !c.IsConnected() {
		return fmt.Errorf("%w: not connected", ErrConnClosed)
	}
	release := c.acquireReader()
	defer release()
	idle := c.agentIdle()
	watch := legacyWatch(idle)
	heartbeats := false
	idleC, idleReset, idleStop := newIdleWatch(watch)
	defer func() { idleStop() }()
	// prompt_done may have gone out between connect and acquireReader; the
	// queue_state mirror is updated regardless of readers.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case raw := <-c.eventCh:
			f := parseTurnFrame(raw)
			switch f.Op {
			case "event":
				if f.OpID != opID {
					continue
				}
				idleReset()
				if f.dataType() == "prompt_done" {
					c.lastDoneOpID.Store(opID)
					return nil
				}
			case "liveness":
				if f.OpID != opID {
					continue
				}
				if !heartbeats && idle > 0 {
					heartbeats = true
					idleStop()
					watch = bridgeLostAfter
					idleC, idleReset, idleStop = newIdleWatch(watch)
				} else {
					idleReset()
				}
			}
		case <-tick.C:
			if st := c.BridgeState(); st.Known && st.RunningOpID != opID {
				return nil
			}
		case <-idleC:
			if !c.CancelTurnAndWait(opID, cancelAckWait) {
				c.setDesynced()
			}
			return fmt.Errorf("%w: no frame from the sandbox for %s", ErrChatIdle, watch)
		case <-ctx.Done():
			if !c.CancelTurnAndWait(opID, cancelAckWait) {
				c.setDesynced()
			}
			return context.Cause(ctx)
		case <-c.done:
			return fmt.Errorf("%w while awaiting turn", ErrConnClosed)
		}
	}
}

// ReplayTurn folds one persisted turn (frames from its prompt_begin through
// prompt_done, as returned by FirstTurnFrames) into a ChatResult with the same
// outcome rules as a live turn: a stuck stop is ErrAgentStuck and an empty
// timed-out turn is ErrSandboxTurnTimeout.
func (c *ACPClient) ReplayTurn(frames []json.RawMessage) (*ChatResult, error) {
	result := &ChatResult{}
	done := false
	for _, raw := range frames {
		env := raw
		var probe struct {
			Op string `json:"op"`
		}
		if json.Unmarshal(raw, &probe) != nil || probe.Op != "event" {
			wrapped, err := json.Marshal(map[string]any{"op": "event", "data": raw})
			if err != nil {
				continue
			}
			env = wrapped
		}
		if c.dispatchEventData(env, result) {
			done = true
		}
	}
	if !done {
		return result, fmt.Errorf("%w: turn has no prompt_done in the event log", ErrConnClosed)
	}
	if result.StopReason == stopReasonStuck {
		if result.ErrorText == "" {
			result.appendErrorText("Agent 长时间没有任何活动，已被终止")
		}
		return result, fmt.Errorf("%w: %s", ErrAgentStuck, result.ErrorText)
	}
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
