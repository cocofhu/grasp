package chatsession

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func pendingIDs(s *Session[*testItem]) []string {
	var ids []string
	for _, it := range s.View().Pending {
		ids = append(ids, it.id)
	}
	return ids
}

func steerSession(t *testing.T, h *harness, result SteerResult) (*Session[*testItem], *atomic.Int32, *[]string) {
	t.Helper()
	var cancels atomic.Int32
	var offered []string
	cfg := h.config()
	cfg.Capacity = 8
	cfg.CancelTurn = func() { cancels.Add(1) }
	cfg.SteerTurn = func(_ context.Context, active, item *testItem) SteerResult {
		offered = append(offered, active.id+"<-"+item.id)
		return result
	}
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{id: "a"}, nil)
	waitFor(t, func() bool { active, ok := s.Active(); return ok && active.id == "a" })
	for _, id := range []string{"b", "c", "d"} {
		_, _ = s.Enqueue(&testItem{id: id}, nil)
	}
	return s, &cancels, &offered
}

func TestSteerJoinsActiveTurn(t *testing.T) {
	h := newHarness()
	gate := h.hold("a")
	s, cancels, offered := steerSession(t, h, SteerJoined)
	out, err := s.Steer(context.Background(), "c")
	if err != nil || out != SteerOutcomeJoined {
		t.Fatalf("steer = %q %v", out, err)
	}
	if got := pendingIDs(s); !slices.Equal(got, []string{"b", "d"}) || cancels.Load() != 0 || s.CancelRequested() {
		t.Fatalf("pending=%v cancels=%d", got, cancels.Load())
	}
	if !slices.Equal(*offered, []string{"a<-c"}) {
		t.Fatalf("offered = %v", *offered)
	}
	if qs := h.last(EventQueueState); qs["waiting"] != 2 {
		t.Fatalf("queue_state after steer = %v", qs)
	}
	close(gate)
	h.waitIdle(t)
	h.mu.Lock()
	ran := slices.Clone(h.ran)
	h.mu.Unlock()
	if !slices.Equal(ran, []string{"a", "b", "d"}) {
		t.Fatalf("a joined item must not run again: %v", ran)
	}
}

func TestSteerRejectedInterruptsAndRunsNext(t *testing.T) {
	h := newHarness()
	h.hold("a")
	h.hold("d")
	s, cancels, _ := steerSession(t, h, SteerRejected)
	out, err := s.Steer(context.Background(), "d")
	if err != nil || out != SteerOutcomeNext || cancels.Load() != 1 {
		t.Fatalf("steer = %q %v cancels=%d", out, err, cancels.Load())
	}
	waitFor(t, func() bool { active, ok := s.Active(); return ok && active.id == "d" })
	if got := pendingIDs(s); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("pending = %v", got)
	}
	if done := h.last(EventTurnDone); done == nil || done["interrupted"] != true {
		t.Fatalf("the interrupted turn must end as interrupted: %v", done)
	}
	s.Cancel(true)
	h.waitIdle(t)
}

func TestSteerAfterTurnEndedDoesNotInterrupt(t *testing.T) {
	h := newHarness()
	gate := h.hold("a")
	s, cancels, _ := steerSession(t, h, SteerDeferred)
	out, err := s.Steer(context.Background(), "d")
	if err != nil || out != SteerOutcomeNext || cancels.Load() != 0 || s.CancelRequested() {
		t.Fatalf("steer = %q %v cancels=%d", out, err, cancels.Load())
	}
	if got := pendingIDs(s); !slices.Equal(got, []string{"d", "b", "c"}) {
		t.Fatalf("pending = %v", got)
	}
	close(gate)
	h.waitIdle(t)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !slices.Equal(h.ran, []string{"a", "d", "b", "c"}) {
		t.Fatalf("ran = %v", h.ran)
	}
}

func TestSteerWithoutRuntimeHookInterrupts(t *testing.T) {
	h := newHarness()
	h.hold("a")
	cfg := h.config()
	var cancels atomic.Int32
	cfg.CancelTurn = func() { cancels.Add(1) }
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{id: "a"}, nil)
	waitFor(t, func() bool { return s.Snapshot().Busy })
	_, _ = s.Enqueue(&testItem{id: "b"}, nil)
	if out, err := s.Steer(context.Background(), "b"); err != nil || out != SteerOutcomeNext || cancels.Load() != 1 {
		t.Fatalf("steer = %q %v cancels=%d", out, err, cancels.Load())
	}
	h.waitIdle(t)
}

func TestSteerErrors(t *testing.T) {
	h := newHarness()
	gate := h.hold("a")
	s, _, _ := steerSession(t, h, SteerJoined)
	if _, err := s.Steer(context.Background(), " "); err == nil {
		t.Fatal("empty id")
	}
	if _, err := s.Steer(context.Background(), "a"); !errors.Is(err, ErrItemActive) {
		t.Fatalf("active item: %v", err)
	}
	if _, err := s.Steer(context.Background(), "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown item: %v", err)
	}
	close(gate)
	h.waitIdle(t)
}

