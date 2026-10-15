package services

import (
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
)

// A retired run sandbox lives as long as any of its leases (see
// models.SandboxLease). DestroyAt is recomputed from the leases whenever one
// changes and again by the sweeper before it destroys, so a lease that started
// holding after the cached deadline was written (activity, run still going)
// is never missed.

// activityTouchEvery throttles activity-lease writes per sandbox.
const activityTouchEvery = time.Minute

// ErrSandboxNotRetained rejects a manual extension on a sandbox that is not
// lease-managed (interactive/agent sandboxes, or a run sandbox still in use).
var ErrSandboxNotRetained = errors.New("sandbox is not in retention")

// runPhase is what lease evaluation needs to know about the owning run.
type runPhase struct {
	active  bool
	endedAt time.Time
}

// SetMaxRetention updates the lease ceiling at runtime (settings page).
func (s *SandboxService) SetMaxRetention(d time.Duration) {
	if d > 0 {
		s.maxRetention.Store(int64(d))
	}
}

// MaxRetention returns the live lease ceiling.
func (s *SandboxService) MaxRetention() time.Duration { return time.Duration(s.maxRetention.Load()) }

// computeDeadline returns when a sandbox holding leases may be destroyed: the
// latest lease end, capped at maxRet after the retention anchor (the debug
// lease, else the oldest lease). Until-run-end leases keep sliding while the
// run is active and then run for runTTL after the run ends. Nil when there
// are no leases.
func computeDeadline(leases []models.SandboxLease, now time.Time, runTTL, maxRet time.Duration, run runPhase) *time.Time {
	if len(leases) == 0 {
		return nil
	}
	var latest, anchor time.Time
	for _, l := range leases {
		end := now
		switch {
		case l.Until != nil:
			end = *l.Until
		case run.active:
			end = now.Add(runTTL)
		case !run.endedAt.IsZero():
			end = run.endedAt.Add(runTTL)
		}
		if end.After(latest) {
			latest = end
		}
		if l.Kind == models.LeaseDebug {
			anchor = l.CreatedAt
		}
	}
	if anchor.IsZero() {
		for _, l := range leases {
			if anchor.IsZero() || l.CreatedAt.Before(anchor) {
				anchor = l.CreatedAt
			}
		}
	}
	if maxRet > 0 && !anchor.IsZero() {
		if ceil := anchor.Add(maxRet); latest.After(ceil) {
			latest = ceil
		}
	}
	return &latest
}

// upsertLease keeps one lease per kind: the incoming lease replaces an
// existing one when it lasts at least as long (nil Until outlasts any time),
// and the first lease's CreatedAt is kept so the retention anchor can't be
// pushed forward by renewals.
func upsertLease(leases []models.SandboxLease, l models.SandboxLease) []models.SandboxLease {
	out := make([]models.SandboxLease, 0, len(leases)+1)
	replaced := false
	for _, cur := range leases {
		if cur.Kind != l.Kind {
			out = append(out, cur)
			continue
		}
		replaced = true
		if leaseOutlasts(cur, l) {
			out = append(out, cur)
			continue
		}
		l.CreatedAt = cur.CreatedAt
		out = append(out, l)
	}
	if !replaced {
		out = append(out, l)
	}
	return out
}

func leaseOutlasts(a, b models.SandboxLease) bool {
	if a.Until == nil {
		return b.Until != nil
	}
	return b.Until != nil && a.Until.After(*b.Until)
}

func (s *SandboxService) runPhaseOf(runID string) runPhase {
	if strings.TrimSpace(runID) == "" {
		return runPhase{}
	}
	var run models.Run
	if err := s.db.Select("status", "started_at", "duration_sec").Where("id = ?", runID).First(&run).Error; err != nil {
		return runPhase{}
	}
	switch run.Status {
	case "queued", "running", "waiting_human":
		return runPhase{active: true}
	}
	return runPhase{endedAt: run.StartedAt.Add(time.Duration(run.DurationSec) * time.Second)}
}

func (s *SandboxService) inUse(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runActive[name]
}

// applyLease adds l to row and persists the leases. The cached DestroyAt is
// only (re)written once the sandbox is out of use; while a node still drives
// it the lease just waits for retirement.
func (s *SandboxService) applyLease(row *models.Sandbox, l models.SandboxLease) error {
	now := time.Now()
	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	row.Leases = upsertLease(row.Leases, l)
	cols := []string{"leases"}
	if !s.inUse(row.Name) {
		row.DestroyAt = computeDeadline(row.Leases, now, s.RunTTL(), s.MaxRetention(), s.runPhaseOf(row.RunID))
		cols = append(cols, "destroy_at")
	}
	// Struct (not map) updates so the leases JSON serializer applies.
	return s.db.Model(&models.Sandbox{ID: row.ID}).Select(cols).
		Updates(&models.Sandbox{Leases: row.Leases, DestroyAt: row.DestroyAt}).Error
}

