package engine

import "sync"

// sema is a counting semaphore whose capacity (limit) can be changed at
// runtime. A fixed-size channel cannot be resized, but the platform settings
// page needs to raise/lower max_sandboxes live, so concurrency admission
// is a mutex-guarded counter instead.
//
// Semantics:
//   - TryAcquire takes a slot without blocking, returning false when full.
//   - Release frees a slot.
//   - SetLimit changes the capacity; lowering it does NOT preempt in-flight
//     holders — cur simply drains back below the new limit as runs finish.
type sema struct {
	mu    sync.Mutex
	limit int
	cur   int
}

func newSema(limit int) *sema {
	if limit <= 0 {
		limit = 1
	}
	return &sema{limit: limit}
}

// TryAcquire takes a slot if one is free, returning whether it succeeded.
func (s *sema) TryAcquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur < s.limit {
		s.cur++
		return true
	}
	return false
}

// ForceAcquire takes a slot even when the limit is reached. Runs that were
// already executing before a restart must not wait behind the limit.
func (s *sema) ForceAcquire() {
	s.mu.Lock()
	s.cur++
	s.mu.Unlock()
}

// Release frees a previously acquired slot.
func (s *sema) Release() {
	s.mu.Lock()
	if s.cur > 0 {
		s.cur--
	}
	s.mu.Unlock()
}

// SetLimit changes the capacity. Lowering it stops new admissions until cur
// drains below the new limit (already-running holders are never preempted).
func (s *sema) SetLimit(n int) {
	if n <= 0 {
		n = 1
	}
	s.mu.Lock()
	s.limit = n
	s.mu.Unlock()
}

// Limit returns the current capacity.
func (s *sema) Limit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}
