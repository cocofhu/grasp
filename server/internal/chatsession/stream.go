package chatsession

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Stream frame types.
const (
	FrameSession = "session"
	FrameAcp     = "acp"
)

// DefaultStreamBuffer caps the frames kept for replaying the active turn.
const DefaultStreamBuffer = 2048

// subscriberGrace is how long a slow subscriber may block a frame before it is
// skipped for that subscriber.
const subscriberGrace = 3 * time.Second

// Event is one frame of a chat stream.
type Event struct {
	Seq int
	// Type is FrameSession (control frame) or FrameAcp (raw ACP frame in Data).
	Type    string
	Event   string
	Data    json.RawMessage
	Payload map[string]any
}

// Frame renders the event for a WebSocket client.
func (e Event) Frame() map[string]any {
	out := map[string]any{"type": e.Type, "seq": e.Seq}
	if e.Type == FrameAcp {
		out["data"] = e.Data
		return out
	}
	for k, v := range e.Payload {
		out[k] = v
	}
	out["event"] = e.Event
	return out
}

// Keep says what Emit does with the active turn's replay buffer.
type Keep int

const (
	// KeepNone sends the frame live only.
	KeepNone Keep = iota
	// KeepAppend adds the frame to the active turn.
	KeepAppend
	// KeepBegin starts a new turn with this frame.
	KeepBegin
	// KeepEnd sends the frame and clears the turn.
	KeepEnd
)

// Stream numbers frames, keeps the active turn for replay and fans frames out
// to subscribers. A subscriber gets a queue_state snapshot, the active turn
// and then live frames, so a reconnecting viewer catches up without gaps.
type Stream struct {
	mu      sync.Mutex
	max     int
	subs    map[*subscriber]struct{}
	nextSeq int
	turn    []Event
}

// NewStream builds a Stream keeping at most max turn frames (0 → default).
func NewStream(max int) *Stream {
	if max <= 0 {
		max = DefaultStreamBuffer
	}
	return &Stream{max: max, subs: map[*subscriber]struct{}{}}
}

// Session emits a control frame.
func (s *Stream) Session(event string, payload map[string]any, keep Keep) {
	s.Emit(Event{Type: FrameSession, Event: event, Payload: payload}, keep)
}

// Acp emits a raw ACP frame as part of the active turn.
func (s *Stream) Acp(raw json.RawMessage) {
	s.Emit(Event{Type: FrameAcp, Data: append(json.RawMessage(nil), raw...)}, KeepAppend)
}

// Emit assigns the next seq and sends ev to every subscriber.
func (s *Stream) Emit(ev Event, keep Keep) {
	s.mu.Lock()
	ev.Seq = s.nextSeq
	s.nextSeq++
	switch keep {
	case KeepBegin:
		s.turn = []Event{ev}
	case KeepAppend:
		s.turn = append(s.turn, ev)
		if len(s.turn) > s.max {
			// Keep the first frame (turn_begin) so a replay still opens the turn.
			s.turn = append(s.turn[:1], s.turn[len(s.turn)-s.max+1:]...)
		}
	case KeepEnd:
		s.turn = nil
	}
	subs := make([]*subscriber, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()
	for _, sub := range subs {
		sub.send(ev)
	}
}

// Subscribe returns a channel that first carries a queue_state built by head,
// then (when head reports busy) the active turn's frames after afterSeq, then
// live frames. head runs under the stream lock and must not emit. The channel
// closes when the returned func is called.
func (s *Stream) Subscribe(head func() (queueState map[string]any, busy bool), afterSeq int) (<-chan Event, func()) {
	s.mu.Lock()
	ch := make(chan Event, s.max+16)
	sub := &subscriber{ch: ch}
	qs, busy := head()
	ch <- Event{Seq: s.nextSeq - 1, Type: FrameSession, Event: EventQueueState, Payload: qs}
	if busy {
		for _, ev := range s.turn {
			if ev.Seq > afterSeq {
				ch <- ev
			}
		}
	}
	s.subs[sub] = struct{}{}
	s.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, sub)
			s.mu.Unlock()
			sub.close()
		})
	}
}

// LastSeq is the seq of the most recent frame (-1 before the first).
func (s *Stream) LastSeq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextSeq - 1
}

type subscriber struct {
	mu     sync.Mutex
	ch     chan Event
	closed bool
}

func (s *subscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

func (s *subscriber) send(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- ev:
		return
	default:
	}
	select {
	case s.ch <- ev:
	case <-time.After(subscriberGrace):
		log.Warn().Int("seq", ev.Seq).Str("type", ev.Type).Msg("chat stream subscriber too slow; frame skipped")
	}
}
