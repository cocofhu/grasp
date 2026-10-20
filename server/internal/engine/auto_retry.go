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
