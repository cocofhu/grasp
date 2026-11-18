package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func (s *SoloService) openSandbox(ctx context.Context, row *models.SoloSession) error {
	return s.openSandboxWith(ctx, row, nil)
}

// openSandboxWith opens a sandbox whose workspace comes from restore instead
// of cloning. A failed restore returns the session to stopped, keeping its
// archive for another attempt.
func (s *SoloService) openSandboxWith(ctx context.Context, row *models.SoloSession, restore func(context.Context, *sandbox.Sandbox) error) error {
	sid := row.ID
	model := chatModelID(row)
	sb, err := s.sbx.OpenSoloSandbox(ctx, SoloSandboxOpts{
		Restore: restore,
		Profile: row.AgentName, SessionID: sid, Repos: effectiveSessionRepos(row), InitialModelID: model,
		InitialConfigurationID: chatConfigurationID(row),
		ReasoningEffort:        row.CurrentReasoningEffort,
		ServiceTier:            row.CurrentServiceTier,
		OnEnv: func(sources map[string]string) {
			s.db.Model(&models.SoloSession{ID: sid}).Select("EnvSources").
				Updates(&models.SoloSession{EnvSources: sources})
		},
		OnReady: func(sandboxID uint) {
			res := s.db.Model(&models.SoloSession{}).
				Where("id = ? AND sandbox_id = ? AND status = ?", sid, sandboxID, models.SoloStatusStarting).
				Updates(map[string]any{"status": models.SoloStatusRunning, "error": ""})
			if res.RowsAffected > 0 {
				s.publishStatus(sid)
			}
		},
		OnFail: func(sandboxID uint, err error) {
			updates := map[string]any{"status": models.SoloStatusError, "error": truncErr(err)}
			if restore != nil {
				updates = map[string]any{"status": models.SoloStatusStopped, "sandbox_id": 0,
					"error": "恢复失败，工作区备份仍保留，可重试：" + truncErr(err)}
			}
			res := s.db.Model(&models.SoloSession{}).
				Where("id = ? AND sandbox_id = ? AND status IN ?", sid, sandboxID,
					[]string{models.SoloStatusStarting, models.SoloStatusRunning}).
				Updates(updates)
			if res.RowsAffected > 0 {
				s.publishStatus(sid)
			}
		},
	})
	if err != nil {
		return err
	}
	row.SandboxID = sb.ID
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", sid).
		Updates(map[string]any{"sandbox_id": sb.ID, "status": models.SoloStatusStarting, "error": ""}).Error; err != nil {
		return err
	}
	// The new container is born on this model. Later switches compare against it.
	return s.rememberAppliedRoute(sid, chatConfigurationID(row), model, row.CurrentReasoningEffort, row.CurrentServiceTier)
}

func (s *SoloService) onSandboxRecovered(sandboxID uint) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status = ?", sandboxID, models.SoloStatusStarting).Find(&rows)
	for _, row := range rows {
		result := s.db.Model(&models.SoloSession{}).Where("id = ? AND status = ?", row.ID, models.SoloStatusStarting).
			Updates(map[string]any{"status": models.SoloStatusRunning, "error": ""})
		if result.Error == nil && result.RowsAffected > 0 {
			s.publishStatus(row.ID)
		}
	}
	if s.setSandboxPaused(sandboxID, false) {
		s.publishRunningStatus(sandboxID)
	}
	s.retryDeferredRestores(sandboxID)
}

// onSandboxPaused tells open pages once that a running resident was paused by
// the provider; the next recovery clears the mark and publishes again.
func (s *SoloService) onSandboxPaused(sandboxID uint) {
	if !s.setSandboxPaused(sandboxID, true) {
		return
	}
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status IN ?", sandboxID, []string{models.SoloStatusRunning, models.SoloStatusStopped}).Find(&rows)
	for _, row := range rows {
		s.publishStatus(row.ID)
	}
}

// setSandboxPaused records the paused mark and reports whether it changed.
func (s *SoloService) setSandboxPaused(sandboxID uint, paused bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pausedSandboxes == nil {
		s.pausedSandboxes = map[uint]bool{}
	}
	if s.pausedSandboxes[sandboxID] == paused {
		return false
	}
	if paused {
		s.pausedSandboxes[sandboxID] = true
	} else {
		delete(s.pausedSandboxes, sandboxID)
	}
	return true
}

func (s *SoloService) publishRunningStatus(sandboxID uint) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status = ?", sandboxID, models.SoloStatusRunning).Find(&rows)
	for _, row := range rows {
		s.publishStatus(row.ID)
	}
}

// onSandboxGone marks sessions whose sandbox was destroyed outside Close.
func (s *SoloService) onSandboxGone(sandboxID uint) {
	s.failSandboxSessions(sandboxID, "沙箱已被销毁，可重新启动会话", "沙箱已不存在，可从工作区备份恢复")
}

// onSandboxCrashed marks sessions whose sandbox the provider reports crashed,
// before the sandbox is released, so they show the crash instead of hanging.
func (s *SoloService) onSandboxCrashed(sandboxID uint) {
	s.failSandboxSessions(sandboxID, "沙箱已崩溃（虚拟机异常退出），请重新启动会话", "沙箱已崩溃（虚拟机异常退出），可从工作区备份恢复")
}

func (s *SoloService) failSandboxSessions(sandboxID uint, message, backupMessage string) {
	var rows []models.SoloSession
	s.db.Where("sandbox_id = ? AND status IN ?", sandboxID,
		[]string{models.SoloStatusStarting, models.SoloStatusRunning}).Find(&rows)
	for _, r := range rows {
		updates := map[string]any{"status": models.SoloStatusError, "sandbox_id": 0, "error": message}
		if s.backups != nil {
			if _, err := s.legacyRestorePoint(r.ID); err == nil {
				// Resume restores the newest archive into a new sandbox.
				updates["status"], updates["error"] = models.SoloStatusStopped, backupMessage
			}
		}
		result := s.db.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND status IN ?", r.ID, sandboxID,
			[]string{models.SoloStatusStarting, models.SoloStatusRunning}).
			Updates(updates)
		if result.Error != nil || result.RowsAffected == 0 {
			continue
		}
		if c := s.lookup(r.ID); c != nil {
			c.sess.Cancel(true)
		}
		s.clearDeferredQueue(r.ID)
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

func (s *SoloService) destroySandbox(ctx context.Context, sandboxID uint) error {
	if sandboxID == 0 || s.sbx == nil {
		return nil
	}
	return s.sbx.CancelSandbox(ctx, sandboxID)
}
