package chatsession

import (
	"context"
	"errors"
	"sync"
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
