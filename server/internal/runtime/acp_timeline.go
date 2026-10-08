package runtime

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// acpTimelineEntry is the platform-side in-memory ACP timeline snapshot for one
// run+node while the sandbox session is live. It is the authoritative read path
// for nodeEvents during execution; cold FetchEventLogLastTurn is a supplement
// only. Snapshots are current-turn rails (last prompt_begin), never a
// full-session concatenation — otherwise hard-refresh seeds stitch prior turns
// into the live streaming bubble.
type acpTimelineEntry struct {
	events    []models.AcpEvent
	live      bool
	ready     bool // at least one successful ingest or emit write
	updatedAt time.Time
}

type acpTimelineStore struct {
	mu      sync.Mutex
	entries map[string]*acpTimelineEntry
	ingest  map[string]context.CancelFunc
	// pull refreshes one node from its sandbox event log; a field so tests can
	// count dials without a bridge.
	pull func(ctx context.Context, runID, nodeID string, reader *sandbox.EventLogReader)
}

// timelineIngestEvery is the event-log poll cadence while a turn is running.
var timelineIngestEvery = 2 * time.Second

func newAcpTimelineStore() *acpTimelineStore {
	s := &acpTimelineStore{
		entries: map[string]*acpTimelineEntry{},
		ingest:  map[string]context.CancelFunc{},
	}
	s.pull = s.refreshFromReader
	return s
}

// acpTurnBusy reports whether the event log needs polling: a turn is running
// that this client is not already streaming into the timeline. When streamed,
// this client's own turn is skipped; the bridge runs one turn at a time, so
// nothing else can be running meanwhile. nil means unknown, so the ingest
// keeps polling.
func acpTurnBusy(acp *sandbox.ACPClient, streamed bool) func() bool {
	if acp == nil {
		return nil
	}
	return func() bool {
		if acp.TurnInFlight() {
			return !streamed
		}
		return acp.BridgeState().Busy
	}
}

func timelineKey(runID, nodeID string) string { return runID + "|" + nodeID }

func (s *acpTimelineStore) begin(runID, nodeID string) {
	key := timelineKey(runID, nodeID)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		e = &acpTimelineEntry{live: true}
		s.entries[key] = e
	} else {
		e.live = true
	}
	e.updatedAt = time.Now()
}

// replace unconditionally installs events (live streamChat absolute snapshots).
func (s *acpTimelineStore) replace(runID, nodeID string, events []models.AcpEvent) {
	if s == nil {
		return
	}
	key := timelineKey(runID, nodeID)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		e = &acpTimelineEntry{live: true}
		s.entries[key] = e
	}
	e.ready = true
	e.events = append([]models.AcpEvent(nil), events...)
	e.updatedAt = time.Now()
}

func (s *acpTimelineStore) upsert(runID, nodeID string, events []models.AcpEvent) {
	if s == nil {
		return
	}
	key := timelineKey(runID, nodeID)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		e = &acpTimelineEntry{live: true}
		s.entries[key] = e
	}
	e.ready = true
	e.events = mergeTurnEvents(e.events, events)
	e.updatedAt = time.Now()
}

// mergeTurnEvents merges timeline snapshots for the current ReAct turn.
//
// Within a turn, prefer the longer message/thought rails (growth / avoid stale
// shorter ingest). Across turns — when the message rail resets to empty or
// rails diverge (neither is a prefix of the other) — take incoming so a new
// prompt's snapshot can replace the previous turn (or a stale full-session photo).
func mergeTurnEvents(prev, incoming []models.AcpEvent) []models.AcpEvent {
	if len(incoming) == 0 {
		if len(prev) == 0 {
			return nil
		}
		return prev
	}
	if len(prev) == 0 {
		return append([]models.AcpEvent(nil), incoming...)
	}

	prevMsg, prevThought := eventRails(prev)
	inMsg, inThought := eventRails(incoming)

	// Strong reset: message rail emptied → new prompt_begin turn.
	if prevMsg != "" && inMsg == "" {
		return append([]models.AcpEvent(nil), incoming...)
	}

	// Same-turn growth: prev rails are prefixes of incoming.
	if strings.HasPrefix(inMsg, prevMsg) && strings.HasPrefix(inThought, prevThought) {
		return append([]models.AcpEvent(nil), incoming...)
	}
	// Stale shorter snapshot of the same turn: incoming is a prefix of prev
	// (also covers omitted thought/message on a partial re-read).
	if strings.HasPrefix(prevMsg, inMsg) && strings.HasPrefix(prevThought, inThought) {
		return prev
	}
	// Divergent rails (new turn content that does not continue prior text).
	return append([]models.AcpEvent(nil), incoming...)
}

