package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type soloRecoveryGrant struct {
	session     models.SoloSession
	agent       Agent
	fingerprint string
	desired     string
}

func (s *SandboxService) soloRecoveryAuthorization(ctx context.Context, row *models.Sandbox) (soloRecoveryGrant, error) {
	var grant soloRecoveryGrant
	if row == nil || row.Purpose != SandboxPurposeSolo || row.ThreadID == "" || row.Profile == "" || s.skills == nil {
		return grant, fmt.Errorf("环境缺少可核对的当前授权来源，已保留聊天与工作区")
	}
	if err := s.db.WithContext(ctx).Select("id", "sandbox_id", "agent_name", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action", "initial_configuration_id", "applied_configuration_id", "current_configuration_id").
		First(&grant.session, "id = ? AND sandbox_id = ?", row.ThreadID, row.ID).Error; err != nil {
		return grant, fmt.Errorf("环境与当前 Solo 会话不匹配")
	}
	if grant.session.Status == models.SoloStatusClosed || grant.session.LifecycleAction != "" || grant.session.AgentName != row.Profile {
		return grant, fmt.Errorf("会话生命周期已改变，当前环境不能恢复")
	}
	profile, ok := s.skills.Get(row.Profile)
	if !ok {
		return grant, fmt.Errorf("当前 Agent 已不可用，环境保持保留")
	}
	agent := s.effectiveAgent(profile)
	applied := grant.session.AppliedConfigurationID
	if applied == "" {
		applied = grant.session.InitialConfigurationID
	}
	var err error
	grant.agent, err = selectSoloConfiguration(s.db, agent, applied)
	if err != nil {
		return grant, fmt.Errorf("环境原先应用的模型授权已不可用")
	}
	grant.fingerprint, err = s.currentAuthorizationFingerprint(ctx, grant.agent)
	if err != nil {
		return grant, err
	}
	if row.AuthorizationFingerprint == "" || row.AuthorizationFingerprint != grant.fingerprint {
		return grant, fmt.Errorf("环境的已应用授权缺少证明或已变化，恢复已阻止，文件和聊天已保留")
	}
	// Selection can move ahead of the applied route while a turn is queued.
	// Validate both; a valid desired route cannot authorize resuming revoked RAM.
	desired := grant.session.CurrentConfigurationID
	if desired == "" {
		desired = grant.session.InitialConfigurationID
	}
	if desired == applied {
		grant.desired = grant.fingerprint
	} else {
		next, resolveErr := selectSoloConfiguration(s.db, agent, desired)
		if resolveErr != nil {
			return grant, fmt.Errorf("会话当前选择的模型授权已不可用")
		}
		grant.desired, err = s.currentAuthorizationFingerprint(ctx, next)
		if err != nil {
			return grant, err
		}
	}
	return grant, nil
}

func (s *SandboxService) recheckSoloRecoveryGrant(ctx context.Context, row *models.Sandbox, before soloRecoveryGrant) error {
	after, err := s.soloRecoveryAuthorization(ctx, row)
	if err != nil {
		return err
	}
	if after.session.LifecycleEpoch != before.session.LifecycleEpoch || after.session.LifecycleAttemptID != before.session.LifecycleAttemptID ||
		after.fingerprint != before.fingerprint || after.desired != before.desired {
		return fmt.Errorf("恢复期间会话或授权已变化，环境仍保留")
	}
	return nil
}

func requireSoloRecoveryParent(tx *gorm.DB, row *models.Sandbox, grant soloRecoveryGrant) error {
	var session models.SoloSession
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
		Where("id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = '' AND status <> ?",
			row.ThreadID, row.ID, grant.session.LifecycleEpoch, grant.session.LifecycleAttemptID, models.SoloStatusClosed).First(&session).Error
	if err != nil {
		return soloLifecycleChanged()
	}
	return nil
}

func (s *SandboxService) blockSoloRecoveryAuthorization(ctx context.Context, row *models.Sandbox, grant soloRecoveryGrant, reason error) error {
	reason = soloErr(409, "environment_authorization_blocked", "当前授权未通过核验，恢复已阻止，聊天、工作区和环境关联已保留：%s", reason.Error())
	if s.host != nil && row.RunID != "" {
		s.host.UnregisterRun(row.RunID)
	}
	// Only the exact bound lifecycle may receive this failure. Losing the parent
	// fence cannot authorize stopping an environment now owned by a successor.
	if grant.session.ID == "" {
		return reason
	}
	err := s.commitSandboxOperationPrepared(ctx, row.ID, func(tx *gorm.DB) error {
		return requireSoloRecoveryParent(tx, row, grant)
	}, func(tx *gorm.DB) error {
		if err := s.updateSandboxOperation(ctx, tx, row.ID, map[string]any{
			"desired_state": "stopped", "status": "error", "destroy_at": nil,
			"error": "当前授权未通过核验，环境保持保留：" + reason.Error(),
		}); err != nil {
			return err
		}
		return tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ?",
			row.ThreadID, row.ID, grant.session.LifecycleEpoch, grant.session.LifecycleAttemptID).
			Updates(map[string]any{"status": models.SoloStatusError, "error": reason.Error()}).Error
	})
	if err != nil {
		return errors.Join(reason, err)
	}
	s.mu.Lock()
	live := s.live[row.ID]
	local := live != nil && live.sb != nil && live.sb.IsLocal()
	if local {
		// Destroy/teardown releases a Solo local launch and deletes its root.
		// Revoke the connection without treating denied authorization as loss
		// consent. Keep its handle and quota until safe stop is implemented.
		acp := live.acp
		live.acp = nil
		s.mu.Unlock()
		if acp != nil {
			acp.Close()
		}
		return reason
	}
	s.mu.Unlock()
	s.teardownLive(row.ID)
	// This is stop-only: no missing-resource path may create a replacement.
	// A paused E2B stays paused; no Resume/Connect/Attach is invoked on denial.
	if s.mgr != nil && s.mgr.Gateway() != nil {
		status, probeErr := s.mgr.ProbeStatus(ctx, row.Name)
		if probeErr == nil && status == "running" {
			if stopErr := s.mgr.Gateway().Stop(ctx, row.Name); stopErr != nil {
				return errors.Join(reason, stopErr)
			}
			status, probeErr = s.mgr.ProbeStatus(ctx, row.Name)
		}
		if probeErr == nil && (status == "stopped" || status == "exited") {
			err := s.commitSandboxOperationPrepared(ctx, row.ID, func(tx *gorm.DB) error {
				return requireSoloRecoveryParent(tx, row, grant)
			}, func(tx *gorm.DB) error {
				if err := s.updateSandboxOperation(ctx, tx, row.ID, map[string]any{"status": "stopped"}); err != nil {
					return err
				}
				return tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ?",
					row.ThreadID, row.ID, grant.session.LifecycleEpoch, grant.session.LifecycleAttemptID).
					Updates(map[string]any{"status": models.SoloStatusStopped, "error": reason.Error()}).Error
			})
			return errors.Join(reason, err)
		}
	}
	return reason
}

