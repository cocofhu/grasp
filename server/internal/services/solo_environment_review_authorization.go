package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// OfflineSoloEnvironmentAuthorization validates current grants for an explicit
// offline stop-only review. It does not adopt an environment, stamp a missing
// historical grant, or authorize resuming a process with unknown credentials.
func (s *SandboxService) OfflineSoloEnvironmentAuthorization(ctx context.Context, id uint) (string, error) {
	if s == nil || s.db == nil || s.skills == nil {
		return "", fmt.Errorf("current Solo authorization is unavailable")
	}
	if _, inside := s.db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return "", fmt.Errorf("offline authorization requires a transaction-free handle")
	}
	var sandbox models.Sandbox
	if err := s.db.WithContext(ctx).Select("id", "name", "profile", "purpose", "thread_id").First(&sandbox, id).Error; err != nil {
		return "", err
	}
	var solo models.SoloSession
	if err := s.db.WithContext(ctx).Select("id", "user_id", "sandbox_id", "agent_name", "status", "lifecycle_action", "lifecycle_epoch", "lifecycle_attempt_id", "initial_configuration_id", "current_configuration_id", "applied_configuration_id").First(&solo, "id = ? AND sandbox_id = ?", sandbox.ThreadID, id).Error; err != nil {
		return "", err
	}
	if sandbox.Purpose != SandboxPurposeSolo || sandbox.Name == "" || solo.UserID == "" || solo.AgentName != sandbox.Profile || solo.LifecycleAction != "" || solo.Status == models.SoloStatusClosed {
		return "", fmt.Errorf("original Solo binding changed")
	}
	profile, ok := s.skills.Get(sandbox.Profile)
	if !ok {
		return "", fmt.Errorf("current Agent is unavailable")
	}
	agent := s.effectiveAgent(profile)
	appliedID, desiredID := solo.AppliedConfigurationID, solo.CurrentConfigurationID
	if appliedID == "" {
		appliedID = solo.InitialConfigurationID
	}
	if desiredID == "" {
		desiredID = solo.InitialConfigurationID
	}
	applied, err := selectSoloConfiguration(s.db, agent, appliedID)
	if err != nil {
		return "", err
	}
	appliedGrant, err := s.currentAuthorizationFingerprint(ctx, applied)
	if err != nil {
		return "", err
	}
	desired, err := selectSoloConfiguration(s.db, agent, desiredID)
	if err != nil {
		return "", err
	}
	desiredGrant, err := s.currentAuthorizationFingerprint(ctx, desired)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal([]any{1, sandbox.ID, sandbox.Name, solo.ID, solo.UserID, solo.LifecycleEpoch, solo.LifecycleAttemptID, appliedID, desiredID, appliedGrant, desiredGrant})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}
