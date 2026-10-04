package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"sync/atomic"
)

func TestAcpTimelineStoreUpsertAndPage(t *testing.T) {
	s := newAcpTimelineStore()
	s.upsert("r1", "n1", []models.AcpEvent{{T: 1, Kind: "message", Text: "a"}})
	ev, next, more, live, ok := s.page("r1", "n1", "", 20)
	if !ok || !live || more || len(ev) != 1 || next != "0" {
		t.Fatalf("first page = ok=%v live=%v more=%v ev=%+v next=%q", ok, live, more, ev, next)
	}

	events := make([]models.AcpEvent, 25)
	for i := range events {
		events[i] = models.AcpEvent{T: i, Kind: "message", Text: "e"}
	}
	s.upsert("r1", "n1", events)
	ev, next, more, _, ok = s.page("r1", "n1", "", 20)
	if !ok || len(ev) != 20 || !more || next != "5" {
		t.Fatalf("paged want 20 hasMore, got len=%d more=%v next=%q", len(ev), more, next)
	}
	ev2, _, more2, _, _ := s.page("r1", "n1", next, 20)
	if len(ev2) != 5 || more2 {
		t.Fatalf("older page want 5 no more, got len=%d more=%v", len(ev2), more2)
	}
}

func TestAcpTimelineStoreStopMarksNotLive(t *testing.T) {
	s := newAcpTimelineStore()
	s.begin("r", "n")
	s.upsert("r", "n", []models.AcpEvent{{T: 1, Kind: "message", Text: "x"}})
	s.stop("r", "n")
	e, ok := s.get("r", "n")
	if !ok || e.live {
		t.Fatalf("after stop want ready snapshot live=false, got ok=%v live=%v", ok, e.live)
	}
}

func TestAcpTimelineIngestKeepsSnapshotOnRefreshFailure(t *testing.T) {
	s := newAcpTimelineStore()
	s.upsert("r", "n", []models.AcpEvent{{T: 1, Kind: "message", Text: "keep"}})
	// Unreachable host — must not clear the prior snapshot.
	s.refreshFromSandbox(context.Background(), "r", "n", "127.0.0.1", 1, "")
	e, ok := s.get("r", "n")
	if !ok || len(e.events) != 1 || e.events[0].Text != "keep" {
		t.Fatalf("snapshot should survive refresh failure: %+v ok=%v", e.events, ok)
	}
}

func TestPickLongerEvents(t *testing.T) {
	prev := []models.AcpEvent{{T: 1, Kind: "message", Text: "a"}}
	in := []models.AcpEvent{{T: 1, Kind: "message", Text: "a"}, {T: 2, Kind: "thought", Text: "b"}}
	got := pickLongerEvents(prev, in)
	if len(got) != 2 {
		t.Fatalf("want longer incoming, got %d", len(got))
	}
	// Same-turn stale shorter must not shrink.
	if got := pickLongerEvents(in, prev); len(got) != 2 {
		t.Fatalf("shorter same-turn incoming must not shrink snapshot, got %d", len(got))
	}
}

func TestMergeTurnEventsAllowsNewTurnToReplace(t *testing.T) {
	prev := []models.AcpEvent{
		{T: 0, Kind: "thought", Text: "turn1-thought"},
		{T: 1, Kind: "message", Text: "turn1-full-session-or-prior"},
	}
	incoming := []models.AcpEvent{
		{T: 0, Kind: "thought", Text: "turn2-thought"},
		{T: 1, Kind: "message", Text: "turn2-partial"},
	}
	got := mergeTurnEvents(prev, incoming)
	if len(got) != 2 || got[1].Text != "turn2-partial" {
		t.Fatalf("divergent new turn must replace prior photo, got %+v", got)
	}

	// Empty rails after a prior turn (prompt just began) must also replace.
	emptyTurn := []models.AcpEvent{}
	// empty incoming keeps prev (no seed yet) — use explicit empty message event set via replace path.
	reset := []models.AcpEvent{{T: 0, Kind: "message", Text: ""}}
	// Text "" is ignored by eventRails; simulate reset with thought-only new turn:
	thoughtOnly := []models.AcpEvent{{T: 0, Kind: "thought", Text: "new"}}
	got = mergeTurnEvents(prev, thoughtOnly)
	if len(got) != 1 || got[0].Text != "new" {
		t.Fatalf("new-turn thought-only must replace prior message photo, got %+v", got)
	}
	_ = emptyTurn
	_ = reset
}

