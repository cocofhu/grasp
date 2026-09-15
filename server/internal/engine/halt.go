package engine

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// Halt stops the priority-then-FIFO dispatcher from admitting new runs and
// prevents new StartRun/resume paths from launching workload.
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

// IsHalted reports whether the scheduler is stopped for shutdown drain.
func (e *Engine) IsHalted() bool {
	e.haltMu.RLock()
	defer e.haltMu.RUnlock()
	return e.halted
}

// CancelQueuedRuns marks every queued run cancelled immediately.
func (e *Engine) CancelQueuedRuns() int {
	var runs []models.Run
	if err := e.db.Where("status = ?", "queued").Find(&runs).Error; err != nil {
		log.Error().Err(err).Msg("cancel queued runs: query failed")
		return 0
	}
	for _, r := range runs {
		e.finish(r.ID, "cancelled")
	}
	if n := len(runs); n > 0 {
		log.Info().Int("count", n).Msg("runs cancelled (queued)")
	}
	return len(runs)
}

// WaitAgentReact polls until no agent/react node is actively running, or until
// deadline. Returns true when the wait ended by timeout (runs were force-cancelled).
func (e *Engine) WaitAgentReact(ctx context.Context, deadline time.Time) bool {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		active := e.countActiveAgentReact()
		if active == 0 {
			log.Info().Msg("waiting runs: all agent/react completed")
			return false
		}
		if time.Now().After(deadline) {
			n := e.forceCancelActiveAgentReact()
			log.Warn().Int("runs", n).Msg("timeout force")
			return true
		}
		select {
		case <-ctx.Done():
			e.forceCancelActiveAgentReact()
			return true
		case <-ticker.C:
			log.Info().Int("active", active).Msg("waiting runs")
		}
	}
}

func (e *Engine) countActiveAgentReact() int {
	var n int64
	e.db.Model(&models.StateRun{}).
		Where("status = ? AND node_type IN ?", "running", []string{"agent", "react", "grasp", "approve"}).
		Count(&n)
	return int(n)
}

func (e *Engine) forceCancelActiveAgentReact() int {
	var states []models.StateRun
	if err := e.db.Where("status = ? AND node_type IN ?", "running", []string{"agent", "react", "grasp", "approve"}).
		Find(&states).Error; err != nil {
		log.Error().Err(err).Msg("force cancel agent/react: query failed")
		return 0
	}
	seen := map[string]bool{}
	cancelled := 0
	for _, sr := range states {
		if seen[sr.RunID] {
			continue
		}
		seen[sr.RunID] = true
		var run models.Run
		if e.db.First(&run, "id = ?", sr.RunID).Error != nil || run.Status != "running" {
			continue
		}
		logDB(e.db.Model(&models.StateRun{}).
			Where("run_id = ? AND status = ?", sr.RunID, "running").
			Updates(map[string]any{"status": "cancelled", "error": "shutdown grace 超时"}), sr.RunID, "force cancel state_run")
		e.finish(sr.RunID, "cancelled")
		cancelled++
	}
	if cancelled > 0 {
		log.Info().Int("count", cancelled).Msg("runs cancelled (timeout)")
	}
	return cancelled
}
