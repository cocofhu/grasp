package runtime

// Takeover vs. control-plane-restart reattach, and vs. a concurrent run
// abort (GraspReview RT1/RT2 + S2).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

func setupTakeoverReattach(t *testing.T, b *survivorBridge) (*acpProvider, *reattachManager, *countingRegistry, NodeReq) {
	t.Helper()
	restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte("deepseek/deepseek-chat\n"), nil
	})
	t.Cleanup(restore)
	p, _, _, fm, reg, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{narration: "fresh sandbox", outcome: true} }
	})
	mgr := &reattachManager{fakeManager: fm, survivors: map[string]*survivorBridge{}}
	b.start(t)
	mgr.survivors["sb-old"] = b
	p.mgr = mgr
	req.Config["produces"] = ""
	req.ReattachSandbox = "sb-old"
	return p, mgr, reg, req
}

// TestTakeoverDuringReattachWaitParksSurvivor: a takeover while a reattached
// node waits for the turn that survived the restart parks the survivor
// session; nothing is resent and no fresh sandbox is created.
func TestTakeoverDuringReattachWaitParksSurvivor(t *testing.T) {
	b := &survivorBridge{running: "g-old"}
	raw, _ := json.Marshal(map[string]any{"type": "prompt_begin", "opId": "g-old"})
	b.log = []json.RawMessage{raw}
	p, mgr, _, req := setupTakeoverReattach(t, b)
	prev := abortTurnWait
	abortTurnWait = 2 * time.Second
	t.Cleanup(func() { abortTurnWait = prev })
	ctx, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(300*time.Millisecond, func() { cancel(ErrNodeTakenOver) })
	_, err := p.RunAgent(ctx, req)
	if !errors.Is(err, ErrNodeTakenOver) {
		t.Fatalf("err=%v want ErrNodeTakenOver", err)
	}
	if !p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("survivor session should be parked for the manual phase")
	}
	if b.chatCount() != 0 || mgr.createCount() != 0 {
		t.Fatalf("chats=%d creates=%d, want 0/0", b.chatCount(), mgr.createCount())
	}
	p.RetireSession(req.RunID, req.NodeID)
}

// TestTakeoverDuringReattachHandshakeKeepsSurvivor (B2): a takeover that lands
// before the reattach handshake finished must not make attachSandbox treat
// the cancelled Connect as a broken sandbox (deregister + Destroy): the
// survivor workspace is parked, no prompt is sent, nothing is unregistered.
func TestTakeoverDuringReattachHandshakeKeepsSurvivor(t *testing.T) {
	b := &survivorBridge{running: "g-old"}
	p, mgr, reg, req := setupTakeoverReattach(t, b)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrNodeTakenOver) // takeover before Connect completes
	_, err := p.RunAgent(ctx, req)
	if !errors.Is(err, ErrNodeTakenOver) {
		t.Fatalf("err=%v want ErrNodeTakenOver", err)
	}
	if !p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("takeover during the reattach handshake must keep (park) the survivor sandbox")
	}
	reg.mu.Lock()
	unreg := reg.unregistered
	reg.mu.Unlock()
	if unreg != 0 {
		t.Fatalf("survivor sandbox was deregistered %d time(s)", unreg)
	}
	if b.chatCount() != 0 || mgr.createCount() != 0 {
		t.Fatalf("chats=%d creates=%d, want 0/0", b.chatCount(), mgr.createCount())
	}
	p.RetireSession(req.RunID, req.NodeID)
}

// TestTakeoverAfterRunAbortDoesNotResurrectSession (S2): once AbortRun has
// claimed the in-flight session (removed it from inflightACP), the late
// takeover park must not re-insert it into sessions / live.
func TestTakeoverAfterRunAbortDoesNotResurrectSession(t *testing.T) {
	p, mgr, req := takeoverTestProvider(t, stallingChat)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	errCh := make(chan error, 1)
	go func() {
		_, err := p.RunAgent(ctx, req)
		errCh <- err
	}()
	waitFirstPrompt(t, mgr)
	// Model AbortRun's claim step winning the race against the park.
	p.mu.Lock()
	delete(p.inflightACP, reactKey(req))
	p.mu.Unlock()
	cancel(ErrNodeTakenOver)
	select {
	case <-errCh:
	case <-time.After(10 * time.Second):
		t.Fatal("RunAgent did not return after takeover")
	}
	p.mu.Lock()
	_, parked := p.sessions[reactKey(req)]
	p.mu.Unlock()
	if parked || p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("a session claimed by AbortRun must not be parked by a late takeover")
	}
}
