package pagebridge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakePage struct {
	mu     sync.Mutex
	frames []map[string]any
	cmds   chan map[string]any
}

func newFakePage() *fakePage { return &fakePage{cmds: make(chan map[string]any, 8)} }

func (p *fakePage) send(b []byte) error {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	p.mu.Lock()
	p.frames = append(p.frames, m)
	p.mu.Unlock()
	if m["type"] == "page_cmd" {
		p.cmds <- m
	}
	return nil
}

func (p *fakePage) lastState() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.frames) - 1; i >= 0; i-- {
		if p.frames[i]["type"] == "page_control_state" {
			return p.frames[i]
		}
	}
	return nil
}

func (p *fakePage) nextCmd(t *testing.T) map[string]any {
	t.Helper()
	select {
	case m := <-p.cmds:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no command reached the page")
		return nil
	}
}

func fastHub() *Hub {
	h := NewHub()
	h.PauseWait = 150 * time.Millisecond
	h.ReconnectWait = 150 * time.Millisecond
	h.CommandTimeout = 300 * time.Millisecond
	h.CallBudget = 2 * time.Second
	return h
}

var key = Key{RunID: "r", NodeID: "n", Owner: "user:alice"}

func doAsync(h *Hub, ctx context.Context, k Key, cmd Command) chan outcome {
	out := make(chan outcome, 1)
	go func() {
		res, err := h.Do(ctx, k, cmd)
		out <- outcome{res, err}
	}()
	return out
}

func wait(t *testing.T, ch chan outcome) outcome {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(3 * time.Second):
		t.Fatal("Do did not return")
		return outcome{}
	}
}

func TestDoRoundTrip(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	if st := p.lastState(); st["state"] != "online" || st["active"] != true {
		t.Fatalf("state frame = %v", st)
	}
	ch := doAsync(h, context.Background(), key, Command{Action: "click", Args: map[string]any{"index": 3}})
	cmd := p.nextCmd(t)
	if cmd["action"] != "click" {
		t.Fatalf("cmd = %v", cmd)
	}
	c.Deliver(cmd["id"].(string), Result{OK: true, State: map[string]any{"stateId": "p1:2"}})
	o := wait(t, ch)
	if o.err != nil || !o.res.OK || o.res.State["stateId"] != "p1:2" {
		t.Fatalf("got %+v %v", o.res, o.err)
	}
}

func TestOfflineFailsImmediately(t *testing.T) {
	h := fastHub()
	start := time.Now()
	if _, err := h.Do(context.Background(), key, Command{Action: "state"}); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("offline should not wait")
	}
	p := newFakePage()
	h.Attach(key, p.send).SetControl(false, true)
	if _, err := h.Do(context.Background(), key, Command{Action: "state"}); !errors.Is(err, ErrOffline) {
		t.Fatalf("toggle off: err = %v", err)
	}
}

func TestOtherOwnerNeverReceives(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	h.Attach(Key{RunID: "r", NodeID: "n", Owner: "embed:bob"}, p.send).SetControl(true, true)
	if _, err := h.Do(context.Background(), key, Command{Action: "state"}); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v", err)
	}
	if len(p.cmds) != 0 {
		t.Fatal("another owner's page got the command")
	}
}

func TestPausedWaitsThenFails(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	h.Attach(key, p.send).SetControl(true, false)
	if st := p.lastState(); st["state"] != "paused" {
		t.Fatalf("state = %v", st)
	}
	start := time.Now()
	_, err := h.Do(context.Background(), key, Command{Action: "state"})
	if !errors.Is(err, ErrPaused) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("paused should wait before failing")
	}
}

func TestPausedResumesWhenVisible(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, false)
	ch := doAsync(h, context.Background(), key, Command{Action: "state"})
	time.Sleep(30 * time.Millisecond)
	c.SetControl(true, true)
	cmd := p.nextCmd(t)
	c.Deliver(cmd["id"].(string), Result{OK: true})
	if o := wait(t, ch); o.err != nil {
		t.Fatalf("err = %v", o.err)
	}
}

func TestLastForegroundWins(t *testing.T) {
	h := fastHub()
	a, b := newFakePage(), newFakePage()
	ca := h.Attach(key, a.send)
	ca.SetControl(true, true)
	cb := h.Attach(key, b.send)
	cb.SetControl(true, true)
	if a.lastState()["active"] != false || b.lastState()["active"] != true {
		t.Fatalf("a=%v b=%v", a.lastState(), b.lastState())
	}
	ch := doAsync(h, context.Background(), key, Command{Action: "state"})
	cmd := b.nextCmd(t)
	cb.Deliver(cmd["id"].(string), Result{OK: true})
	wait(t, ch)

	// Tab A back in front takes over.
	ca.SetControl(true, false)
	ca.SetControl(true, true)
	ch = doAsync(h, context.Background(), key, Command{Action: "state"})
	cmd = a.nextCmd(t)
	ca.Deliver(cmd["id"].(string), Result{OK: true})
	wait(t, ch)
	if len(b.cmds) != 0 {
		t.Fatal("background tab got the command")
	}
}

