package engine

import "sync"

// brokerBuffer is how many frames a subscriber may fall behind before it is
// dropped. ACP streaming publishes many small frames per turn.
const brokerBuffer = 256

// Broker is a tiny per-run pub/sub used to push run updates to WebSocket
// subscribers (state trace + status changes).
type Broker struct {
	mu   sync.RWMutex
	subs map[string]map[*brokerSub]struct{}
}

type brokerSub struct {
	ch   chan []byte
	once sync.Once
}

func (s *brokerSub) close() { s.once.Do(func() { close(s.ch) }) }

// NewBroker builds an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: map[string]map[*brokerSub]struct{}{}}
}

// Subscribe registers a channel for a run's updates and returns an
// unsubscribe func. The channel is closed when the subscriber falls more than
// brokerBuffer frames behind: a missed frame (e.g. turn_done) must surface as
// a disconnect so the client reconnects and re-syncs, never as silent loss.
func (b *Broker) Subscribe(runID string) (<-chan []byte, func()) {
	s := &brokerSub{ch: make(chan []byte, brokerBuffer)}
	b.mu.Lock()
	if b.subs[runID] == nil {
		b.subs[runID] = map[*brokerSub]struct{}{}
	}
	b.subs[runID][s] = struct{}{}
	b.mu.Unlock()
	return s.ch, func() {
		b.remove(runID, s)
		s.close()
	}
}

func (b *Broker) remove(runID string, s *brokerSub) {
	b.mu.Lock()
	if m := b.subs[runID]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(b.subs, runID)
		}
	}
	b.mu.Unlock()
}

// Publish delivers a message to all subscribers of a run (non-blocking).
func (b *Broker) Publish(runID string, msg []byte) {
	var slow []*brokerSub
	b.mu.RLock()
	for s := range b.subs[runID] {
		select {
		case s.ch <- msg:
		default:
			slow = append(slow, s)
		}
	}
	b.mu.RUnlock()
	for _, s := range slow {
		b.remove(runID, s)
		s.close()
	}
}
