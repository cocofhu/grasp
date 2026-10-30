package chatsession

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testItem struct {
	id   string
	text string
}

type frame struct {
	event   string
	payload map[string]any
}

type harness struct {
	mu      sync.Mutex
	frames  []frame
	ran     []string
	release map[string]chan struct{}
	errs    map[string]error
	idle    chan struct{}
}

func newHarness() *harness {
	return &harness{release: map[string]chan struct{}{}, errs: map[string]error{}, idle: make(chan struct{}, 8)}
}

func (h *harness) hold(id string) chan struct{} {
	ch := make(chan struct{})
	h.mu.Lock()
	h.release[id] = ch
	h.mu.Unlock()
	return ch
}

func (h *harness) config() Config[*testItem] {
	return Config[*testItem]{
		Capacity:   2,
		FullError:  "full",
		View:       func(it *testItem) ItemView { return ItemView{ID: it.id, Text: it.text} },
		FrameExtra: map[string]any{"kind": "test"},
		Publish: func(event string, payload map[string]any) {
			h.mu.Lock()
			h.frames = append(h.frames, frame{event, payload})
			h.mu.Unlock()
		},
		Execute: func(ctx context.Context, it *testItem) (bool, error) {
			h.mu.Lock()
			h.ran = append(h.ran, it.id)
			ch := h.release[it.id]
			err := h.errs[it.id]
			h.mu.Unlock()
			if ch != nil {
				select {
				case <-ch:
				case <-ctx.Done():
					return false, ctx.Err()
				}
			}
			return false, err
		},
		OnIdle: func() { h.idle <- struct{}{} },
	}
}

func (h *harness) events() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.frames))
	for _, f := range h.frames {
		out = append(out, f.event)
	}
	return out
}

func (h *harness) last(event string) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.frames) - 1; i >= 0; i-- {
		if h.frames[i].event == event {
			return h.frames[i].payload
		}
	}
	return nil
}

