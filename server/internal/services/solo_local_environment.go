package services

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// resumeLocalSolo restarts only the proved stopped original handle/root. A cold
// server that lost this private process receipt never adopts a PID by path.
func (s *SandboxService) resumeLocalSolo(ctx context.Context, row *models.Sandbox, live *liveSandbox, grant soloRecoveryGrant) error {
	requested, stopped := live.sb.LocalStopState()
	if !requested {
		return nil
	}
	if !stopped || live.prepared == nil {
		return sandbox.ErrLocalStopUnconfirmed
	}
	if err := s.recheckSoloRecoveryGrant(ctx, row, grant); err != nil {
		return err
	}
	if err := s.commitSandboxOperationPrepared(ctx, row.ID, func(tx *gorm.DB) error { return requireSoloRecoveryParent(tx, row, grant) }, func(tx *gorm.DB) error {
		return tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ?", row.ThreadID, row.ID).Update("resume_context_pending", true).Error
	}); err != nil {
		return err
	}
	if err := s.mgr.Reprime(ctx, live.sb, live.prepared.spec, nil); err != nil {
		return err
	}
	failed := func(cause error) error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		// Keep the same local handle if readiness or publication is uncertain.
		if err := s.commitSandboxOperation(cleanup, row.ID, func(*gorm.DB) error { return nil }); err == nil {
			cause = errors.Join(cause, live.sb.StopLocal(cleanup))
		}
		return cause
	}
	ops := liveSoloRouteOps{manager: s.mgr}
	if err := ops.Ready(ctx, live.sb); err != nil {
		return failed(err)
	}
	acp, err := ops.Connect(ctx, live.sb, live.prepared.specs)
	if err != nil {
		return failed(err)
	}
	s.mu.Lock()
	previous := live.acp
	live.acp = acp
	s.mu.Unlock()
	if err := s.recordSoloRecovered(ctx, row, live.sb, grant); err != nil {
		acp.Close()
		s.mu.Lock()
		live.acp = previous
		s.mu.Unlock()
		return failed(err)
	}
	if previous != nil {
		previous.Close()
	}
	return nil
}
