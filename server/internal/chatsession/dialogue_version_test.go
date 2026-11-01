package chatsession

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

type memEpochStore struct {
	v   int64
	has bool
	err error
}

func (m *memEpochStore) LoadEpoch() (int64, bool) { return m.v, m.has }
func (m *memEpochStore) SaveEpoch(v int64) error {
	if m.err != nil {
		return m.err
	}
	m.v, m.has = v, true
	return nil
}

func withClock(t *testing.T, ms ...int64) {
	t.Helper()
	old := nowMilli
	i := 0
	nowMilli = func() int64 {
		v := ms[i]
		if i < len(ms)-1 {
			i++
		}
		return v
	}
	t.Cleanup(func() { nowMilli = old })
}

func TestNextEpochGrowsAcrossRestartsEvenWhenTheClockStepsBack(t *testing.T) {
	// Boots at 1000, 2000, then the clock steps back to 500, then a second
	// boot in the same millisecond.
	withClock(t, 1000, 2000, 500, 500)
	store := &memEpochStore{}
	var prev int64
	for i := 0; i < 4; i++ {
		e, err := NextEpoch(store)
		if err != nil {
			t.Fatal(err)
		}
		if e <= prev {
			t.Fatalf("boot %d: epoch %d not above %d", i, e, prev)
		}
		if store.v != e {
			t.Fatalf("boot %d: stored %d, handed out %d", i, store.v, e)
		}
		prev = e
	}
	if prev != 2002 {
		t.Fatalf("epoch after two stepped-back boots = %d, want 2002", prev)
	}
}

func TestNextEpochFailsWhenItCannotBeSaved(t *testing.T) {
	if _, err := NextEpoch(&memEpochStore{err: errors.New("disk")}); err == nil {
		t.Fatal("an unsaved epoch could repeat after a restart")
	}
}

func TestDialogueVersionsAreStrictlyIncreasing(t *testing.T) {
	if err := InitDialogueEpoch(&memEpochStore{}); err != nil {
		t.Fatal(err)
	}
	prev := NextDialogueVersion()
	for i := 0; i < 1000; i++ {
		v := NextDialogueVersion()
		if !prev.Less(v) {
			t.Fatalf("%v not after %v", v, prev)
		}
		prev = v
	}
	if !(DialogueVersion{Epoch: 1, Seq: 99}).Less(DialogueVersion{Epoch: 2, Seq: 1}) {
		t.Fatal("a newer epoch must win over any seq")
	}
}

func TestSessionViewAndActiveOnlyLiveEvents(t *testing.T) {
	h := newHarness()
	gate := h.hold("a")
	s := New(h.config())
	if s.SetLiveEventsIfActive([]models.AcpEvent{{Kind: "message", Text: "stale"}}) {
		t.Fatal("an idle session took a stream")
	}
	if _, err := s.Enqueue(&testItem{"a", "first"}, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, ok := s.Active(); return ok })
	if _, err := s.Enqueue(&testItem{"b", "second"}, nil); err != nil {
		t.Fatal(err)
	}
	if !s.SetLiveEventsIfActive([]models.AcpEvent{{Kind: "message", Text: "live"}}) {
		t.Fatal("active session refused its stream")
	}
	v := s.View()
	if !v.HasActive || v.Active.id != "a" || len(v.Pending) != 1 || v.Pending[0].id != "b" ||
		len(v.LiveEvents) != 1 || v.LiveEvents[0].Text != "live" {
		t.Fatalf("view=%+v", v)
	}
	close(gate)
	h.waitIdle(t)
	if v := s.View(); v.HasActive || len(v.Pending) != 0 || v.LiveEvents != nil {
		t.Fatalf("idle view=%+v", v)
	}
}

func TestDialogueEpochIsSafeForJavaScript(t *testing.T) {
	const maxSafe = 1<<53 - 1
	if e := DialogueEpoch(); e <= 0 || e > maxSafe {
		t.Fatalf("epoch %d is not an exact JavaScript number", e)
	}
}