func (h *harness) waitIdle(t *testing.T) {
	t.Helper()
	select {
	case <-h.idle:
	case <-time.After(2 * time.Second):
		t.Fatal("pump never went idle")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSessionRunsInOrderAndPublishesFrames(t *testing.T) {
	h := newHarness()
	gate := h.hold("a")
	s := New(h.config())
	if n, err := s.Enqueue(&testItem{"a", "first"}, nil); err != nil || n != 1 {
		t.Fatalf("enqueue a: n=%d err=%v", n, err)
	}
	waitFor(t, func() bool { _, ok := s.Active(); return ok })
	if n, err := s.Enqueue(&testItem{"b", "second"}, nil); err != nil || n != 1 {
		t.Fatalf("enqueue b: n=%d err=%v", n, err)
	}
	sn := s.Snapshot()
	if !sn.Busy || sn.Waiting != 1 || sn.ActiveItem["id"] != "a" || sn.Items[0]["id"] != "b" {
		t.Fatalf("snapshot=%+v", sn)
	}
	close(gate)
	h.waitIdle(t)

	if got := h.ran; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ran=%v", got)
	}
	begin := h.last(EventTurnBegin)
	if begin["kind"] != "test" || begin["item"].(map[string]any)["id"] != "b" {
		t.Fatalf("turn_begin=%v", begin)
	}
	if qs := h.last(EventQueueState); qs["kind"] != "test" {
		t.Fatalf("queue_state missing frame extra: %v", qs)
	}
	if done := h.last(EventTurnDone); done["interrupted"] != false {
		t.Fatalf("turn_done=%v", done)
	}
	if !s.Idle() || !s.Ready() {
		t.Fatal("session should be idle")
	}
}

func TestSessionPanickingTurnPublishesErrorAndKeepsPumping(t *testing.T) {
	h := newHarness()
	cfg := h.config()
	execute := cfg.Execute
	cfg.Execute = func(ctx context.Context, it *testItem) (bool, error) {
		if it.id == "a" {
			panic("boom")
		}
		return execute(ctx, it)
	}
	afterTurns := 0
	cfg.AfterTurn = func(*testItem) { afterTurns++ }
	gate := h.hold("b")
	s := New(cfg)
	if _, err := s.Enqueue(&testItem{"a", "first"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(&testItem{"b", "second"}, nil); err != nil {
		t.Fatal(err)
	}
	close(gate)
	h.waitIdle(t)

	failed := h.last(EventError)
	if failed == nil || failed["item"].(map[string]any)["id"] != "a" || failed["message"] != errTurnPanicked.Error() {
		t.Fatalf("error frame=%v", failed)
	}
	if done := h.last(EventTurnDone); done == nil || done["item"].(map[string]any)["id"] != "b" {
		t.Fatalf("turn_done=%v", done)
	}
	if got := h.ran; len(got) != 1 || got[0] != "b" {
		t.Fatalf("ran=%v", got)
	}
	if afterTurns != 2 {
		t.Fatalf("AfterTurn ran %d times", afterTurns)
	}
	if !s.Idle() {
		t.Fatal("session should be idle")
	}
}

func TestSessionPanickingBeforeTurnSkipsExecute(t *testing.T) {
	h := newHarness()
	cfg := h.config()
	cfg.BeforeTurn = func(*testItem, <-chan struct{}) { panic("boom") }
	s := New(cfg)
	if _, err := s.Enqueue(&testItem{"a", "first"}, nil); err != nil {
		t.Fatal(err)
	}
	h.waitIdle(t)

	if failed := h.last(EventError); failed == nil || failed["message"] != errTurnPanicked.Error() {
		t.Fatalf("error frame=%v", failed)
	}
	if len(h.ran) != 0 {
		t.Fatalf("execute ran after BeforeTurn panic: %v", h.ran)
	}
}

// A paused queue publisher must keep the version captured at the mutation,
// even when three turns finish before its callback reaches the wire. Otherwise
// a delayed enqueue/reconnect snapshot can make completed B appear queued again.
func TestSessionDelayedQueueFrameCannotBecomeNewerThanCompletedTurns(t *testing.T) {
	h := newHarness()
	finishA, finishB, finishC := h.hold("a"), h.hold("b"), h.hold("c")
	cfg := h.config()
	cfg.Capacity = 4
	originalPublish := cfg.Publish
	delayed := make(chan struct{})
	releasePublish := make(chan struct{})
	enqueued := make(chan struct{})
	cfg.Publish = func(event string, payload map[string]any) {
		if event == EventQueueState {
			items := payload["items"].([]map[string]any)
			if len(items) == 1 && items[0]["id"] == "b" {
				close(delayed)
				<-releasePublish
			}
		}
		originalPublish(event, payload)
	}
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{"a", "first"}, nil)
	waitFor(t, func() bool { return h.last(EventTurnBegin) != nil })
	go func() { _, _ = s.Enqueue(&testItem{"b", "second"}, nil); close(enqueued) }()
	select {
	case <-delayed:
	case <-time.After(2 * time.Second):
		t.Fatal("queue publication did not pause")
	}
	_, _ = s.Enqueue(&testItem{"c", "third"}, nil)
	close(finishA)
	waitFor(t, func() bool { active, ok := s.Active(); return ok && active.id == "b" })
	beginB := h.last(EventTurnBegin)
	waitFor(t, func() bool {
		beginB = h.last(EventTurnBegin)
		return beginB != nil && beginB["item"].(map[string]any)["id"] == "b"
	})
	if beginB["activeItem"].(map[string]any)["id"] != "b" || beginB["waiting"] != 1 || beginB["items"].([]map[string]any)[0]["id"] != "c" {
		t.Fatalf("begin must carry the complete current FIFO: %v", beginB)
	}
	close(finishB)
	waitFor(t, func() bool { active, ok := s.Active(); return ok && active.id == "c" })
	close(finishC)
	h.waitIdle(t)
	latest := h.last(EventTurnDone)
	snapshot := s.Snapshot()
	close(releasePublish)
	select {
	case <-enqueued:
	case <-time.After(2 * time.Second):
		t.Fatal("delayed enqueue never returned")
	}
	stale := h.last(EventQueueState)
	if latest["sessionGeneration"] == nil || latest["sessionGeneration"] != stale["sessionGeneration"] || latest["revision"].(uint64) <= stale["revision"].(uint64) {
		t.Fatalf("completed state must supersede the delayed snapshot: latest=%v stale=%v", latest, stale)
	}
	if snapshot.SessionGeneration != latest["sessionGeneration"] || snapshot.Revision != latest["revision"] || snapshot.Busy || snapshot.Waiting != 0 {
		t.Fatalf("reconnect snapshot must match terminal authority: %+v latest=%v", snapshot, latest)
	}
	if latest["item"].(map[string]any)["id"] != "c" || latest["busy"] != false || latest["waiting"] != 0 {
		t.Fatalf("terminal identity/state missing: %v", latest)
	}
}

func TestSessionQueueRefreshCapturesVersionAndActiveIdentityTogether(t *testing.T) {
	h := newHarness()
	finish := h.hold("a")
	cfg := h.config()
	cfg.View = func(it *testItem) ItemView {
		return ItemView{ID: it.id, ClientMessageID: "client-" + it.id, Text: it.text}
	}
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{"a", "first"}, nil)
	waitFor(t, func() bool { return h.last(EventTurnBegin) != nil })
	_, _ = s.Enqueue(&testItem{"b", "second"}, nil)
	for range 2 { // Independent viewers reconnecting must observe the same authority.
		s.PublishQueueState()
		state := h.last(EventQueueState)
		snap := s.Snapshot()
		if state["sessionGeneration"] != snap.SessionGeneration || state["revision"] != snap.Revision || state["waiting"] != 1 || state["activeItem"].(map[string]any)["clientMessageId"] != "client-a" {
			t.Fatalf("refresh lost atomic version/identity: frame=%v snapshot=%+v", state, snap)
		}
		if state["items"].([]map[string]any)[0]["clientMessageId"] != "client-b" {
			t.Fatalf("pending item lost sender identity: %v", state)
		}
	}
	close(finish)
	h.waitIdle(t)
}

func TestSessionSnapshotWatermarkDistinguishesRetiredAndNewSessions(t *testing.T) {
	retired := New(Config[string]{}).Snapshot()
	floor := CurrentSessionGeneration()
	next := New(Config[string]{}).Snapshot()
	if retired.SessionGeneration > floor || next.SessionGeneration <= floor {
		t.Fatalf("idle snapshot cannot distinguish retired/new sessions: retired=%d floor=%d next=%d", retired.SessionGeneration, floor, next.SessionGeneration)
	}
}

func TestSessionCancelPersistsDroppedItemsBeforeInterruptingActiveTurn(t *testing.T) {
	var s *Session[string]
	var droppedPersisted atomic.Bool
	activeCtx := make(chan context.Context, 1)
	interruptedBeforeDrop := make(chan struct{}, 1)
	dropStarted, releaseDrop := make(chan struct{}), make(chan struct{})
	cancelDone, idle := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseDrop) }) }
	t.Cleanup(release)
	var dropCtxErr error
	var dropSnapshot Snapshot
	var nativeTarget string
	s = New(Config[string]{
		View: func(id string) ItemView { return ItemView{ID: id} },
		Execute: func(ctx context.Context, _ string) (bool, error) {
			activeCtx <- ctx
			<-ctx.Done()
			// PM persists its active turn's failure immediately after cancellation.
			// Pending-item persistence must finish before releasing that writer.
			if !droppedPersisted.Load() {
				interruptedBeforeDrop <- struct{}{}
			}
			return true, nil
		},
		OnDropped: func(items []string) {
			if len(items) != 1 || items[0] != "b" {
				t.Errorf("dropped items = %v, want [b]", items)
			}
			dropCtxErr = (<-activeCtx).Err()
			dropSnapshot = s.Snapshot() // Cleanup callbacks must not hold the state mutex.
			close(dropStarted)
			<-releaseDrop
			droppedPersisted.Store(true)
		},
		CancelTurn: func() { nativeTarget, _ = s.Active() },
		Publish:    func(string, map[string]any) {},
		OnIdle:     func() { close(idle) },
	})
	_, _ = s.Enqueue("a", nil)
	waitFor(t, func() bool { return s.Snapshot().Busy })
	_, _ = s.Enqueue("b", nil)
	go func() { s.Cancel(true); close(cancelDone) }()
	select {
	case <-dropStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("dropped-item cleanup could not inspect session state")
	}
	if dropCtxErr != nil {
		t.Errorf("active turn was interrupted before dropped-item persistence: %v", dropCtxErr)
	}
	if !dropSnapshot.Busy || dropSnapshot.Waiting != 0 || dropSnapshot.ActiveItem["id"] != "a" {
		t.Errorf("cleanup snapshot = %+v, want active a with no pending items", dropSnapshot)
	}
	release()
	select {
	case <-cancelDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel never returned")
	}
	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled session never became idle")
	}
	if nativeTarget != "a" {
		t.Errorf("native cancel targeted %q, want a", nativeTarget)
	}
	select {
	case <-interruptedBeforeDrop:
		t.Error("active cancellation finalization raced dropped-item persistence")
	default:
	}
}