func TestMergeTurnEventsEmptyMessageResetsPrior(t *testing.T) {
	prev := []models.AcpEvent{{T: 0, Kind: "message", Text: "prior-turn-body"}}
	// Incoming has thought but cleared message rail → new turn.
	incoming := []models.AcpEvent{{T: 0, Kind: "thought", Text: "thinking-now"}}
	got := mergeTurnEvents(prev, incoming)
	msg, thought := eventRails(got)
	if msg != "" || thought != "thinking-now" {
		t.Fatalf("want message cleared and thought current, got msg=%q thought=%q", msg, thought)
	}
}

func TestTimelineReplaceOverridesLongerPrior(t *testing.T) {
	s := newAcpTimelineStore()
	s.upsert("r", "n", []models.AcpEvent{
		{T: 0, Kind: "message", Text: "stitched-turn1-turn2-photo"},
	})
	s.replace("r", "n", []models.AcpEvent{
		{T: 0, Kind: "message", Text: "turn2-only"},
	})
	e, ok := s.get("r", "n")
	if !ok || len(e.events) != 1 || e.events[0].Text != "turn2-only" {
		t.Fatalf("replace must install current-turn snapshot, got %+v ok=%v", e.events, ok)
	}
}

func TestAcpTimelineIngestLoopCancels(t *testing.T) {
	s := newAcpTimelineStore()
	s.startIngest("r", "n", "127.0.0.1", 1, "", nil)
	time.Sleep(10 * time.Millisecond)
	s.stop("r", "n")
	// stop is idempotent
	s.stop("r", "n")
}

func TestAcpTimelineIngestPollsOnlyWhileBusy(t *testing.T) {
	prev := timelineIngestEvery
	timelineIngestEvery = 5 * time.Millisecond
	t.Cleanup(func() { timelineIngestEvery = prev })

	s := newAcpTimelineStore()
	var pulls atomic.Int32
	s.pull = func(context.Context, string, string, *sandbox.EventLogReader) { pulls.Add(1) }
	var busy atomic.Bool
	s.startIngest("r", "n", "127.0.0.1", 1, "", busy.Load)
	t.Cleanup(func() { s.stop("r", "n") })

	waitPulls := func(min int32) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for pulls.Load() < min && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if pulls.Load() < min {
			t.Fatalf("pulls=%d want >= %d", pulls.Load(), min)
		}
	}

	waitPulls(1) // cold first pull
	time.Sleep(60 * time.Millisecond)
	if n := pulls.Load(); n != 1 {
		t.Fatalf("idle session must not be polled, pulls=%d", n)
	}

	busy.Store(true)
	waitPulls(4)
	busy.Store(false)
	time.Sleep(30 * time.Millisecond)
	settled := pulls.Load()
	time.Sleep(60 * time.Millisecond)
	if n := pulls.Load(); n != settled {
		t.Fatalf("polling must stop after the turn ends: %d -> %d", settled, n)
	}
}

func TestAcpTurnBusyNilClient(t *testing.T) {
	if acpTurnBusy(nil) != nil {
		t.Fatal("nil client should poll unconditionally")
	}
	c := sandbox.NewACPClient("127.0.0.1", 1)
	busy := acpTurnBusy(c)
	if busy() {
		t.Fatal("fresh client is idle")
	}
	c.SeedBridgeForTest(sandbox.BridgeState{Known: true, Busy: true, RunningOpID: "op-x"}, "", "")
	if !busy() {
		t.Fatal("bridge busy should count")
	}
	c.SeedBridgeForTest(sandbox.BridgeState{Known: true}, "", "op-mine")
	if !busy() {
		t.Fatal("own in-flight turn should count")
	}
}
