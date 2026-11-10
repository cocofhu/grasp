package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func (s *SoloService) openSandbox(ctx context.Context, row *models.SoloSession) error {
	sid := row.ID
	model, configuration, effort := chatModelID(row), chatConfigurationID(row), row.CurrentReasoningEffort
	epoch, attempt, action := row.LifecycleEpoch, row.LifecycleAttemptID, row.LifecycleAction
	current := func(tx *gorm.DB, id uint) *gorm.DB {
		return tx.Model(&models.SoloSession{}).Where("id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND sandbox_id = ?", sid, epoch, attempt, id)
	}
	sb, err := s.sbx.OpenSoloSandbox(ctx, SoloSandboxOpts{
		Profile: row.AgentName, SessionID: sid, Repos: effectiveSessionRepos(row), InitialModelID: model,
		InitialConfigurationID: configuration,
		ReasoningEffort:        effort,
		Admit: func(tx *gorm.DB) error {
			var count int64
			if err := tx.Model(&models.SoloSession{}).Where("id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = ? AND status = ?", sid, epoch, attempt, action, models.SoloStatusStarting).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return soloLifecycleChanged()
			}
			return nil
		},
		Current: func(tx *gorm.DB, sandboxID uint) error {
			var count int64
			if err := current(tx, sandboxID).Where("status IN ? AND lifecycle_action = ''", []string{models.SoloStatusStarting, models.SoloStatusRunning}).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return soloLifecycleChanged()
			}
			return nil
		},
		Bind: func(tx *gorm.DB, sandbox *models.Sandbox) error {
			result := current(tx, 0).Where("status = ? AND lifecycle_action = ?", models.SoloStatusStarting, action).Updates(map[string]any{"sandbox_id": sandbox.ID, "error": "", "lifecycle_action": ""})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return soloErr(409, "session_changed", "会话状态已变化，请重新加载")
			}
			return nil
		},
		OnEnvForSandbox: func(sandboxID uint, sources map[string]string) {
			current(s.db, sandboxID).Where("status = ? AND lifecycle_action = ''", models.SoloStatusStarting).
				Select("EnvSources").Updates(&models.SoloSession{EnvSources: sources})
		},
		PublishReady: func(tx *gorm.DB, sandboxID uint) error {
			if err := lockSoloCompute(tx); err != nil {
				return err
			}
			if err := s.durable.GuardOpen(tx, soloQueueScope(sid)); err != nil {
				return err
			}
			res := current(tx, sandboxID).Where("status = ? AND lifecycle_action = ''", models.SoloStatusStarting).
				Updates(map[string]any{"status": models.SoloStatusRunning, "error": "",
					"applied_configuration_id": configuration, "applied_model_id": model, "applied_model_set": true,
					"applied_reasoning_effort": effort, "model_notice": ""})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return soloLifecycleChanged()
			}
			return nil
		},
		OnReady: func(uint) { s.publishStatus(sid) },
		OnFail: func(sandboxID uint, err error) {
			res := current(s.db, sandboxID).
				Where("status IN ? AND lifecycle_action = ''",
					[]string{models.SoloStatusStarting, models.SoloStatusRunning}).
				Updates(map[string]any{"status": models.SoloStatusError, "error": truncErr(err)})
			if res.RowsAffected > 0 {
				s.publishStatus(sid)
			}
		},
	})
	if err != nil {
		return err
	}
	row.SandboxID = sb.ID
	return nil
}

func (s *SoloService) onSandboxRecovered(sandboxID uint) {
	var rows []models.SoloSession
	// The recovery service publishes running atomically with its captured
	// parent grant and physical lease. A callback carries only a sandbox ID,
	// so it cannot authorize a state change for a newer epoch on the same ID.
	s.db.Where("sandbox_id = ? AND status = ? AND lifecycle_action = ''", sandboxID, models.SoloStatusRunning).Find(&rows)
	for _, row := range rows {
		s.publishStatus(row.ID)
	}
	s.retryDeferredRestores(sandboxID)
}

func (s *SoloService) onSandboxRecoveryFailed(sandboxID uint) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status = ? AND lifecycle_action = ''", sandboxID, models.SoloStatusError).Find(&rows)
	for _, row := range rows {
		s.publishStatus(row.ID)
	}
}

// onSandboxGone observes a missing bookkeeping row. The legacy callback has
// no provider deletion/incarnation proof, so it retains the unknown binding;
// future explicit release must carry its own loss-aware operation receipt.
func (s *SoloService) onSandboxGone(sandboxID uint) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND lifecycle_action = '' AND status IN ?", sandboxID,
		[]string{models.SoloStatusStarting, models.SoloStatusRunning}).Find(&rows)
	for _, r := range rows {
		var activeID string
		err := s.executionTransaction(context.Background(), r.ID, func(tx *gorm.DB) error {
			if err := lockSoloCompute(tx); err != nil {
				return err
			}
			// An old hook may arrive after the same retained sandbox reopened.
			// Its ID alone proves no destroyed incarnation. Only actual absence
			// of that persistent sandbox row permits repairing a dangling bind.
			var existing int64
			if err := tx.Model(&models.Sandbox{}).Where("id = ?", sandboxID).Count(&existing).Error; err != nil {
				return err
			}
			if existing != 0 {
				return soloLifecycleChanged()
			}
			if err := s.durable.Retire(tx, soloQueueScope(r.ID)); err != nil {
				return err
			}
			result := soloLifecycleQuery(tx, &r).Where("lifecycle_action = '' AND status IN ?", []string{models.SoloStatusStarting, models.SoloStatusRunning}).
				Updates(map[string]any{"status": models.SoloStatusError, "error": "原环境记录未找到，聊天和环境关联已保留，请核对后恢复"})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return soloLifecycleChanged()
			}
			var queue models.ChatQueue
			if err := tx.Select("active_turn_id").First(&queue, "id = ?", soloQueueScope(r.ID).ID()).Error; err != nil {
				return err
			}
			activeID = queue.ActiveTurnID
			return nil
		})
		if err != nil {
			continue
		}
		if c := s.lookup(r.ID); c != nil && activeID != "" {
			// Retire already cancelled this epoch's queue atomically. Waking a
			// native watcher afterward must target its exact old turn, never a
			// replacement that started while this callback was delayed.
			_, _ = c.sess.CancelChecked(activeID, false)
		}
		s.publishStatus(r.ID)
	}
}

// ReconcileSandboxReferences repairs dangling bindings left by older startup
// reconciliation, even when there is no sandbox row left to invoke its hook.
func (s *SoloService) ReconcileSandboxReferences() {
	var rows []models.SoloSession
	s.db.Where("status IN ? AND sandbox_id <> 0", []string{models.SoloStatusStarting, models.SoloStatusRunning}).Find(&rows)
	for _, row := range rows {
		var count int64
		if err := s.db.Model(&models.Sandbox{}).Where("id = ?", row.SandboxID).Count(&count).Error; err == nil && count == 0 {
			s.onSandboxGone(row.SandboxID)
		}
	}
}