func TestSessionDelayedCancelPublicationCannotCancelNextTurn(t *testing.T) {
	var s *Session[string]
	var cancelling atomic.Bool
	activeA, activeB := make(chan struct{}), make(chan struct{})
	finishB := make(chan struct{})
	delayedPublish, releasePublish, cancelDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	nativeTarget := make(chan string, 1)
	idle := make(chan struct{})
	s = New(Config[string]{
		View: func(id string) ItemView { return ItemView{ID: id} },
		Execute: func(ctx context.Context, id string) (bool, error) {
			if id == "a" {
				close(activeA)
				<-ctx.Done()
				return true, nil
			}
			close(activeB)
			<-finishB
			return false, nil
		},
		CancelTurn: func() {
			active, _ := s.Active() // Native cancellation may inspect session state.
			nativeTarget <- active
		},
		Publish: func(event string, payload map[string]any) {
			active, _ := payload["activeItem"].(map[string]any)
			if cancelling.Load() && event == EventQueueState && active["id"] == "a" {
				close(delayedPublish)
				<-releasePublish
			}
		},
		OnIdle: func() { close(idle) },
	})
	_, _ = s.Enqueue("a", nil)
	select {
	case <-activeA:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn never started")
	}
	_, _ = s.Enqueue("b", nil)
	cancelling.Store(true)
	go func() { s.Cancel(false); close(cancelDone) }()
	select {
	case <-delayedPublish:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel publication did not pause")
	}
	// The old cancellation frame is still blocked, while the kept queue has
	// already advanced. Native cancellation must have targeted A before this.
	select {
	case <-activeB:
	case <-time.After(2 * time.Second):
		t.Fatal("next turn was pinned behind a slow cancel publisher")
	}
	select {
	case target := <-nativeTarget:
		if target != "a" {
			t.Fatalf("cancel for A targeted %q", target)
		}
	default:
		t.Fatal("native cancellation was postponed until after queue publication")
	}
	close(releasePublish)
	select {
	case <-cancelDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel never returned")
	}
	select {
	case target := <-nativeTarget:
		t.Fatalf("late cancellation targeted %q", target)
	default:
	}
	close(finishB)
	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatal("session never became idle")
	}
}

