// Package shutdown holds process-wide drain state for K8s SIGTERM handling.
package shutdown

import (
	"sync"
	"time"
)

// Coordinator tracks shutting-down state and the grace window shared by
// /api/health, the HTTP drain middleware, and the shutdown orchestrator in main.
type Coordinator struct {
	mu        sync.RWMutex
	draining  bool
	startedAt time.Time
	grace     time.Duration
}

// New builds a coordinator with the given grace upper bound.
func New(grace time.Duration) *Coordinator {
	if grace <= 0 {
		grace = 600 * time.Second
	}
	return &Coordinator{grace: grace}
}

// BeginDraining marks the process as shutting down and records the start time
// used for grace_remaining_seconds.
func (c *Coordinator) BeginDraining() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.draining {
		return
	}
	c.draining = true
	c.startedAt = time.Now()
}

// IsDraining reports whether SIGTERM drain mode is active.
func (c *Coordinator) IsDraining() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.draining
}

// Deadline is startedAt + grace; waiting loops should stop at this instant.
func (c *Coordinator) Deadline() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.draining {
		return time.Now().Add(c.grace)
	}
	return c.startedAt.Add(c.grace)
}

// GraceRemainingSeconds returns seconds left until the grace deadline (min 0).
func (c *Coordinator) GraceRemainingSeconds() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.draining {
		return int(c.grace.Seconds())
	}
	rem := time.Until(c.startedAt.Add(c.grace))
	if rem <= 0 {
		return 0
	}
	return int(rem.Round(time.Second) / time.Second)
}
