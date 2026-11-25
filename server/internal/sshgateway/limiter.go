package sshgateway

import (
	"sync"
	"time"
)

// failLimiter blocks a source address after limit failed handshakes within
// window. A zero limit disables it.
type failLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

func newFailLimiter(limit int, window time.Duration) *failLimiter {
	return &failLimiter{limit: limit, window: window, now: time.Now, hits: map[string][]time.Time{}}
}

func (l *failLimiter) blocked(addr string) bool {
	if l.limit <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(addr)) >= l.limit
}

func (l *failLimiter) fail(addr string) {
	if l.limit <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[addr] = append(l.pruneLocked(addr), l.now())
	if len(l.hits) > 4096 {
		for a := range l.hits {
			if len(l.pruneLocked(a)) == 0 {
				delete(l.hits, a)
			}
		}
	}
}

func (l *failLimiter) pruneLocked(addr string) []time.Time {
	cutoff := l.now().Add(-l.window)
	hits := l.hits[addr]
	i := 0
	for i < len(hits) && !hits[i].After(cutoff) {
		i++
	}
	hits = hits[i:]
	if len(hits) == 0 {
		delete(l.hits, addr)
		return nil
	}
	l.hits[addr] = hits
	return hits
}