func TestSessionErrorFrameUnlessCancelled(t *testing.T) {
	h := newHarness()
	h.errs["a"] = errors.New("boom")
	s := New(h.config())
	_, _ = s.Enqueue(&testItem{"a", ""}, nil)
	h.waitIdle(t)
	if e := h.last(EventError); e == nil || e["message"] != "boom" {
		t.Fatalf("want error frame, got %v", h.events())
	}
}

func TestSessionErrorFrameExtra(t *testing.T) {
	h := newHarness()
	h.errs["a"] = errors.New("boom")
	cfg := h.config()
	cfg.ErrorExtra = func(err error) map[string]any {
		return map[string]any{"code": "x_" + err.Error(), "message": "override"}
	}
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{"a", ""}, nil)
	h.waitIdle(t)
	e := h.last(EventError)
	if e == nil || e["code"] != "x_boom" || e["message"] != "override" || e["interrupted"] != false {
		t.Fatalf("error frame=%v", e)
	}
}

func TestSessionCancelKeepsOrClearsQueue(t *testing.T) {
	for _, clear := range []bool{false, true} {
		h := newHarness()
		h.hold("a")
		var dropped []*testItem
		cfg := h.config()
		cfg.OnDropped = func(items []*testItem) { dropped = items }
		cancels := 0
		cfg.CancelTurn = func() { cancels++ }
		s := New(cfg)
		_, _ = s.Enqueue(&testItem{"a", ""}, nil)
		waitFor(t, func() bool { _, ok := s.Active(); return ok })
		_, _ = s.Enqueue(&testItem{"b", ""}, nil)

		s.Cancel(clear)
		h.waitIdle(t)
		if cancels != 1 {
			t.Fatalf("clear=%v CancelTurn calls=%d", clear, cancels)
		}
		ranB := len(h.ran) == 2
		if clear && (ranB || len(dropped) != 1 || dropped[0].id != "b") {
			t.Fatalf("clear: ran=%v dropped=%v", h.ran, dropped)
		}
		if !clear && (!ranB || len(dropped) != 0) {
			t.Fatalf("keep: ran=%v dropped=%v", h.ran, dropped)
		}
		h.mu.Lock()
		var firstDone map[string]any
		for _, f := range h.frames {
			if f.event == EventTurnDone {
				firstDone = f.payload
				break
			}
		}
		h.mu.Unlock()
		if firstDone["interrupted"] != true {
			t.Fatalf("clear=%v cancelled turn should be turn_done interrupted, frames=%v", clear, h.events())
		}
	}
}