// AddLeaseByName attaches a lease to the run sandbox named name. Used by
// set_preview while the node is still running.
func (s *SandboxService) AddLeaseByName(name string, l models.SandboxLease) error {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	var row models.Sandbox
	if err := s.db.Where("name = ?", name).First(&row).Error; err != nil {
		return err
	}
	if row.Purpose != "run" {
		return ErrSandboxNotRetained
	}
	return s.applyLease(&row, l)
}

// ExtendLease is the operator extension from the sandbox UI: either a fixed
// number of minutes from now or until the run ends (minutes <= 0).
func (s *SandboxService) ExtendLease(id uint, minutes int, untilRunEnd bool, by string) (*models.Sandbox, error) {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	row, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if row.Purpose != "run" || len(row.Leases) == 0 {
		return nil, ErrSandboxNotRetained
	}
	l := models.SandboxLease{Kind: models.LeaseManual, By: by, Reason: "manual extension"}
	if !untilRunEnd {
		until := time.Now().Add(time.Duration(minutes) * time.Minute)
		l.Until = &until
	}
	// A manual lease always replaces the previous one, so an operator can
	// also shorten their own earlier extension.
	kept := row.Leases[:0:0]
	for _, cur := range row.Leases {
		if cur.Kind == models.LeaseManual {
			l.CreatedAt = cur.CreatedAt
			continue
		}
		kept = append(kept, cur)
	}
	row.Leases = kept
	if err := s.applyLease(row, l); err != nil {
		return nil, err
	}
	log.Info().Str("name", row.Name).Int("minutes", minutes).Bool("until_run_end", untilRunEnd).
		Str("by", by).Msg("run sandbox lease extended")
	return row, nil
}

// TouchActivity records that a human is using a retained run sandbox
// (console view / terminal / chat / VNC / preview), sliding its activity lease
// to now+RunTTL. Throttled; a no-op for sandboxes that aren't lease-managed.
func (s *SandboxService) TouchActivity(id uint) {
	if s == nil || id == 0 {
		return
	}
	now := time.Now()
	s.mu.Lock()
	if last, ok := s.lastTouch[id]; ok && now.Sub(last) < activityTouchEvery {
		s.mu.Unlock()
		return
	}
	s.lastTouch[id] = now
	s.mu.Unlock()
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	var row models.Sandbox
	if err := s.db.Where("id = ?", id).First(&row).Error; err != nil {
		return
	}
	if row.Purpose != "run" || len(row.Leases) == 0 {
		return
	}
	until := now.Add(s.RunTTL())
	if err := s.applyLease(&row, models.SandboxLease{Kind: models.LeaseActivity, Until: &until, Reason: "console activity"}); err != nil {
		log.Warn().Err(err).Str("name", row.Name).Msg("touch sandbox activity failed")
	}
}

// TouchActivityByName is TouchActivity for callers that only know the
// container name (preview proxy).
func (s *SandboxService) TouchActivityByName(name string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	var row models.Sandbox
	if err := s.db.Select("id").Where("name = ?", name).First(&row).Error; err != nil {
		return
	}
	s.TouchActivity(row.ID)
}

// claimExpired decides, under leaseMu and from the row's current leases (not
// the sweeper's snapshot), whether a due run sandbox may be destroyed. A lease
// that still holds refreshes DestroyAt and keeps the row. An expired row has
// its leases cleared before returning true, so an extension or touch racing
// the destroy is rejected (ErrSandboxNotRetained / no-op) instead of being
// silently lost.
func (s *SandboxService) claimExpired(id uint, now time.Time) bool {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	var row models.Sandbox
	if err := s.db.Where("id = ?", id).First(&row).Error; err != nil {
		return false
	}
	if s.inUse(row.Name) {
		return false
	}
	if len(row.Leases) == 0 {
		return row.DestroyAt != nil && !row.DestroyAt.After(now)
	}
	dl := computeDeadline(row.Leases, now, s.RunTTL(), s.MaxRetention(), s.runPhaseOf(row.RunID))
	if dl != nil && dl.After(now) {
		_ = s.db.Model(&models.Sandbox{}).Where("id = ?", id).Update("destroy_at", dl).Error
		return false
	}
	return s.db.Model(&models.Sandbox{ID: id}).Select("leases").
		Updates(&models.Sandbox{Leases: []models.SandboxLease{}}).Error == nil
}

func (s *SandboxService) forgetTouch(id uint) {
	s.mu.Lock()
	delete(s.lastTouch, id)
	s.mu.Unlock()
}
