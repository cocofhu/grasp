package services

import (
	"context"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TouchActivity records user access for display. It does not extend the idle
// interval: only model turns do (MarkModelActivity).
func (s *SandboxService) TouchActivity(id uint) {
	if s == nil || id == 0 {
		return
	}
	_ = s.db.Model(&models.Sandbox{}).Where("id = ? AND status NOT IN ?", id, []string{"saving", "stopped", "deleting"}).
		Update("last_active_at", time.Now()).Error
}

func (s *SandboxService) TouchActivityByName(name string) {
	if id := s.sandboxIDByName(name); id != 0 {
		s.TouchActivity(id)
	}
}

// MarkModelActivity restarts the idle interval: a model turn is running, has
// just ended, or the environment just became ready.
func (s *SandboxService) MarkModelActivity(id uint) {
	if s == nil || id == 0 {
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.modelActive[id] = now
	s.mu.Unlock()
	_ = s.db.Model(&models.Sandbox{}).Where("id = ? AND status <> ?", id, "deleting").
		Updates(map[string]any{"model_active_at": now, "last_active_at": now}).Error
}

func (s *SandboxService) MarkModelActivityByName(name string) {
	if id := s.sandboxIDByName(name); id != 0 {
		s.MarkModelActivity(id)
	}
}

func (s *SandboxService) sandboxIDByName(name string) uint {
	if s == nil || strings.TrimSpace(name) == "" {
		return 0
	}
	var row models.Sandbox
	if s.db.Select("id").Where("name = ?", name).First(&row).Error != nil {
		return 0
	}
	return row.ID
}

// modelIdleSince is when the current idle interval began. Rows written before
// ModelActiveAt existed fall back to their last access.
func (s *SandboxService) modelIdleSince(row *models.Sandbox) time.Time {
	since := row.CreatedAt
	switch {
	case row.ModelActiveAt != nil:
		since = *row.ModelActiveAt
	case row.LastActiveAt != nil:
		since = *row.LastActiveAt
	}
	s.mu.Lock()
	if t := s.modelActive[row.ID]; t.After(since) {
		since = t
	}
	if t := s.externalEnded[row.ID]; t.After(since) {
		since = t
	}
	s.mu.Unlock()
	return since
}

func (s *SandboxService) forgetModelActivity(id uint) {
	s.mu.Lock()
	delete(s.modelActive, id)
	delete(s.externalEnded, id)
	delete(s.memoryTrims, id)
	delete(s.recoveryKinds, id)
	s.mu.Unlock()
}

// idleReclaimExempt reports Docker/Kubernetes environments. Releasing them
// removes the container and stops every process, so idle time never does;
// only manual Stop, Cancel or Close release them. An unresolved kind keeps
// the environment until the gateway answers.
func (s *SandboxService) idleReclaimExempt(ctx context.Context, row *models.Sandbox) bool {
	kind, ok := s.cachedRecoveryKind(row)
	if !ok {
		k, err := s.mgr.RecoveryKind(ctx, row.Name)
		if err != nil {
			return true
		}
		kind = k
		s.mu.Lock()
		if s.recoveryKinds == nil {
			s.recoveryKinds = map[uint]string{}
		}
		s.recoveryKinds[row.ID] = kind
		s.mu.Unlock()
	}
	return kind != "snapshot"
}

// knownIdleReclaimExempt is idleReclaimExempt without a gateway call; an
// unresolved kind reads as reclaimable.
func (s *SandboxService) knownIdleReclaimExempt(row *models.Sandbox) bool {
	kind, ok := s.cachedRecoveryKind(row)
	return ok && kind != "snapshot"
}

// RunsIdleReclaimExempt reports runs whose every sandbox is known to be
// exempt from idle reclaim, so keep-alive has nothing to change.
func (s *SandboxService) RunsIdleReclaimExempt(runIDs []string) map[string]bool {
	out := map[string]bool{}
	if s == nil || len(runIDs) == 0 {
		return out
	}
	var rows []models.Sandbox
	if s.db.Where("run_id IN ? AND purpose = ?", runIDs, "run").Find(&rows).Error != nil {
		return out
	}
	reclaimable := map[string]bool{}
	for i := range rows {
		if s.knownIdleReclaimExempt(&rows[i]) {
			out[rows[i].RunID] = true
		} else {
			reclaimable[rows[i].RunID] = true
		}
	}
	for id := range reclaimable {
		delete(out, id)
	}
	return out
}

func (s *SandboxService) cachedRecoveryKind(row *models.Sandbox) (string, bool) {
	if row.RecoveryKind != "" {
		return row.RecoveryKind, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kind, ok := s.recoveryKinds[row.ID]
	return kind, ok
}

// keepAlive reports whether the owning Solo session or run disabled automatic
// idle reclaim. Lookup failures keep the environment.
func (s *SandboxService) keepAlive(row *models.Sandbox) bool {
	var n int64
	var err error
	switch {
	case row.Purpose == SandboxPurposeSolo:
		err = s.db.Model(&models.SoloSession{}).Where("sandbox_id = ? AND keep_alive = ?", row.ID, true).Count(&n).Error
	case row.Purpose == "run" && row.RunID != "":
		err = s.db.Model(&models.Run{}).Where("id = ? AND keep_alive = ?", row.RunID, true).Count(&n).Error
	default:
		return false
	}
	return err != nil || n > 0
}
