package engine

import (
	"github.com/rs/zerolog/log"
)

// Halt stops the priority-then-FIFO dispatcher from admitting new runs and
// prevents new StartRun/resume paths from launching workload. Drivers already
// in flight requeue their run at the next node boundary (see execute), and
// agent sandboxes keep running so the next process can reattach them.
func (e *Engine) Halt() {
	e.haltMu.Lock()
	e.halted = true
	e.haltMu.Unlock()
	log.Info().Msg("scheduler halted")
}

// Close halts admission and stops the dispatcher goroutine. Safe to call more
// than once. Tests should Close engines so leftover dispatch loops do not keep
// polling a closed *sql.DB after the case ends.
func (e *Engine) Close() {
	e.stopOnce.Do(func() {
		e.Halt()
		close(e.stop)
	})
}

// IsHalted reports whether the scheduler is stopped for shutdown.
func (e *Engine) IsHalted() bool {
	e.haltMu.RLock()
	defer e.haltMu.RUnlock()
	return e.halted
}

// ActiveDrivers reports admitted executions still reaching a safe boundary.
func (e *Engine) ActiveDrivers() int { e.sem.mu.Lock(); defer e.sem.mu.Unlock(); return e.sem.cur }
