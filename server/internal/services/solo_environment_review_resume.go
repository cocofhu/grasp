package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This optional protocol must update the exact stopped environment's complete
// trusted configuration before any old backend can run. An ordinary Resume
// or a post-start Reprime does not implement this contract.
type soloReviewedColdResumer interface {
	CanResumeConfigured(context.Context, string) (bool, error)
	ResumeConfigured(context.Context, string, sandbox.Spec, string) (*sandbox.Sandbox, error)
}

func (s *SandboxService) resumeReviewedSoloEnvironment(ctx context.Context, row *models.Sandbox) (bool, error) {
	if row.AuthorizationFingerprint != "" || s.execution == nil || !s.db.Migrator().HasTable(&executionlineage.SoloEnvironmentReview{}) {
		return false, nil
	}
	var receipt executionlineage.SoloEnvironmentReview
	err := s.db.WithContext(ctx).Where("sandbox_id = ? AND solo_id = ? AND dataset_id = ? AND protocol = ? AND finalized_at IS NOT NULL", row.ID, row.ThreadID, s.execution.DatasetID(), executionlineage.SoloEnvironmentReviewProtocol).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if receipt.ProviderID != row.Name || receipt.State != "finalized" || receipt.CompletedAt == nil || receipt.StoppedAt == nil {
		return true, soloErr(409, "environment_review_changed", "原环境核对收据与当前绑定不一致，工作区已保留")
	}
	resumer, ok := any(s.mgr).(soloReviewedColdResumer)
	if !ok || s.mgr == nil {
		return true, soloErr(409, "environment_cold_configuration_unsupported", "当前环境不能在冷启动前安全刷新授权，原工作区与聊天已保留")
	}
	return true, s.resumeReviewedSoloWith(ctx, row, receipt, resumer)
}

func (s *SandboxService) reviewedSoloCurrentGrant(ctx context.Context, row *models.Sandbox) (soloRecoveryGrant, error) {
	var grant soloRecoveryGrant
	if s.skills == nil {
		return grant, fmt.Errorf("当前 Agent 授权不可用")
	}
	if err := s.db.WithContext(ctx).Where("id = ? AND sandbox_id = ?", row.ThreadID, row.ID).First(&grant.session).Error; err != nil {
		return grant, err
	}
	if grant.session.Status != models.SoloStatusStarting || grant.session.LifecycleAction != "" || grant.session.AgentName != row.Profile || row.DesiredState != "running" {
		return grant, soloErr(409, "environment_resume_required", "请明确点击恢复原环境，再继续当前会话")
	}
	profile, ok := s.skills.Get(row.Profile)
	if !ok {
		return grant, fmt.Errorf("当前 Agent 已不可用")
	}
	agent := s.effectiveAgent(profile)
	var err error
	grant.agent, err = selectSoloConfiguration(s.db, agent, chatConfigurationID(&grant.session))
	if err != nil {
		return grant, err
	}
	grant.fingerprint, err = s.currentAuthorizationFingerprint(ctx, grant.agent)
	grant.desired = grant.fingerprint
	return grant, err
}

