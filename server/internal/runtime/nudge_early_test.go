package runtime

import (
	"context"
	"testing"
	"time"
)

func shrinkNudgeGrace(t *testing.T) {
	t.Helper()
	oldGrace, oldPoll := nudgeGrace, nudgePoll
	nudgeGrace, nudgePoll = 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { nudgeGrace, nudgePoll = oldGrace, oldPoll })
}

// A nudge turn that has made the call it was asked for is ended shortly after,
// instead of waiting for the model to finish the turn on its own.
func TestNudgeChatEndsOnceDone(t *testing.T) {
	shrinkNudgeGrace(t)
	p, _, _, mgr, _, req := setupProvider(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "calling node_complete", hang: true}
			}
			return turnAction{narration: "ok"}
		}
	})
	p.opts.ChatIdleTimeout = time.Hour
	sb, acp, home, err := p.openSandbox(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer p.retireRunSandbox(sb, acp, home)
	b := mgr.bridge(0)
	done := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.hungOp != ""
	}
	start := time.Now()
	res, err := p.nudgeChat(context.Background(), acp, req, "call node_complete", done)
	if err != nil || res == nil || res.Narration != "calling node_complete" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("nudge turn should end early, took %s", d)
	}
	b.mu.Lock()
	cancels := b.cancels
	b.mu.Unlock()
	if cancels != 1 || acp.BridgeState().Desynced {
		t.Fatalf("cancels=%d desynced=%v", cancels, acp.BridgeState().Desynced)
	}

	// A turn that finishes on its own is left alone.
	res, err = p.nudgeChat(context.Background(), acp, req, "noop", func() bool { return false })
	if err != nil || res.Narration != "ok" || res.Interrupted {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
