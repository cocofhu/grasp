package oneshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"backend/internal/provider"
)

// ledgerSessions bounds how many native sessions a ledger remembers.
const ledgerSessions = 256

// sessionLedger keeps, per native session, the baselines that turn a CLI's
// running session counters into per-turn usage. It belongs to the Provider,
// not to one engine: a session resumed in a replacement process continues the
// CLI's counters, so its first turn there is differenced against the last
// turn of the process it replaced. With a state directory the baselines are
// also saved there, for a session a restarted backend resumes.
type sessionLedger struct {
	mu       sync.Mutex
	order    []string
	sessions map[string]*sessionBaseline
}

type sessionBaseline struct {
	// Models is the last per-model running total the CLI reported.
	Models map[string]provider.TokenUsage `json:"models,omitempty"`
	// Flat is the flat counter plus usage already reported on partial turns.
	Flat provider.TokenUsage `json:"flat"`
}

// baseline returns the session's entry, loading it from dir or creating it,
// and evicting the oldest beyond ledgerSessions. The caller holds l.mu.
func (l *sessionLedger) baseline(dir, session string) *sessionBaseline {
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
	b := loadBaseline(dir, session)
	l.sessions[session] = b
	l.order = append(l.order, session)
	return b
}

// modelDelta differences per-model running totals against the session's last
// report. A counter that went backwards means the CLI restarted its totals;
// the report is then taken as this turn's usage and flagged uncertain.
func (l *sessionLedger) modelDelta(dir, session string, current map[string]provider.TokenUsage) (map[string]provider.TokenUsage, bool) {
	if session == "" {
		return current, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.baseline(dir, session)
	defer saveBaseline(dir, session, b)
	delta := make(map[string]provider.TokenUsage, len(current))
	var reset bool
	for model, u := range current {
		previous := b.Models[model]
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
	b.Models = make(map[string]provider.TokenUsage, len(current))
	for model, u := range current {
		b.Models[model] = u
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
func (l *sessionLedger) reconcileFlat(dir, session string, current *provider.TokenUsage, usage map[string]provider.TokenUsage) (map[string]provider.TokenUsage, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := provider.TokenUsage{}
	var b *sessionBaseline
	if session != "" {
		b = l.baseline(dir, session)
		previous = b.Flat
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
		b.Flat = addUsage(previous, sumUsage(usage))
		saveBaseline(dir, session, b)
	}
	return usage, uncertain
}

// baselinePath names the session's file under dir; session ids are hashed so
// any id is a safe file name.
func baselinePath(dir, session string) string {
	sum := sha256.Sum256([]byte(session))
	return filepath.Join(dir, "usage", hex.EncodeToString(sum[:16])+".json")
}

func loadBaseline(dir, session string) *sessionBaseline {
	b := &sessionBaseline{}
	if dir == "" {
		return b
	}
	raw, err := os.ReadFile(baselinePath(dir, session))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("oneshot: 读取用量基线失败: %v", err)
		}
		return b
	}
	if err := json.Unmarshal(raw, b); err != nil {
		log.Printf("oneshot: 用量基线无法解析，忽略: %v", err)
		return &sessionBaseline{}
	}
	return b
}

// saveBaseline writes the session's baseline atomically and keeps only the
// newest ledgerSessions files.
func saveBaseline(dir, session string, b *sessionBaseline) {
	if dir == "" {
		return
	}
	if err := writeBaseline(baselinePath(dir, session), b); err != nil {
		log.Printf("oneshot: 保存用量基线失败: %v", err)
		return
	}
	pruneBaselines(filepath.Join(dir, "usage"))
}

func writeBaseline(path string, b *sessionBaseline) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return provider.WriteFileAtomic(path, raw)
}

func pruneBaselines(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= ledgerSessions {
		return
	}
	type file struct {
		name string
		mod  time.Time
	}
	files := make([]file, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil && filepath.Ext(e.Name()) == ".json" {
			files = append(files, file{e.Name(), info.ModTime()})
		}
	}
	slices.SortFunc(files, func(a, b file) int { return a.mod.Compare(b.mod) })
	for _, f := range files[:max(len(files)-ledgerSessions, 0)] {
		_ = os.Remove(filepath.Join(dir, f.name))
	}
}