func TestSteerPanicIsRejection(t *testing.T) {
	h := newHarness()
	h.hold("a")
	cfg := h.config()
	cfg.SteerTurn = func(context.Context, *testItem, *testItem) SteerResult { panic("boom") }
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{id: "a"}, nil)
	waitFor(t, func() bool { return s.Snapshot().Busy })
	_, _ = s.Enqueue(&testItem{id: "b"}, nil)
	if out, err := s.Steer(context.Background(), "b"); err != nil || out != SteerOutcomeNext {
		t.Fatalf("steer = %q %v", out, err)
	}
	s.Cancel(true)
	h.waitIdle(t)
}

func TestSteerItemRemovedDuringDelivery(t *testing.T) {
	for _, result := range []SteerResult{SteerJoined, SteerRejected} {
		h := newHarness()
		gate := h.hold("a")
		cfg := h.config()
		var s *Session[*testItem]
		cfg.SteerTurn = func(_ context.Context, _, item *testItem) SteerResult {
			_ = s.Remove(item.id)
			return result
		}
		s = New(cfg)
		_, _ = s.Enqueue(&testItem{id: "a"}, nil)
		waitFor(t, func() bool { return s.Snapshot().Busy })
		_, _ = s.Enqueue(&testItem{id: "b"}, nil)
		out, err := s.Steer(context.Background(), "b")
		if result == SteerJoined && (err != nil || out != SteerOutcomeJoined) {
			t.Fatalf("delivered then removed = %q %v", out, err)
		}
		if result == SteerRejected && !errors.Is(err, ErrNotFound) {
			t.Fatalf("rejected then removed = %q %v", out, err)
		}
		close(gate)
		h.waitIdle(t)
	}
}

func TestCancelEndsAnInFlightSteer(t *testing.T) {
	for _, clearQueue := range []bool{false, true} {
		h := newHarness()
		h.hold("a")
		cfg := h.config()
		cfg.Capacity = 8
		var cancels atomic.Int32
		cfg.CancelTurn = func() { cancels.Add(1) }
		delivering := make(chan struct{})
		cfg.SteerTurn = func(ctx context.Context, _, _ *testItem) SteerResult {
			close(delivering)
			<-ctx.Done()
			return SteerRejected
		}
		s := New(cfg)
		_, _ = s.Enqueue(&testItem{id: "a"}, nil)
		waitFor(t, func() bool { return s.Snapshot().Busy })
		_, _ = s.Enqueue(&testItem{id: "b"}, nil)
		_, _ = s.Enqueue(&testItem{id: "c"}, nil)
		steered := make(chan SteerOutcome, 1)
		go func() {
			out, _ := s.Steer(context.Background(), "c")
			steered <- out
		}()
		<-delivering
		stale := make(chan bool, 1)
		go func() { stale <- s.CancelActive("other", clearQueue) }()
		select {
		case <-steered:
			t.Fatal("a cancel of another turn ended the steer")
		case <-time.After(50 * time.Millisecond):
		}
		cancelled := make(chan struct{})
		go func() { s.Cancel(clearQueue); close(cancelled) }()
		select {
		case <-cancelled:
		case <-time.After(2 * time.Second):
			t.Fatal("cancel waited for the steer delivery")
		}
		if out := <-steered; out != SteerOutcomeNext || cancels.Load() != 1 || <-stale {
			t.Fatalf("steer = %q, interrupts = %d", out, cancels.Load())
		}
		h.waitIdle(t)
		want := []string{"a", "c", "b"}
		if clearQueue {
			want = []string{"a"}
		}
		h.mu.Lock()
		ran := slices.Clone(h.ran)
		h.mu.Unlock()
		if !slices.Equal(ran, want) {
			t.Fatalf("clearQueue=%v ran %v, want %v", clearQueue, ran, want)
		}
	}
}

func TestASteerOutlivesItsCaller(t *testing.T) {
	h := newHarness()
	gate := h.hold("a")
	cfg := h.config()
	cfg.Capacity = 8
	var cancels atomic.Int32
	cfg.CancelTurn = func() { cancels.Add(1) }
	cfg.SteerTurn = func(ctx context.Context, _, _ *testItem) SteerResult {
		select {
		case <-ctx.Done():
			return SteerRejected
		case <-time.After(80 * time.Millisecond):
			return SteerJoined
		}
	}
	s := New(cfg)
	_, _ = s.Enqueue(&testItem{id: "a"}, nil)
	waitFor(t, func() bool { return s.Snapshot().Busy })
	_, _ = s.Enqueue(&testItem{id: "b"}, nil)
	_, _ = s.Enqueue(&testItem{id: "c"}, nil)
	ctx, leave := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer leave()
	if out, err := s.Steer(ctx, "c"); err != nil || out != SteerOutcomeJoined || cancels.Load() != 0 {
		t.Fatalf("a delivery whose caller left was abandoned: %q %v, interrupts = %d", out, err, cancels.Load())
	}
	if got := pendingIDs(s); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("pending = %v", got)
	}
	close(gate)
	h.waitIdle(t)
}

func TestSteerIdleSessionMovesToHead(t *testing.T) {
	h := newHarness()
	cfg := h.config()
	cfg.Capacity = 8
	s := New(cfg)
	s.SeedForTest([]*testItem{{id: "a"}, {id: "b"}})
	if out, err := s.Steer(context.Background(), "b"); err != nil || out != SteerOutcomeNext {
		t.Fatalf("steer = %q %v", out, err)
	}
	if got := pendingIDs(s); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("pending = %v", got)
	}
}
