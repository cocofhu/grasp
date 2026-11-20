package oneshot

import (
	"sync"

	"backend/internal/provider"
)

// ledgerSessions bounds how many native sessions a ledger remembers.
const ledgerSessions = 256

// sessionLedger keeps, per native session, the baselines that turn a CLI's
// running session counters into per-turn usage. It belongs to the Provider,
// not to one engine: a session resumed in a replacement process continues the
// CLI's counters, so its first turn there is differenced against the last
// turn of the process it replaced.
type sessionLedger struct {
	mu       sync.Mutex
	order    []string
	sessions map[string]*sessionBaseline
}

type sessionBaseline struct {
	// models is the last per-model running total the CLI reported.
	models map[string]provider.TokenUsage
	// flat is the flat counter plus usage already reported on partial turns.
	flat provider.TokenUsage
}

// baseline returns the session's entry, creating it and evicting the oldest
// beyond ledgerSessions. The caller holds l.mu.
func (l *sessionLedger) baseline(session string) *sessionBaseline {
	if b, ok := l.sessions[session]; ok {
		return b
	}
	if l.sessions == nil {
		l.sessions = map[string]*sessionBaseline{}
	}
	if len(l.order) == ledgerSessions {
		delete(l.sessions, l.order[0])
		l.order = l.order[1:]
	}
	b := &sessionBaseline{}
	l.sessions[session] = b
	l.order = append(l.order, session)
	return b
}

// modelDelta differences per-model running totals against the session's last
// report. A counter that went backwards means the CLI restarted its totals;
// the report is then taken as this turn's usage and flagged uncertain.
func (l *sessionLedger) modelDelta(session string, current map[string]provider.TokenUsage) (map[string]provider.TokenUsage, bool) {
	if session == "" {
		return current, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.baseline(session)
	delta := make(map[string]provider.TokenUsage, len(current))
	var reset bool
	for model, u := range current {
		previous := b.models[model]
		if u.InputTokens < previous.InputTokens || u.OutputTokens < previous.OutputTokens || u.CacheReadTokens < previous.CacheReadTokens || u.CacheWriteTokens < previous.CacheWriteTokens {
			reset = true
		}
		delta[model] = provider.TokenUsage{
			InputTokens:      u.InputTokens - previous.InputTokens,
			OutputTokens:     u.OutputTokens - previous.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens - previous.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens - previous.CacheWriteTokens,
		}
	}
	b.models = make(map[string]provider.TokenUsage, len(current))
	for model, u := range current {
		b.models[model] = u
	}
	if reset {
		return current, true
	}
	return delta, false
}

// reconcileFlat reconciles process-local model buckets against a flat
// native-session counter. The baseline includes already reported partial
// turns, so a later cumulative snapshot cannot ledger those failed/cancelled
// attempts a second time.
func (l *sessionLedger) reconcileFlat(session string, current *provider.TokenUsage, usage map[string]provider.TokenUsage) (map[string]provider.TokenUsage, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := provider.TokenUsage{}
	var b *sessionBaseline
	if session != "" {
		b = l.baseline(session)
		previous = b.flat
	}
	known := sumUsage(usage)
	var uncertain bool
	if current != nil {
		if current.InputTokens < previous.InputTokens || current.OutputTokens < previous.OutputTokens || current.CacheReadTokens < previous.CacheReadTokens || current.CacheWriteTokens < previous.CacheWriteTokens {
			previous = provider.TokenUsage{}
			uncertain = true
		}
		delta := provider.TokenUsage{
			InputTokens:      current.InputTokens - previous.InputTokens,
			OutputTokens:     current.OutputTokens - previous.OutputTokens,
			CacheReadTokens:  current.CacheReadTokens - previous.CacheReadTokens,
			CacheWriteTokens: current.CacheWriteTokens - previous.CacheWriteTokens,
		}
		missing := provider.TokenUsage{
			InputTokens:      max(delta.InputTokens-known.InputTokens, 0),
			OutputTokens:     max(delta.OutputTokens-known.OutputTokens, 0),
			CacheReadTokens:  max(delta.CacheReadTokens-known.CacheReadTokens, 0),
			CacheWriteTokens: max(delta.CacheWriteTokens-known.CacheWriteTokens, 0),
		}
		if missing != (provider.TokenUsage{}) {
			uncertain = uncertain || len(usage) > 0
			if usage == nil {
				usage = map[string]provider.TokenUsage{}
			}
			usage["default"] = addUsage(usage["default"], missing)
		}
		uncertain = uncertain || known.InputTokens > delta.InputTokens || known.OutputTokens > delta.OutputTokens || known.CacheReadTokens > delta.CacheReadTokens || known.CacheWriteTokens > delta.CacheWriteTokens
		if len(usage) == 0 {
			usage = map[string]provider.TokenUsage{"default": {}}
		}
	}
	if b != nil {
		b.flat = addUsage(previous, sumUsage(usage))
	}
	return usage, uncertain
}
