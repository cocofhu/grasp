package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

const soloRecoveryTimeout = 2 * time.Minute

// reconcileSoloSandbox only observes on boot. Cold resumes run after HTTP is
// serving bootstrap/MCP, before the persisted message queues are restored.
func (s *SandboxService) reconcileSoloSandbox(ctx context.Context, row *models.Sandbox, tracked, correlated map[string]bool) {
	if s.soloSessionClosed(row) {
		s.destroyLocalSandbox(ctx, row)
		return
	}
	tracked[row.Name], correlated[row.Name] = true, true
	s.db.Model(row).Update("destroy_at", nil)
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	if sandbox.GatewayRecordMissing(err) {
		// A process can exit after Create but before adopting the gateway id.
		// Find the correlated resident instead of treating it as an orphan.
		managed, listErr := s.mgr.ListManaged(ctx)
		if listErr == nil {
			for _, candidate := range managed {
				if candidate.Labels[sandbox.CorrelationNameKey()] == row.Name {
					if updateErr := s.db.Model(row).Update("name", candidate.ID).Error; updateErr == nil {
						row.Name = candidate.ID
						tracked[row.Name] = true
						status, err = s.mgr.ProbeStatus(ctx, row.Name)
					}
					break
				}
			}
		}
	}
	if err == nil && status == "not_found" {
		// This is a successful live-driver observation, not an HTTP 404 or a
		// failed probe. The data-bearing resource is already absent.
		s.destroyLocalSandbox(ctx, row)
		delete(tracked, row.Name)
		delete(correlated, row.Name)
		return
	}
	if err == nil && status == "running" {
		var sb *sandbox.Sandbox
		sb, err = s.mgr.AttachNoEnsure(ctx, row.Name)
		if err == nil {
			err = s.recordSoloRecovered(row, sb)
			if err == nil {
				return
			}
		}
	}
	if err == nil {
		err = fmt.Errorf("sandbox status=%s; waiting for recovery", status)
	}
	s.recordSoloRecoveryFailure(row.ID, err)
}

func (s *SandboxService) soloSessionClosed(row *models.Sandbox) bool {
	var session models.SoloSession
	return row.ThreadID != "" && s.db.First(&session, "id = ?", row.ThreadID).Error == nil &&
		session.Status == models.SoloStatusClosed
}

func (s *SandboxService) recordSoloRecovered(row *models.Sandbox, sb *sandbox.Sandbox) error {
	if err := s.db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{
		"status": "running", "error": "", "destroy_at": nil,
		"host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort,
	}).Error; err != nil {
		return err
	}
	if s.host != nil && row.RunID != "" {
		s.host.RestoreRun(row.RunID, row.Token)
	}
	s.mu.Lock()
	fn := s.soloOnRecovered
	s.mu.Unlock()
	if fn != nil {
		fn(row.ID)
	}
	return nil
}

func (s *SandboxService) recordSoloRecoveryFailure(id uint, err error) {
	s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"status": "error", "error": "沙箱恢复暂未完成，工作区已保留，可重试：" + truncErr(err), "destroy_at": nil,
	})
	log.Warn().Err(err).Uint("sandbox", id).Msg("solo recovery deferred; workspace retained")
}

// EnsureSoloRunning recovers the same sandbox, or leaves its data and binding
// intact on failure. It is also called before a new Solo turn, so a later
// dockerd restart does not require restarting Grasp or recreating the session.
func (s *SandboxService) EnsureSoloRunning(ctx context.Context, id uint) error {
	ctx, cancel := context.WithTimeout(ctx, soloRecoveryTimeout)
	defer cancel()
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
	if s.soloSessionClosed(row) {
		return fmt.Errorf("Solo 会话已关闭")
	}
	s.mu.Lock()
	live := s.live[id]
	local := live != nil && live.sb != nil && live.sb.IsLocal()
	s.mu.Unlock()
	if local {
		return nil // worktree processes have no gateway resource to resume
	}
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	var sb *sandbox.Sandbox
	if err == nil {
		switch status {
		case "running":
			sb, err = s.mgr.AttachNoEnsure(ctx, row.Name)
		case "exited", "stopped":
			// Mark before starting: readiness can time out even though the
			// container later starts successfully. Its next turn still needs
			// persisted dialogue context in that case.
			s.teardownLive(id)
			s.db.Model(&models.SoloSession{}).Where("sandbox_id = ?", id).Update("resume_context_pending", true)
			sb, err = s.mgr.Resume(ctx, row.Name)
		case "pending", "creating", "pulling":
			sb, err = s.mgr.WaitForRunning(ctx, row.Name)
		case "not_found":
			s.destroyLocalSandbox(ctx, row)
			return fmt.Errorf("沙箱已不存在，可重新启动会话")
		default:
			err = fmt.Errorf("sandbox status=%s", status)
		}
	}
	if err != nil {
		s.recordSoloRecoveryFailure(id, err)
		return fmt.Errorf("沙箱恢复失败，工作区已保留，请重试：%w", err)
	}
	if s.soloSessionClosed(row) {
		return fmt.Errorf("Solo 会话已关闭")
	}
	return s.recordSoloRecovered(row, sb)
}

// RecoverSoloSandboxes retries only the residents not yet ready. Run once
// before restoring queues, then periodically; failures never turn into TTLs.
func (s *SandboxService) RecoverSoloSandboxes(ctx context.Context) {
	var rows []models.Sandbox
	if err := s.db.Where("purpose = ? AND status <> ?", SandboxPurposeSolo, "running").Find(&rows).Error; err != nil {
		return
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		wg.Go(func() { _ = s.EnsureSoloRunning(ctx, row.ID) })
	}
}

func (s *SandboxService) lockSoloRecovery(ctx context.Context, id uint) (func(), error) {
	value, _ := s.soloRecoveryLocks.LoadOrStore(id, make(chan struct{}, 1))
	lock := value.(chan struct{})
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *SandboxService) RunSoloRecovery(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RecoverSoloSandboxes(ctx)
		}
	}
}
