package services

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// ErrSoloSandboxGone means the stopped sandbox resource no longer exists, so
// its workspace must come from the backup archive.
var ErrSoloSandboxGone = errors.New("solo sandbox no longer exists")

// soloHandle returns an SSH handle to a running Solo sandbox.
func (s *SandboxService) soloHandle(ctx context.Context, id uint) (*sandbox.Sandbox, error) {
	s.mu.Lock()
	live := s.live[id]
	s.mu.Unlock()
	if live != nil && live.sb != nil {
		return live.sb, nil
	}
	row, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	return s.mgr.AttachNoEnsure(ctx, row.Name)
}

// SoloSandboxIsLocal reports a worktree-mode Solo sandbox, which has no
// gateway resource to stop.
func (s *SandboxService) SoloSandboxIsLocal(id uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := s.live[id]
	return live != nil && live.sb != nil && live.sb.IsLocal()
}

// ResumeSolo restores the common sandbox recovery point, then reconnects the
// Solo resident. Process memory survives only with a snapshot provider.
func (s *SandboxService) ResumeSolo(ctx context.Context, id uint) (bool, error) {
	sb, warm, err := s.EnsureRunning(ctx, id)
	if err != nil {
		return false, err
	}
	row, err := s.Get(id)
	if err != nil {
		return false, err
	}
	if !warm {
		s.db.Model(&models.SoloSession{}).Where("sandbox_id = ?", id).Update("resume_context_pending", true)
	}
	return warm, s.recordSoloRecovered(row, sb)
}

// soloSessionStopped keeps background recovery from starting a sandbox the
// user stopped.
func (s *SandboxService) soloSessionStopped(row *models.Sandbox) bool {
	var session models.SoloSession
	return row.ThreadID != "" && s.db.First(&session, "id = ?", row.ThreadID).Error == nil &&
		(session.Status == models.SoloStatusStopped || session.Status == models.SoloStatusStopping)
}