// Startup observes a running provider without starting a stopped one. It still
// requires the same operation lease and authorization as an explicit resume.
func (s *SandboxService) attachAuthorizedSolo(ctx context.Context, id uint) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx, release, err := s.acquireSandboxOperation(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	row, err := s.Get(id)
	if err != nil || row.DesiredState == "stopped" {
		return fmt.Errorf("环境状态已改变，保持停止")
	}
	grant, err := s.soloRecoveryAuthorization(ctx, row)
	if err != nil {
		return s.blockSoloRecoveryAuthorization(ctx, row, grant, err)
	}
	sb, err := s.mgr.AttachNoEnsure(ctx, row.Name)
	if err != nil {
		return err
	}
	return s.recordSoloRecovered(ctx, row, sb, grant)
}

// CurrentSoloMCPAuthorization validates a resident grant on every authorized
// MCP request. A cached token is not an enduring authority after revocation.
func (s *SandboxService) CurrentSoloMCPAuthorization(runID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var row models.Sandbox
	if err := s.db.WithContext(ctx).Where("run_id = ? AND purpose = ? AND status = ?", runID, SandboxPurposeSolo, "running").First(&row).Error; err != nil || row.DesiredState == "stopped" {
		return false
	}
	grant, err := s.soloRecoveryAuthorization(ctx, &row)
	if err != nil {
		return false
	}
	if s.host != nil {
		s.host.SetActiveNode(runID, "solo", grant.agent.Capabilities.Clone())
	}
	return true
}