func TestSessionCapacityAndAdmit(t *testing.T) {
	h := newHarness()
	h.hold("a")
	s := New(h.config())
	_, _ = s.Enqueue(&testItem{"a", ""}, nil)
	waitFor(t, func() bool { _, ok := s.Active(); return ok })
	_, _ = s.Enqueue(&testItem{"b", ""}, nil)
	_, _ = s.Enqueue(&testItem{"c", ""}, nil)
	if _, err := s.Enqueue(&testItem{"d", ""}, nil); err == nil || err.Error() != "full" {
		t.Fatalf("want full, got %v", err)
	}
	deny := errors.New("dup")
	_, err := s.Enqueue(&testItem{"e", ""}, func(active *testItem, has bool, pending []*testItem) error {
		if !has || active.id != "a" || len(pending) != 2 {
			t.Fatalf("admit view active=%v has=%v pending=%d", active, has, len(pending))
		}
		return deny
	})
	if !errors.Is(err, deny) {
		t.Fatalf("admit error=%v", err)
	}
	s.Cancel(true)
	h.waitIdle(t)
}

func TestSessionRemoveReorder(t *testing.T) {
	h := newHarness()
	h.hold("a")
	cfg := h.config()
	cfg.Capacity = 5
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{"a", ""}, nil)
	waitFor(t, func() bool { _, ok := s.Active(); return ok })
	for _, id := range []string{"b", "c", "d"} {
		_, _ = s.Enqueue(&testItem{id, ""}, nil)
	}
	if err := s.Remove("a"); err == nil {
		t.Fatal("active item must not be removable")
	}
	if err := s.Remove("zz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove unknown=%v", err)
	}
	if err := s.Remove("c"); err != nil {
		t.Fatal(err)
	}
	if err := s.Reorder([]string{"b"}); err == nil {
		t.Fatal("partial reorder must fail")
	}
	if err := s.Reorder([]string{"d", "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reorder unknown=%v", err)
	}
	if err := s.Reorder([]string{"d", "b"}); err != nil {
		t.Fatal(err)
	}
	if qs := h.last(EventQueueState); qs["waiting"] != 2 || qs["busy"] != true {
		t.Fatalf("queue_state after reorder=%v", qs)
	}
	h.mu.Lock()
	close(h.release["a"])
	h.mu.Unlock()
	h.waitIdle(t)
	if got := h.ran; len(got) != 3 || got[1] != "d" || got[2] != "b" {
		t.Fatalf("ran=%v", got)
	}
}

