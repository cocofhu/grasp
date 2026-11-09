package services

import (
	"context"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
)

// EnsureSoloTurnRunning runs only at the owned FIFO turn boundary. A user's
// valid new model group may replace an unavailable previous route while the
// exact environment is already running. It cannot authorize waking paused RAM
// that still contains the old credentials, or change any unrelated Git grant.
func (s *SandboxService) EnsureSoloTurnRunning(ctx context.Context, expected *models.SoloSession, target Agent) error {
	ctx, release, err := s.acquireSandboxOperation(ctx, expected.SandboxID)
	if err != nil {
		return err
	}
	defer release()
	row, err := s.Get(expected.SandboxID)
	if err != nil {
		return err
	}
	if err := requireSoloRecoveryParent(s.db.WithContext(ctx), row, soloRecoveryGrant{session: *expected}); err != nil {
		return err
	}
	desired, applied := chatConfigurationID(expected), appliedChatConfigurationID(expected)
	if desired != "" && desired != applied && row.Purpose == SandboxPurposeSolo && row.Status == "running" && row.DesiredState != "stopped" {
		if target.ModelBinding == nil || target.ModelBinding.ConfigurationID != desired {
			return fmt.Errorf("会话当前模型组与待切换授权不匹配")
		}
		s.mu.Lock()
		live := s.live[row.ID]
		local := live != nil && live.sb != nil && live.sb.IsLocal()
		s.mu.Unlock()
		running := local
		if !local && s.mgr != nil {
			status, probeErr := s.mgr.ProbeStatus(ctx, row.Name)
			if probeErr != nil {
				return probeErr
			}
			running = status == "running"
		}
		if running {
			if _, err := s.AlignSoloModelRoute(ctx, row.ID, target); err != nil {
				return err
			}
		}
	}
	return s.EnsureSoloRunning(ctx, row.ID)
}
