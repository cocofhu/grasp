package chatsession

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
)

type persisted struct {
	mu     sync.Mutex
	states []string
}

func (p *persisted) record(active *testItem, hasActive bool, pending []*testItem) {
	ids := make([]string, 0, len(pending))
	for _, it := range pending {
		ids = append(ids, it.id)
	}
	head := ""
	if hasActive {
		head = active.id + "*"
	}
	p.mu.Lock()
	p.states = append(p.states, head+"|"+strings.Join(ids, ","))
	p.mu.Unlock()
}

func (p *persisted) last() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.states) == 0 {
		return ""
	}
	return p.states[len(p.states)-1]
}

func TestSessionPersistTracksUnfinishedTurns(t *testing.T) {
	h := newHarness()
	var p persisted
	cfg := h.config()
	cfg.Capacity = 4
	cfg.Persist = p.record
	s := New(cfg)

	releaseA := h.hold("a")
	if _, err := s.Enqueue(&testItem{id: "a"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.last(); got != "|a" && got != "a*|" {
		t.Fatalf("Enqueue returned before storing the turn: %q", got)
	}
	waitFor(t, func() bool { return p.last() == "a*|" })
	for _, id := range []string{"b", "c", "d"} {
		h.hold(id)
		if _, err := s.Enqueue(&testItem{id: id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := p.last(); got != "a*|b,c,d" {
		t.Fatalf("after enqueue = %q", got)
	}
	if err := s.Remove("c"); err != nil {
		t.Fatal(err)
	}
	if got := p.last(); got != "a*|b,d" {
		t.Fatalf("after remove = %q", got)
	}
	if err := s.Reorder([]string{"d", "b"}); err != nil {
		t.Fatal(err)
	}
	if got := p.last(); got != "a*|d,b" {
		t.Fatalf("after reorder = %q", got)
	}
	close(releaseA)
	waitFor(t, func() bool { return p.last() == "d*|b" })
	s.Cancel(true)
	if got := p.last(); got != "d*|" {
		t.Fatalf("after clearing cancel = %q", got)
	}
	h.waitIdle(t)
	if got := p.last(); got != "|" {
		t.Fatalf("finished session still stores %q", got)
	}
}

func TestSessionRestoreRunsStoredTurnsInOrder(t *testing.T) {
	h := newHarness()
	var p persisted
	cfg := h.config()
	cfg.Capacity = 1
	cfg.Persist = p.record
	s := New(cfg)
	s.Restore(nil)
	if len(p.states) != 0 {
		t.Fatal("an empty restore must not store anything")
	}
	s.Restore([]*testItem{{id: "x"}, {id: "y"}})
	h.waitIdle(t)
	h.mu.Lock()
	ran := strings.Join(h.ran, ",")
	h.mu.Unlock()
	if ran != "x,y" {
		t.Fatalf("restored turns ran %q, want x,y past the capacity of 1", ran)
	}
	if got := p.last(); got != "|" {
		t.Fatalf("finished restore still stores %q", got)
	}
}

func TestSessionPersistNeverRegresses(t *testing.T) {
	var p persisted
	s := New(Config[*testItem]{View: func(it *testItem) ItemView { return ItemView{ID: it.id} }, Persist: p.record})
	newer := persistState[*testItem]{revision: 5, pending: []*testItem{{id: "new"}}}
	older := persistState[*testItem]{revision: 4, pending: []*testItem{{id: "old"}}}
	s.persist(newer)
	s.persist(older)
	if got := p.last(); got != "|new" || len(p.states) != 1 {
		t.Fatalf("states = %q, an older state must be skipped", p.states)
	}
}

type storedItem struct {
	ID   string
	Text string
}

func TestPendingStoreKeepsOrderAndEnqueueTime(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "pending.db"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewPendingStore(db)
	persist := Persister(store, "test", "k1", func(it *storedItem) string { return it.ID })
	persist(&storedItem{ID: "a", Text: "one"}, true, []*storedItem{{ID: "b", Text: "two"}})
	if err := store.Save("test", "k0", []PendingTurn{{ID: "z", Payload: []byte(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("other", "k1", []PendingTurn{{ID: "o", Payload: []byte(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	sessions, keys, err := store.Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, ",") != "k1,k0" {
		t.Fatalf("keys = %q, want oldest session first", keys)
	}
	first := sessions["k1"]
	if len(first) != 2 || first[0].ID != "a" || !first[0].Started || first[1].ID != "b" || first[1].Started ||
		string(first[1].Payload) != `{"ID":"b","Text":"two"}` {
		t.Fatalf("k1 turns = %+v", first)
	}
	enqueued := first[1].EnqueuedAt

	time.Sleep(20 * time.Millisecond)
	persist(&storedItem{ID: "b", Text: "two"}, true, []*storedItem{{ID: "c"}})
	sessions, _, _ = store.Load("test")
	again := sessions["k1"]
	if len(again) != 2 || again[0].ID != "b" || !again[0].Started || again[1].ID != "c" {
		t.Fatalf("rewritten k1 turns = %+v", again)
	}
	if !again[0].EnqueuedAt.Equal(enqueued) {
		t.Fatalf("rewrite moved the enqueue time from %v to %v", enqueued, again[0].EnqueuedAt)
	}
	if err := store.Clear("test", "k1"); err != nil {
		t.Fatal(err)
	}
	sessions, _, _ = store.Load("test")
	if len(sessions["k1"]) != 0 || len(sessions["k0"]) != 1 {
		t.Fatalf("clear touched other sessions: %+v", sessions)
	}
	if other, _, _ := store.Load("other"); len(other["k1"]) != 1 {
		t.Fatal("surfaces must not share rows")
	}
}

func TestPendingStoreWithoutDatabaseStoresNothing(t *testing.T) {
	var nilStore *PendingStore
	if Persister(nilStore, "s", "k", func(it *storedItem) string { return it.ID }) != nil {
		t.Fatal("a store without a database must not install a persist hook")
	}
	if err := NewPendingStore(nil).Save("s", "k", []PendingTurn{{ID: "x"}}); err != nil {
		t.Fatal(err)
	}
	if sessions, keys, err := nilStore.Load("s"); sessions != nil || keys != nil || err != nil {
		t.Fatal("nil store load must be empty")
	}
}