func TestSessionHooksAndLiveEvents(t *testing.T) {
	h := newHarness()
	gate := h.hold("a")
	var order []string
	var omu sync.Mutex
	note := func(s string) { omu.Lock(); order = append(order, s); omu.Unlock() }
	cfg := h.config()
	cfg.BeforeTurn = func(it *testItem, done <-chan struct{}) {
		note("before:" + it.id)
		go func() { <-done; note("done:" + it.id) }()
	}
	cfg.AfterTurn = func(it *testItem) { note("after:" + it.id) }
	cfg.TurnBeginExtra = func(it *testItem) map[string]any { return map[string]any{"source": "x"} }
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{"a", ""}, nil)
	waitFor(t, func() bool { _, ok := s.Active(); return ok })
	if s.LiveEvents() != nil {
		t.Fatal("no live events yet")
	}
	s.SetLiveEvents(nil)
	if s.CancelRequested() {
		t.Fatal("no cancel yet")
	}
	close(gate)
	h.waitIdle(t)
	if s.LiveEvents() != nil {
		t.Fatal("live events must clear when idle")
	}
	waitFor(t, func() bool { omu.Lock(); defer omu.Unlock(); return len(order) == 3 })
	if order[0] != "before:a" {
		t.Fatalf("order=%v", order)
	}
	if b := h.last(EventTurnBegin); b["source"] != "x" {
		t.Fatalf("turn_begin extra missing: %v", b)
	}
}

func TestSessionPublishQueueStateAndSeed(t *testing.T) {
	h := newHarness()
	s := New(h.config())
	s.SeedForTest([]*testItem{{"q", "queued"}})
	s.PublishQueueState()
	qs := h.last(EventQueueState)
	if qs["waiting"] != 1 || qs["busy"] != false || qs["kind"] != "test" {
		t.Fatalf("queue_state=%v", qs)
	}
	if s.Ready() {
		t.Fatal("seeded session is not ready")
	}
	if _, ok := qs["activeItem"]; ok {
		t.Fatal("idle snapshot has no active item")
	}
}

func TestItemViewFrameEmptySlices(t *testing.T) {
	f := ItemView{ID: "x"}.Frame()
	if f["images"] == nil || f["annotations"] == nil {
		t.Fatalf("frame=%v", f)
	}
}

func TestRegistry(t *testing.T) {
	var r Registry[*Session[*testItem]]
	h := newHarness()
	created := 0
	mk := func() *Session[*testItem] { created++; return New(h.config()) }
	a := r.GetOrCreate("k", mk)
	if b := r.GetOrCreate("k", mk); a != b || created != 1 {
		t.Fatal("GetOrCreate must reuse")
	}
	if _, ok := r.Get("k"); !ok {
		t.Fatal("Get")
	}
	n := 0
	r.Range(func(string, *Session[*testItem]) bool { n++; return false })
	if n != 1 {
		t.Fatalf("range=%d", n)
	}
	a.SeedForTest([]*testItem{{"q", ""}})
	r.DropIfIdle("k")
	if _, ok := r.Get("k"); !ok {
		t.Fatal("busy session must stay")
	}
	a.SeedForTest(nil)
	r.DropIfIdle("k")
	if _, ok := r.Get("k"); ok {
		t.Fatal("idle session must drop")
	}
}
