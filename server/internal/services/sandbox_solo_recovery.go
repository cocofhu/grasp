package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const soloRecoveryTimeout = 2 * time.Minute

// reconcileSoloSandbox only observes on boot. Cold resumes run after HTTP is
// serving bootstrap/MCP, before the persisted message queues are restored.
func (s *SandboxService) reconcileSoloSandbox(ctx context.Context, row *models.Sandbox, tracked, correlated map[string]bool) {
	if s.soloSessionClosed(row) {
		// Closed chats can own the sole retained worktree. Observation never
		// upgrades Close into permission to delete a disk or its attribution.
		tracked[row.Name], correlated[row.Name] = true, true
		_ = s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error {
			return tx.Model(&models.Sandbox{}).Where("id = ? AND generation = ?", row.ID, row.Generation).Update("destroy_at", nil).Error
		})
		return
	}
	tracked[row.Name], correlated[row.Name] = true, true
	if row.OperationOwner != "" && row.OperationExpiresAt != nil && row.OperationExpiresAt.After(time.Now()) {
		return // another replica is changing this environment; observation waits
	}
	if s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error { return tx.Model(row).Update("destroy_at", nil).Error }) != nil {
		return
	}
	if row.DesiredState == "stopped" {
		// Observation must not wake manually stopped residents on restart.
		if status, err := s.mgr.ProbeStatus(ctx, row.Name); err == nil && (status == "stopped" || status == "exited") {
			_ = s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error {
				if err := tx.Model(row).Updates(map[string]any{"status": "stopped", "error": ""}).Error; err != nil {
					return err
				}
				return tx.Model(&models.SoloSession{}).Where("sandbox_id = ? AND status <> ?", row.ID, models.SoloStatusClosed).Update("status", models.SoloStatusStopped).Error
			})
		}
		return
	}
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	if sandbox.GatewayRecordMissing(err) {
		// A process can exit after Create but before adopting the gateway id.
		// Find the correlated resident instead of treating it as an orphan.
		managed, listErr := s.mgr.ListManaged(ctx)
		if listErr == nil {
			for _, candidate := range managed {
				if candidate.Labels[sandbox.CorrelationNameKey()] == row.Name {
					if updateErr := s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error { return tx.Model(row).Update("name", candidate.ID).Error }); updateErr == nil {
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
		// A missing workload can still have recoverable disks/configuration.
		// Keep its identity and any local files; never turn a probe into delete.
		_ = s.recordSoloResourceUnavailable(row)
		return
	}
	if err == nil && status == "running" {
		err = s.attachAuthorizedSolo(ctx, row.ID)
		if err == nil {
			return
		}
	}
	if err == nil {
		err = fmt.Errorf("sandbox status=%s; waiting for recovery", status)
	}
	s.recordSoloRecoveryFailure(row, err)
}

func (s *SandboxService) soloSessionClosed(row *models.Sandbox) bool {
	var session models.SoloSession
	return row.ThreadID != "" && s.db.First(&session, "id = ?", row.ThreadID).Error == nil &&
		session.Status == models.SoloStatusClosed
}

func (s *SandboxService) recordSoloRecovered(ctx context.Context, row *models.Sandbox, sb *sandbox.Sandbox, grant soloRecoveryGrant) error {
	// A recreated pod retains the PVC head but loses /run platform env. Rebuild
	// the trusted tuple only through the native standby protocol before making
	// this environment available; an already configured runtime is read-only.
	if err := s.mgr.ConfigureWorkspaceRuntime(ctx, sb); err != nil {
		return err
	}
	if err := s.recheckSoloRecoveryGrant(ctx, row, grant); err != nil {
		return s.blockSoloRecoveryAuthorization(ctx, row, grant, err)
	}
	if err := s.commitSandboxOperationPrepared(ctx, row.ID, func(tx *gorm.DB) error {
		return requireSoloRecoveryParent(tx, row, grant)
	}, func(tx *gorm.DB) error {
		if err := s.updateSandboxOperation(ctx, tx, row.ID, map[string]any{
			"status": "running", "error": "", "destroy_at": nil,
			"host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort,
		}); err != nil {
			return err
		}
		result := tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = '' AND status <> ?",
			row.ThreadID, row.ID, grant.session.LifecycleEpoch, grant.session.LifecycleAttemptID, models.SoloStatusClosed).
			Updates(map[string]any{"status": models.SoloStatusRunning, "error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("会话生命周期已改变，未发布旧恢复状态")
		}
		return nil
	}); err != nil {
		return err
	}
	if s.host != nil && row.RunID != "" {
		s.host.RestoreRun(row.RunID, row.Token)
		s.host.SetActiveNode(row.RunID, "solo", grant.agent.Capabilities.Clone())
	}
	s.mu.Lock()
	fn := s.soloOnRecovered
	s.mu.Unlock()
	if fn != nil {
		fn(row.ID)
	}
	return nil
}

func (s *SandboxService) recordSoloRecoveryFailure(row *models.Sandbox, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error {
		query := tx.Model(&models.Sandbox{}).Where("id = ? AND generation = ? AND (desired_state IS NULL OR desired_state <> ?)", row.ID, row.Generation, "stopped")
		if row.OperationOwner != "" {
			query = query.Where("operation_owner = ? AND operation_expires_at > ?", row.OperationOwner, time.Now().UTC())
		}
		return query.Updates(map[string]any{
			"status": "error", "error": "沙箱恢复暂未完成，未释放现有环境，可重试：" + truncErr(err), "destroy_at": nil,
		}).Error
	})
	log.Warn().Err(err).Uint("sandbox", row.ID).Msg("solo recovery deferred; workspace retained")
}

func (s *SandboxService) recordSoloResourceUnavailable(row *models.Sandbox) error {
	reason := "原环境未找到，聊天和环境关联已保留，等待恢复；未自动创建替代环境"
	recovery, err := json.Marshal(models.SandboxRecovery{History: "retained", Files: "unavailable", NativeSession: "unavailable", Process: "unavailable", Reason: reason})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.executionTransaction(ctx, row.ID, func(tx *gorm.DB) error {
		result := tx.Model(&models.Sandbox{}).Where("id = ? AND generation = ?", row.ID, row.Generation).
			Updates(map[string]any{"status": "error", "error": reason, "recovery": string(recovery), "destroy_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("environment changed during observation")
		}
		return tx.Model(&models.SoloSession{}).Where("sandbox_id = ? AND status <> ?", row.ID, models.SoloStatusClosed).
			Updates(map[string]any{"status": models.SoloStatusError, "error": reason}).Error
	})
}

// EnsureSoloRunning recovers the same sandbox, or leaves its data and binding
// intact on failure. It is also called before a new Solo turn, so a later
// dockerd restart does not require restarting Grasp or recreating the session.
func (s *SandboxService) EnsureSoloRunning(ctx context.Context, id uint) error {
	ctx, cancel := context.WithTimeout(ctx, soloRecoveryTimeout)
	defer cancel()
	ctx, unlock, err := s.acquireSandboxOperation(ctx, id)
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
	if row.DesiredState == "stopped" {
		return soloErr(409, "environment_stopped", "环境已手动停止，请先恢复环境")
	}
	grant, err := s.soloRecoveryAuthorization(ctx, row)
	if err != nil {
		return s.blockSoloRecoveryAuthorization(ctx, row, grant, err)
	}
	s.mu.Lock()
	live := s.live[id]
	local := live != nil && live.sb != nil && live.sb.IsLocal()
	s.mu.Unlock()
	if local {
		return s.resumeLocalSolo(ctx, row, live, grant)
	}
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	var sb *sandbox.Sandbox
	if err == nil {
		if authErr := s.recheckSoloRecoveryGrant(ctx, row, grant); authErr != nil {
			return s.blockSoloRecoveryAuthorization(ctx, row, grant, authErr)
		}
		if err := s.executionCheck(ctx, row.ID); err != nil {
			return err
		}
		switch status {
		case "running":
			sb, err = s.mgr.AttachNoEnsure(ctx, row.Name)
		case "exited", "stopped":
			// Mark before starting: readiness can time out even though the
			// container later starts successfully. Its next turn still needs
			// persisted dialogue context in that case.
			s.teardownLive(id)
			if err := s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
				return tx.Model(&models.SoloSession{}).Where("sandbox_id = ?", id).Update("resume_context_pending", true).Error
			}); err != nil {
				return err
			}
			sb, err = s.mgr.Resume(ctx, row.Name)
		case "pending", "creating", "pulling":
			sb, err = s.mgr.WaitForRunning(ctx, row.Name)
		case "not_found":
			if err := s.recordSoloResourceUnavailable(row); err != nil {
				return err
			}
			return soloErr(409, "environment_unavailable", "原环境未找到，已保留关联，未自动创建替代环境")
		default:
			err = fmt.Errorf("sandbox status=%s", status)
		}
	}
	if err != nil {
		s.recordSoloRecoveryFailure(row, err)
		return fmt.Errorf("沙箱恢复失败，未释放现有环境，请重试：%w", err)
	}
	if s.soloSessionClosed(row) {
		return fmt.Errorf("Solo 会话已关闭")
	}
	return s.recordSoloRecovered(ctx, row, sb, grant)
}

// RecoverSoloSandboxes retries residents whose recovery was deferred. Active
// creation owns creating/pulling rows and has not necessarily adopted a gateway
// id yet. Startup reconciliation marks interrupted creation for recovery.
// Run once before restoring queues, then periodically; failures never become TTLs.
func (s *SandboxService) RecoverSoloSandboxes(ctx context.Context) {
	// Finish an interrupted explicit stop as well as interrupted starts. This
	// branch only stops existing compute; it never wakes a manually stopped guest.
	var pendingStops []models.Sandbox
	if err := s.db.Where("purpose = ? AND desired_state = ? AND status <> ?", SandboxPurposeSolo, "stopped", "stopped").Find(&pendingStops).Error; err == nil {
		for _, row := range pendingStops {
			if ctx.Err() != nil {
				return
			}
			_ = s.retrySoloStop(ctx, row.ID)
		}
	}
	var rows []models.Sandbox
	if err := s.db.Where("purpose = ? AND status NOT IN ?", SandboxPurposeSolo,
		[]string{"running", "creating", "pulling"}).Where("desired_state IS NULL OR desired_state <> ?", "stopped").Find(&rows).Error; err != nil {
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