func TestDisconnectMidCommandReturnsFreshStateUnconfirmed(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	ch := doAsync(h, context.Background(), key, Command{Action: "click", Args: map[string]any{"index": 1}})
	p.nextCmd(t)
	c.Detach()

	p2 := newFakePage()
	c2 := h.Attach(key, p2.send)
	c2.SetControl(true, true)
	cmd := p2.nextCmd(t)
	if cmd["action"] != "state" {
		t.Fatalf("reconnect must read state, not replay: %v", cmd)
	}
	c2.Deliver(cmd["id"].(string), Result{OK: true, State: map[string]any{"url": "/home"}})
	o := wait(t, ch)
	if o.err != nil || !o.res.Unconfirmed || o.res.State["url"] != "/home" {
		t.Fatalf("got %+v %v", o.res, o.err)
	}
}

func TestDisconnectWithoutReconnect(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	ch := doAsync(h, context.Background(), key, Command{Action: "click"})
	p.nextCmd(t)
	c.Detach()
	if o := wait(t, ch); !errors.Is(o.err, ErrLost) || !o.res.Unconfirmed {
		t.Fatalf("got %+v %v", o.res, o.err)
	}
}

func TestReloadBetweenCommandsWaitsForReconnect(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	c.Detach()
	ch := doAsync(h, context.Background(), key, Command{Action: "state"})
	time.Sleep(30 * time.Millisecond)
	p2 := newFakePage()
	c2 := h.Attach(key, p2.send)
	c2.SetControl(true, true)
	cmd := p2.nextCmd(t)
	c2.Deliver(cmd["id"].(string), Result{OK: true})
	if o := wait(t, ch); o.err != nil {
		t.Fatalf("err = %v", o.err)
	}
}

func TestToggleOffStopsPending(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	ch := doAsync(h, context.Background(), key, Command{Action: "click"})
	p.nextCmd(t)
	c.SetControl(false, true)
	if o := wait(t, ch); !errors.Is(o.err, ErrStopped) {
		t.Fatalf("err = %v", o.err)
	}
}

func TestTimeoutSendsCancel(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	ch := doAsync(h, context.Background(), key, Command{Action: "click"})
	p.nextCmd(t)
	if o := wait(t, ch); !errors.Is(o.err, ErrTimeout) {
		t.Fatalf("err = %v", o.err)
	}
	p.mu.Lock()
	last := p.frames[len(p.frames)-1]
	p.mu.Unlock()
	if last["type"] != "page_cmd_cancel" {
		t.Fatalf("last frame = %v", last)
	}
}

type fakeTurns struct {
	owner string
	done  chan struct{}
}

func (f fakeTurns) ActivePageTurn(string, string) (string, <-chan struct{}, bool) {
	return f.owner, f.done, f.owner != ""
}

func TestRouterCancelsWhenTurnEnds(t *testing.T) {
	h := fastHub()
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	turns := fakeTurns{owner: key.Owner, done: make(chan struct{})}
	r := &Router{Hub: h, Turns: turns}
	out := make(chan outcome, 1)
	go func() {
		res, err := r.Do("r", "n", Command{Action: "click"})
		out <- outcome{res, err}
	}()
	p.nextCmd(t)
	close(turns.done)
	if o := wait(t, out); !errors.Is(o.err, ErrCancelled) {
		t.Fatalf("err = %v", o.err)
	}
	if _, err := (&Router{Hub: h, Turns: fakeTurns{}}).Do("r", "n", Command{Action: "state"}); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("no owner: err = %v", err)
	}
}

func TestWatchSeesOwnerStatusOnly(t *testing.T) {
	h := fastHub()
	w := newFakePage()
	stop := h.Watch("r", key.Owner, w.send)
	defer stop()
	other := newFakePage()
	h.Attach(Key{RunID: "r", NodeID: "n", Owner: "embed:bob"}, other.send).SetControl(true, true)
	if w.lastState() != nil {
		t.Fatal("watcher saw another owner's page")
	}
	p := newFakePage()
	c := h.Attach(key, p.send)
	c.SetControl(true, true)
	if st := w.lastState(); st["state"] != "online" || st["nodeId"] != "n" {
		t.Fatalf("watch = %v", st)
	}
	c.Detach()
	if st := w.lastState(); st["state"] != "offline" {
		t.Fatalf("watch = %v", st)
	}
}

func TestTokenOwnerHidesToken(t *testing.T) {
	o := TokenOwner("embed", "gse_secret")
	if o == "" || o == "embed:gse_secret" || o != TokenOwner("embed", "gse_secret") {
		t.Fatalf("owner = %q", o)
	}
	if UserOwner(" ") != "" || UserOwner("a") != "user:a" {
		t.Fatal("UserOwner")
	}
}
