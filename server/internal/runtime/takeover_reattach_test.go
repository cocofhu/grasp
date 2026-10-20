package runtime

// Takeover vs. control-plane-restart reattach, and vs. a concurrent run
// abort (GraspReview RT1/RT2 + S2).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
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

// gatedManager blocks Create until release is closed (a slow sandbox setup:
// image pull / clone / ACP ready).
type gatedManager struct {
	*fakeManager
	entered chan struct{}
	release chan struct{}
}

func (m *gatedManager) Create(ctx context.Context, spec sandbox.Spec) (*sandbox.Sandbox, error) {
	select {
	case m.entered <- struct{}{}:
	default:
	}
	<-m.release
	return m.fakeManager.Create(ctx, spec)
}

func gatedTakeoverProvider(t *testing.T, chat func(int) chatFunc) (*acpProvider, *gatedManager, NodeReq) {
	p, fm, req := takeoverTestProvider(t, chat)
	gm := &gatedManager{fakeManager: fm, entered: make(chan struct{}, 1), release: make(chan struct{})}
	p.mgr = gm
	return p, gm, req
}

// TestTakeoverAndCancelDuringSetupRetiresSession (GraspReview v2 N1): a
// takeover and a run cancel both landing during sandbox setup — AbortRun runs
// before anything is registered — must not leave a parked live session for
// the aborted run.
func TestTakeoverAndCancelDuringSetupRetiresSession(t *testing.T) {
	p, gm, req := gatedTakeoverProvider(t, stallingChat)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	errCh := make(chan error, 1)
	go func() { _, err := p.RunAgent(ctx, req); errCh <- err }()
	select {
	case <-gm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("create never entered")
	}
	cancel(ErrNodeTakenOver)
	p.AbortRun(req.RunID)
	close(gm.release)
	select {
	case <-errCh:
	case <-time.After(10 * time.Second):
		t.Fatal("RunAgent did not return")
	}
	if p.HasLiveSession(req.RunID, req.NodeID) {
		p.RetireSession(req.RunID, req.NodeID)
		t.Fatal("parked session left behind for an aborted run")
	}
	p.mu.Lock()
	_, live := p.live[reactKey(req)]
	_, inflight := p.inflightACP[reactKey(req)]
	p.mu.Unlock()
	if live || inflight {
		t.Fatalf("aborted run still registered: live=%v inflight=%v", live, inflight)
	}
	if b := gm.bridge(0); b != nil && b.promptAt(0) != "" {
		t.Fatal("no prompt may be sent for an aborted run")
	}
}

// TestRunAbortDuringPlainSetupDoesNotRun: the same window without a takeover
// — the aborted run's fresh session is retired, never prompted.
func TestRunAbortDuringPlainSetupDoesNotRun(t *testing.T) {
	p, gm, req := gatedTakeoverProvider(t, stallingChat)
	errCh := make(chan error, 1)
	go func() { _, err := p.RunAgent(context.Background(), req); errCh <- err }()
	<-gm.entered
	p.AbortRun(req.RunID)
	close(gm.release)
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("aborted run must not succeed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunAgent did not return")
	}
	if b := gm.bridge(0); b != nil && b.promptAt(0) != "" {
		t.Fatal("no prompt may be sent for an aborted run")
	}
	// A later attempt on the same run|node key (ResumeFrom) is unaffected.
	gm.release = make(chan struct{})
	close(gm.release)
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { _, err := p.RunAgent(ctx2, req); errCh <- err }()
	waitFirstPromptOn(t, gm.fakeManager, 1)
	cancel2()
	select {
	case <-errCh:
	case <-time.After(10 * time.Second):
		t.Fatal("second attempt did not return")
	}
}

func waitFirstPromptOn(t *testing.T, mgr *fakeManager, i int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b := mgr.bridge(i); b != nil && b.promptAt(0) != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no prompt reached bridge %d", i)
}

// TestTakeoverDuringFreshSetupPrimesFirstManualTurn (GraspReview v2 N2): a
// takeover during a fresh setup does not park the never-prompted session;
// the first manual turn rehydrates a sandbox primed with the node task.
func TestTakeoverDuringFreshSetupPrimesFirstManualTurn(t *testing.T) {
	p, gm, req := gatedTakeoverProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{narration: "ok"} }
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	errCh := make(chan error, 1)
	go func() { _, err := p.RunAgent(ctx, req); errCh <- err }()
	<-gm.entered
	cancel(ErrNodeTakenOver)
	close(gm.release)
	if err := <-errCh; !errors.Is(err, ErrNodeTakenOver) {
		t.Fatalf("err=%v", err)
	}
	if p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("an unprimed fresh session must not be parked")
	}
	if b := gm.bridge(0); b != nil && b.promptAt(0) != "" {
		t.Fatal("no auto prompt may be sent after the takeover")
	}
	req.Caps = models.ManualReviewCaps(req.Caps)
	history := []models.ReactMessage{{Role: "agent", Text: "已中断自动执行…"}, {Role: "human", Text: "继续完成"}}
	turn := p.ReviseInPlace(context.Background(), req, history, "继续完成", nil)
	var prompts []string
	for bi := 0; bi < gm.createCount(); bi++ {
		b := gm.bridge(bi)
		for i := 0; b.promptAt(i) != ""; i++ {
			prompts = append(prompts, b.promptAt(i))
		}
	}
	if !strings.Contains(strings.Join(prompts, "\n"), "work") {
		t.Fatalf("first manual turn never primed the agent with the node task (turnErr=%v, prompts=%q)", turn.Err, prompts)
	}
	p.RetireSession(req.RunID, req.NodeID)
}

// TestTakeoverDuringReattachHandshakeCancelsSurvivorTurn (GraspReview v2 N3):
// the survivor's still-running auto turn is cancelled by the park even when
// the bridge's queue_state has not been observed yet.
func TestTakeoverDuringReattachHandshakeCancelsSurvivorTurn(t *testing.T) {
	b := &survivorBridge{running: "g-old"}
	p, _, _, req := setupTakeoverReattach(t, b)
	prev := abortTurnWait
	abortTurnWait = 2 * time.Second
	t.Cleanup(func() { abortTurnWait = prev })
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrNodeTakenOver)
	_, err := p.RunAgent(ctx, req)
	if !errors.Is(err, ErrNodeTakenOver) || !p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatalf("err=%v parked=%v", err, p.HasLiveSession(req.RunID, req.NodeID))
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		still := b.running
		b.mu.Unlock()
		if still == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("survivor auto turn %q still running after takeover", still)
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.RetireSession(req.RunID, req.NodeID)
}