// pickLongerEvents is retained for tests / callers that only need length-based
// merge; prefer mergeTurnEvents for timeline ingest.
func pickLongerEvents(prev, incoming []models.AcpEvent) []models.AcpEvent {
	return mergeTurnEvents(prev, incoming)
}

func eventRails(events []models.AcpEvent) (message, thought string) {
	for _, ev := range events {
		switch ev.Kind {
		case "message":
			if ev.Text != "" {
				message = ev.Text
			}
		case "thought":
			if ev.Text != "" {
				thought = ev.Text
			}
		}
	}
	return message, thought
}

func (s *acpTimelineStore) stop(runID, nodeID string) {
	if s == nil {
		return
	}
	key := timelineKey(runID, nodeID)
	s.mu.Lock()
	if cancel, ok := s.ingest[key]; ok {
		cancel()
		delete(s.ingest, key)
	}
	if e, ok := s.entries[key]; ok {
		e.live = false
		e.updatedAt = time.Now()
	}
	s.mu.Unlock()
}

// startIngest polls the sandbox event log for runID/nodeID. busy gates the
// poll: each pull dials the bridge /ws, so an idle parked session must not be
// dialed every tick. A nil busy polls unconditionally.
func (s *acpTimelineStore) startIngest(runID, nodeID, host string, port int, password string, busy func() bool) {
	if s == nil || host == "" || port <= 0 {
		return
	}
	key := timelineKey(runID, nodeID)
	s.begin(runID, nodeID)

	s.mu.Lock()
	if cancel, ok := s.ingest[key]; ok {
		cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ingest[key] = cancel
	s.mu.Unlock()

	go s.ingestLoop(ctx, runID, nodeID, host, port, password, busy)
}

func (s *acpTimelineStore) ingestLoop(ctx context.Context, runID, nodeID, host string, port int, password string, busy func() bool) {
	// Keep one cookie for this ingest lifecycle; polling must not allocate a
	// new bridge session (and hash the login secret) every two seconds.
	reader := sandbox.NewEventLogReader(host, port, password)
	// Immediate first pull so cold page loads see history without waiting.
	s.pull(ctx, runID, nodeID, reader)

	ticker := time.NewTicker(timelineIngestEvery)
	defer ticker.Stop()
	wasBusy := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := busy == nil || busy()
			// One more pull after the turn ends picks up its final frames.
			if now || wasBusy {
				s.pull(ctx, runID, nodeID, reader)
			}
			wasBusy = now
		}
	}
}

func (s *acpTimelineStore) refreshFromSandbox(ctx context.Context, runID, nodeID, host string, port int, password string) {
	s.refreshFromReader(ctx, runID, nodeID, sandbox.NewEventLogReader(host, port, password))
}

func (s *acpTimelineStore) refreshFromReader(ctx context.Context, runID, nodeID string, reader *sandbox.EventLogReader) {
	// Last turn only — full-session FetchEventLog would lock a concatenated
	// photo into the timeline via upsert and poison hard-refresh seeds.
	res, _, err := reader.LastTurn(ctx)
	if err != nil || res == nil {
		return // keep existing snapshot on transient bridge failures
	}
	// Same AcpEvents converter as live ReAct (plan g1.2) — full Thought, no fork.
	s.upsert(runID, nodeID, res.AcpEvents())
}

func (s *acpTimelineStore) get(runID, nodeID string) (acpTimelineEntry, bool) {
	if s == nil {
		return acpTimelineEntry{}, false
	}
	key := timelineKey(runID, nodeID)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok || !e.ready {
		return acpTimelineEntry{}, false
	}
	return *e, true
}

func (s *acpTimelineStore) page(runID, nodeID, cursor string, limit int) ([]models.AcpEvent, string, bool, bool, bool) {
	e, ok := s.get(runID, nodeID)
	if !ok {
		return nil, "", false, false, false
	}
	ev, next, more := pageTimelineEvents(e.events, cursor, limit)
	return ev, next, more, e.live, true
}

// pageTimelineEvents slices a chronological event array for cursor/limit
// pagination. The first page returns the most recent limit items.
func pageTimelineEvents(events []models.AcpEvent, cursor string, limit int) ([]models.AcpEvent, string, bool) {
	total := len(events)
	if total == 0 {
		return nil, "", false
	}
	if limit <= 0 {
		limit = 20
	}
	if cursor == "" {
		start := total - limit
		if start < 0 {
			start = 0
		}
		page := events[start:]
		return page, strconv.Itoa(start), start > 0
	}
	end, err := strconv.Atoi(cursor)
	if err != nil || end <= 0 {
		return nil, "", false
	}
	if end > total {
		end = total
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	page := events[start:end]
	return page, strconv.Itoa(start), start > 0
}
