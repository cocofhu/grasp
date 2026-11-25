package services

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
)

// MemoryTrimPolicy controls page-cache trims of idle E2B guests. IdleAfter <= 0
// disables them.
type MemoryTrimPolicy struct {
	IdleAfter   time.Duration
	Interval    time.Duration
	MinCacheMiB int
}

type memoryTrimState struct {
	last    time.Time
	running bool
	// kind caches the gateway driver's recovery kind; only "snapshot" (E2B) is a VM.
	kind string
}

const memoryTrimTimeout = 3 * time.Minute

// maybeTrimMemory starts at most one background trim per sandbox once its
// model turn has been idle for IdleAfter, then again every Interval.
func (s *SandboxService) maybeTrimMemory(ctx context.Context, row models.Sandbox, now time.Time) {
	p := s.memoryTrim
	if p.IdleAfter <= 0 || now.Sub(s.modelIdleSince(&row)) < p.IdleAfter {
		return
	}
	kind, _ := s.cachedRecoveryKind(&row)
	s.mu.Lock()
	st := s.memoryTrims[row.ID]
	if st == nil {
		st = &memoryTrimState{kind: kind}
		s.memoryTrims[row.ID] = st
	}
	if st.running || !st.last.IsZero() && now.Sub(st.last) < p.Interval || st.kind != "" && st.kind != "snapshot" {
		s.mu.Unlock()
		return
	}
	st.running, st.last = true, now
	s.mu.Unlock()
	go s.trimMemory(ctx, row, st)
}

func (s *SandboxService) trimMemory(ctx context.Context, row models.Sandbox, st *memoryTrimState) {
	defer func() {
		s.mu.Lock()
		st.running = false
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, memoryTrimTimeout)
	defer cancel()
	s.mu.Lock()
	kind := st.kind
	s.mu.Unlock()
	if kind == "" {
		k, err := s.mgr.RecoveryKind(ctx, row.Name)
		if err != nil {
			return
		}
		s.mu.Lock()
		st.kind = k
		s.mu.Unlock()
		if k != "snapshot" {
			return
		}
	}
	sb, err := s.soloHandle(ctx, row.ID)
	if err != nil {
		return
	}
	res, err := sb.TrimMemory(ctx, s.memoryTrim.MinCacheMiB)
	l := log.Info()
	switch {
	case err != nil:
		l = log.Warn().Err(err)
	case res.Skipped != "":
		l = log.Debug()
	}
	l.Uint("id", row.ID).Str("name", row.Name).Str("skipped", res.Skipped).
		Int64("cachedMiB", res.CachedKiB/1024).Int64("freedMiB", res.FreedKiB/1024).Msg("sandbox memory trim")
}
