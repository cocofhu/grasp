package services

import (
	"context"
	"errors"
	"fmt"
	"io"

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

// SoloSandboxRunning reports whether the provider has the sandbox running, so
// a scheduled backup never wakes a paused one.
func (s *SandboxService) SoloSandboxRunning(ctx context.Context, id uint) bool {
	row, err := s.Get(id)
	if err != nil || s.mgr == nil {
		return false
	}
	return s.mgr.Status(ctx, row.Name) == "running"
}

// ArchiveSoloWorkspace streams the workspace of a running Solo sandbox.
func (s *SandboxService) ArchiveSoloWorkspace(ctx context.Context, id uint) (io.ReadCloser, error) {
	sb, err := s.soloHandle(ctx, id)
	if err != nil {
		return nil, err
	}
	return sb.ArchiveWorkspace(ctx)
}

// StopSolo closes the chat connection and stops the sandbox, retaining its
// resource for ResumeSolo.
func (s *SandboxService) StopSolo(ctx context.Context, id uint) error {
	unlock, err := s.lockSoloRecovery(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	row, err := s.Get(id)
	if err != nil {
		return err
	}
	if row.Purpose != SandboxPurposeSolo {
		return fmt.Errorf("sandbox is not a Solo sandbox")
	}
	if s.SoloSandboxIsLocal(id) {
		return fmt.Errorf("local worktree sandboxes cannot be stopped")
	}
	s.teardownLive(id)
	if err := s.mgr.Stop(ctx, row.Name); err != nil {
		return err
	}
	return s.db.Model(&models.Sandbox{}).Where("id = ?", id).
		Updates(map[string]any{"status": "stopped", "error": "", "destroy_at": nil}).Error
}

// ResumeSolo starts a stopped Solo sandbox. warm reports that process memory
// survived the stop (E2B pause), so the coding CLI keeps its session.
func (s *SandboxService) ResumeSolo(ctx context.Context, id uint) (warm bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, soloRecoveryTimeout)
	defer cancel()
	unlock, err := s.lockSoloRecovery(ctx, id)
	if err != nil {
		return false, err
	}
	defer unlock()
	row, err := s.Get(id)
	if err != nil {
		return false, ErrSoloSandboxGone
	}
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	if sandbox.GatewayRecordMissing(err) {
		return false, ErrSoloSandboxGone
	}
	if err != nil {
		return false, err
	}
	var sb *sandbox.Sandbox
	switch status {
	case "running":
		warm = true
		sb, err = s.mgr.AttachNoEnsure(ctx, row.Name)
	case "exited", "stopped":
		warm = s.mgr.KeepsProcessesOnStop(ctx, row.Name)
		s.teardownLive(id)
		if !warm {
			// recordSoloRecovered publishes running; the first turn after it
			// must already see the pending dialogue context.
			s.db.Model(&models.SoloSession{}).Where("sandbox_id = ?", id).Update("resume_context_pending", true)
		}
		sb, err = s.mgr.Resume(ctx, row.Name)
	case "pending", "creating", "pulling":
		sb, err = s.mgr.WaitForRunning(ctx, row.Name)
	case "not_found":
		return false, ErrSoloSandboxGone
	default:
		err = fmt.Errorf("sandbox status=%s", status)
	}
	if err != nil {
		return false, err
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