// The physical operation lease is already held by EnsureSoloRunning. This
// function never runs at startup for a manually stopped original environment.
func (s *SandboxService) resumeReviewedSoloWith(ctx context.Context, row *models.Sandbox, receipt executionlineage.SoloEnvironmentReview, resumer soloReviewedColdResumer) error {
	supported, err := resumer.CanResumeConfigured(ctx, row.Name)
	if err != nil {
		return err
	}
	if !supported {
		return soloErr(409, "environment_cold_configuration_unsupported", "当前环境不能在冷启动前安全刷新授权，原工作区与聊天已保留")
	}
	grant, err := s.reviewedSoloCurrentGrant(ctx, row)
	if err != nil {
		return err
	}
	current := func(tx *gorm.DB) error {
		if err := chatsession.NewDurableStore(s.db).GuardIdle(tx, soloQueueScope(row.ThreadID)); err != nil {
			return err
		}
		if err := requireSoloRecoveryParent(tx, row, grant); err != nil {
			return err
		}
		var selected models.SoloSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ? AND sandbox_id = ? AND status = ? AND current_model_set = ? AND COALESCE(current_configuration_id,'') = ? AND COALESCE(current_model_id,'') = ? AND COALESCE(current_reasoning_effort,'') = ? AND COALESCE(initial_configuration_id,'') = ?", grant.session.ID, row.ID, models.SoloStatusStarting, grant.session.CurrentModelSet, grant.session.CurrentConfigurationID, grant.session.CurrentModelID, grant.session.CurrentReasoningEffort, grant.session.InitialConfigurationID).Take(&selected).Error; err != nil {
			return soloLifecycleChanged()
		}
		return executionlineage.VerifyReviewedSoloEnvironmentBindingTx(tx, receipt)
	}
	if err := s.commitSandboxOperationPrepared(ctx, row.ID, current, func(tx *gorm.DB) error {
		var saved executionlineage.SoloEnvironmentReview
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND sandbox_id = ? AND solo_id = ? AND provider_id = ? AND dataset_id = ? AND finalized_at IS NOT NULL AND state = 'finalized'", receipt.ID, row.ID, row.ThreadID, row.Name, s.execution.DatasetID()).Take(&saved).Error; err != nil {
			return err
		}
		if saved.ColdAuthorizationFingerprint != "" && saved.ColdAuthorizationFingerprint != grant.fingerprint {
			return soloErr(409, "environment_configuration_unknown", "原冷启动配置结果尚待核对，未重发或更换原环境")
		}
		if saved.ColdRequestID != "" && saved.ColdRequestID != receipt.ID+"-cold" {
			return soloLifecycleChanged()
		}
		if saved.ColdCompletedAt != nil {
			return soloLifecycleChanged()
		}
		if saved.ColdDispatchedAt == nil {
			now := time.Now().UTC()
			changed := tx.Model(&executionlineage.SoloEnvironmentReview{}).Where("id = ? AND cold_dispatched_at IS NULL AND finalized_at IS NOT NULL", receipt.ID).Updates(map[string]any{"cold_request_id": receipt.ID + "-cold", "cold_authorization_fingerprint": grant.fingerprint, "cold_state": "dispatching", "cold_dispatched_at": now})
			if changed.Error != nil {
				return changed.Error
			}
			if changed.RowsAffected != 1 {
				return soloLifecycleChanged()
			}
		}
		return nil
	}); err != nil {
		return err
	}
	prepared, err := s.prepareAgentContainer(row.Profile, row.ThreadID, row.RunID, row.Token, row.Name, s.soloPlatformSpecs(row), grant.agent, agentContainerExtra{Resident: true, LaunchKind: "solo", InitialChatModel: chatModelID(&grant.session), InitialChatReasoningEffort: &grant.session.CurrentReasoningEffort})
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(prepared.spec.ConfigHome) }()
	// Cold migration refreshes authorization/configuration, never repositories.
	// The native protocol preserves its existing workspace root and all files.
	prepared.spec.WorkspaceDir = ""
	prepared.spec.Env["GIT_REPOS"] = ""
	latest, err := s.soloStartAuthorization(ctx, row.Profile, grant.agent)
	if err != nil || latest != grant.fingerprint {
		return errors.Join(soloErr(409, "environment_authorization_blocked", "恢复前当前授权已变化，原工作区已保留"), err)
	}
	if err := s.commitSandboxOperationPrepared(ctx, row.ID, current, func(*gorm.DB) error { return nil }); err != nil {
		return err
	}
	sb, err := resumer.ResumeConfigured(ctx, row.Name, prepared.spec, receipt.ID+"-cold")
	if err != nil {
		return fmt.Errorf("原环境冷启动结果待核对，保留工作区与原操作身份：%w", err)
	}
	if sb == nil || sb.ID != row.Name {
		return soloLifecycleChanged()
	}
	latest, err = s.soloStartAuthorization(ctx, row.Profile, grant.agent)
	if err != nil || latest != grant.fingerprint {
		return s.blockSoloRecoveryAuthorization(ctx, row, grant, fmt.Errorf("冷启动期间当前授权已变化"))
	}
	if err := s.mgr.ConfigureWorkspaceRuntime(ctx, sb); err != nil {
		return err
	}
	// Native success is not sufficient: a fresh current Authority, parent,
	// selected route and physical operation fence guard this publication.
	if err := s.commitSandboxOperationPrepared(ctx, row.ID, current, func(tx *gorm.DB) error {
		if err := s.updateSandboxOperation(ctx, tx, row.ID, map[string]any{"status": "running", "desired_state": "running", "authorization_fingerprint": grant.fingerprint, "model_binding_fingerprint": prepared.modelFingerprint, "git_bindings_fingerprint": gitBindingsFingerprint(prepared.spec.GitCredentials), "host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort, "destroy_at": nil, "error": ""}); err != nil {
			return err
		}
		changed := tx.Table("solo_sessions").Where("id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = '' AND status = ?", grant.session.ID, row.ID, grant.session.LifecycleEpoch, grant.session.LifecycleAttemptID, models.SoloStatusStarting).Updates(map[string]any{"status": models.SoloStatusRunning, "error": "", "applied_configuration_id": grant.agent.ModelBinding.ConfigurationID, "applied_model_id": chatModelID(&grant.session), "applied_model_set": true, "applied_reasoning_effort": grant.session.CurrentReasoningEffort, "resume_context_pending": true})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return soloLifecycleChanged()
		}
		now := time.Now().UTC()
		changed = tx.Model(&executionlineage.SoloEnvironmentReview{}).Where("id = ? AND cold_request_id = ? AND cold_authorization_fingerprint = ? AND cold_completed_at IS NULL", receipt.ID, receipt.ID+"-cold", grant.fingerprint).Updates(map[string]any{"cold_state": "completed", "cold_completed_at": now})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return soloLifecycleChanged()
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
