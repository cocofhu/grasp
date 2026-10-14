package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// isAutoRetryable reports whether a failed node's outcome may be auto-retried
// from the failure position. It reads only oc.retryable — callers set that
// flag on RunAgent / finish-path transport faults and on CAPA A7 empty MCP
// surfaces (tools unreachable). Contract finalize misses (agent wrote other
// MCP traffic but skipped the reserved product), structured-gate rejects, and
// other deterministic faults leave the zero value false and are not
// auto-retried. Note: "计划未全部完成" returned as a RunAgent error still goes
// through execAgent and is therefore retryable. A refused Codex login is never
// retried: every attempt fails the same way until the user re-pastes the file.
func isAutoRetryable(oc nodeOutcome) bool {
	return oc.retryable && !codexLoginRejected(oc.err)
}

// codexLoginRejected reports a node failure caused by a refused or
// unrefreshable Codex login (runtime sentinel or the gateway's re-paste text).
func codexLoginRejected(msg string) bool {
	return strings.Contains(msg, runtime.CodexLoginRepasteMark)
}

// shortReason renders a failure message as a single trimmed line, capped in
// length, for a compact auto-retry trace detail.
func shortReason(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}

// autoRetryBackoff is the pause before re-entering a node on auto-retry, giving
// a transient fault (e.g. a flaky CI push, a sandbox/registry hiccup) a moment
// to clear before the fresh attempt. A package var (not const) so tests can
// zero it out.
var autoRetryBackoff = 5 * time.Second

// tryAutoRetry decides whether a failed node (with no explicit failure/rollback
// edge) should be re-run from its failure position, and if so records the
// attempt. It returns true when the caller should re-enter node.ID; false when
// the budget is exhausted, the fault is deterministic, or auto-retry is off.
// The failed StateRun was already persisted by the caller, so it stays as
// history and the re-entry opens a fresh visit (like a manual ResumeFrom).
func (e *Engine) tryAutoRetry(c *execCtx, node *models.Node, outcome nodeOutcome) bool {
	max := e.AutoRetryMax()
	if max <= 0 || !isAutoRetryable(outcome) || c.autoRetries[node.ID] >= max {
		return false
	}
	c.autoRetries[node.ID]++
	reason := outcome.err
	if strings.TrimSpace(reason) == "" {
		reason = outcome.outputMd
	}
	e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "resume",
		Detail: fmt.Sprintf("自动从失败位置重试 %d/%d:%s", c.autoRetries[node.ID], max, shortReason(reason))})
	log.Info().Str("run_id", c.run.ID).Str("node_id", node.ID).
		Int("attempt", c.autoRetries[node.ID]).Int("max", max).Str("err", outcome.err).
		Msg("auto-retrying failed node from failure position")
	if autoRetryBackoff > 0 {
		time.Sleep(autoRetryBackoff)
	}
	return true
}

// signalDispatch wakes the dispatcher without blocking (buffered, coalescing).
func (e *Engine) signalDispatch() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// reconcileInterrupted cleans up runs whose in-memory execution goroutine was
// lost on a process restart. A node's sandbox is a separate container that can
// keep running independently, but nothing in this fresh process will finalize
// it — leaving the run in a zombie "running" (or orphaned "waiting_human")
// state that never advances. We mark those runs (and their mid-flight node
// states) failed so they surface clearly and can be re-run. Runs legitimately
// paused at a human gate or react dialogue have no in-flight node state and are
// left untouched so they stay resumable.
//
// Runs still "queued" are also left untouched: they had not started executing,
// so they are safe to recover — the dispatcher (started right after this in
// New) admits them by priority then FIFO once slots are free. This is the
// durable-queue guarantee: a backlog survives a restart instead of being lost.
func (e *Engine) reconcileInterrupted() {
	orphans := map[string]bool{}

	// Node states caught mid-execution cannot resume: their driving goroutine
	// died with the old process.
	var running []models.StateRun
	if err := e.db.Where("status = ?", "running").Find(&running).Error; err != nil {
		log.Error().Err(err).Msg("startup reconciliation: query running node states failed")
	} else {
		for _, sr := range running {
			logDB(e.db.Model(&models.StateRun{}).Where("id = ?", sr.ID).Updates(map[string]any{
				"status": "failed",
				"error":  "服务重启中断,执行协程丢失,节点未收尾",
			}), sr.RunID, "reconcile state_run")
			orphans[sr.RunID] = true
		}
	}

	// Runs still flagged running had no live goroutine after restart.
	var runs []models.Run
	if err := e.db.Where("status = ?", "running").Find(&runs).Error; err != nil {
		log.Error().Err(err).Msg("startup reconciliation: query running runs failed")
	} else {
		for _, r := range runs {
			orphans[r.ID] = true
		}
	}

	for id := range orphans {
		var r models.Run
		if e.db.First(&r, "id = ?", id).Error != nil {
			continue
		}
		if r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled" {
			continue
		}
		logDB(e.db.Model(&models.Run{}).Where("id = ?", id).UpdateColumn("status", "failed"), id, "reconcile run")
		e.host.UnregisterRun(id)
		log.Warn().Str("run_id", id).Msg("reconciled interrupted run on startup -> failed")
	}
	if len(orphans) > 0 {
		log.Info().Int("count", len(orphans)).Msg("startup reconciliation complete")
	}
}
