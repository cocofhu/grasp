package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
)

func takeoverTestProvider(t *testing.T, chatFor func(int) chatFunc) (*acpProvider, *fakeManager, NodeReq) {
	t.Helper()
	prev := abortTurnWait
	abortTurnWait = 50 * time.Millisecond
	t.Cleanup(func() { abortTurnWait = prev })
	host := mcp.NewHost(newMemStore())
	runID, nodeID := "to-run", "impl"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	mgr := newFakeManager(t, host, runID, nodeID, tok, chatFor)
	opts := testOpts()
	opts.ChatIdleTimeout = 10 * time.Second
	p, _ := newTestProvider(t, host, opts, mgr)
	req := reqWithProfile(NodeReq{RunID: runID, NodeID: nodeID, NodeType: "agent", Caps: testPlainCaps, Token: tok,
		Config: map[string]any{"prompt": "work"}, Vars: map[string]any{}})
	return p, mgr, req
}

func stallingChat(int) chatFunc {
	return func(int) turnAction { return turnAction{stall: true, ackCancel: true} }
}

func waitFirstPrompt(t *testing.T, mgr *fakeManager) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b := mgr.bridge(0); b != nil && b.promptAt(0) != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("agent prompt never reached the bridge")
}

// TestRunAgentTakeoverParksSession: cancelling RunAgent with ErrNodeTakenOver
// mid-turn parks the live session (no retire / discard / retry) and reports
// the takeover instead of a chat failure.
func TestRunAgentTakeoverParksSession(t *testing.T) {
	p, mgr, req := takeoverTestProvider(t, stallingChat)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	errCh := make(chan error, 1)
	go func() {
		_, err := p.RunAgent(ctx, req)
		errCh <- err
	}()
	waitFirstPrompt(t, mgr)
	cancel(ErrNodeTakenOver)

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrNodeTakenOver) {
			t.Fatalf("err=%v, want ErrNodeTakenOver", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunAgent did not return after takeover")
	}
	if !p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("taken-over session must stay parked for the manual phase")
	}
	if n := mgr.createCount(); n != 1 {
		t.Fatalf("takeover must not retry in a fresh sandbox (creates=%d)", n)
	}
	p.mu.Lock()
	_, inflight := p.inflightACP[reactKey(req)]
	p.mu.Unlock()
	if inflight {
		t.Fatal("parked session must leave the in-flight ACP map")
	}
	p.RetireSession(req.RunID, req.NodeID)
	if p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("RetireSession must release the parked takeover session")
	}
}

// TestRunAgentPlainCancelDoesNotPark: any other cancel keeps the old
// behaviour (sandbox retired, nothing parked).
func TestRunAgentPlainCancelDoesNotPark(t *testing.T) {
	p, mgr, req := takeoverTestProvider(t, stallingChat)
	ctx, cancel := context.WithCancelCause(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := p.RunAgent(ctx, req)
		errCh <- err
	}()
	waitFirstPrompt(t, mgr)
	cancel(errors.New("run aborted"))
	select {
	case err := <-errCh:
		if err == nil || errors.Is(err, ErrNodeTakenOver) {
			t.Fatalf("err=%v, want a plain cancellation error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunAgent did not return after cancel")
	}
	if p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("a non-takeover cancel must not park the session")
	}
}

// TestRunAgentTakeoverDuringSandboxOpen: no ACP session exists yet, so there
// is nothing to park; RunAgent stops without retrying and the manual phase
// later rehydrates a cold sandbox.
func TestRunAgentTakeoverDuringSandboxOpen(t *testing.T) {
	p, mgr, req := takeoverTestProvider(t, stallingChat)
	mgr.createErr = func(int) error { return errors.New("gateway create: boom") }
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrNodeTakenOver)
	if _, err := p.RunAgent(ctx, req); err == nil {
		t.Fatal("want an error when the sandbox never opened")
	}
	if n := mgr.createCount(); n != 1 {
		t.Fatalf("takeover during open must not retry (creates=%d)", n)
	}
	if p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("nothing to park before the sandbox exists")
	}
}
