package services

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// Legacy cleanup intents never authorize deletion or replacement of a disk.
	soloRestartCleanup = "restart_cleanup"
	soloRestartCreate  = "restart_create"
	soloCloseCleanup   = "close_cleanup"
	soloDeleteCleanup  = "delete_cleanup"
)

func soloCleanupPending(action string) bool {
	return action == soloRestartCleanup || action == soloCloseCleanup || action == soloDeleteCleanup
}

func sameSoloLifecycle(a, b *models.SoloSession) bool {
	return a.ID == b.ID && a.UserID == b.UserID && a.LifecycleEpoch == b.LifecycleEpoch &&
		a.LifecycleAttemptID == b.LifecycleAttemptID && a.LifecycleAction == b.LifecycleAction &&
		a.SandboxID == b.SandboxID && a.Status == b.Status
}

func soloLifecycleQuery(tx *gorm.DB, row *models.SoloSession) *gorm.DB {
	return tx.Model(&models.SoloSession{}).Where("id = ? AND user_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND sandbox_id = ?", row.ID, row.UserID, row.LifecycleEpoch, row.LifecycleAttemptID, row.SandboxID)
}

func soloLifecycleChanged() error {
	return soloErr(http.StatusConflict, "session_changed", "会话状态已变化，请核对当前环境后重试")
}

func soloLifecycleObsolete(err error) bool {
	var failure *SoloError
	return errors.As(err, &failure) && failure.Code == "session_changed"
}

func (s *SoloService) requireSoloLifecycle(tx *gorm.DB, expected *models.SoloSession) (*models.SoloSession, error) {
	var row models.SoloSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND user_id = ?", expected.ID, expected.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, soloErr(http.StatusNotFound, "session_not_found", "Solo 会话不存在")
		}
		return nil, err
	}
	if !sameSoloLifecycle(&row, expected) {
		return nil, soloLifecycleChanged()
	}
	return &row, nil
}

// claimSoloRestart admits a fresh resource only for a session without any old
// resource association. A retained or uncertain disk can never take this path.
func (s *SoloService) claimSoloRestart(ctx context.Context, expected *models.SoloSession) (*models.SoloSession, error) {
	var claimed *models.SoloSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		if err := s.durable.Reopen(tx, soloQueueScope(expected.ID)); err != nil {
			return err
		}
		row, err := s.requireSoloLifecycle(tx, expected)
		if err != nil {
			return err
		}
		if row.SandboxID != 0 || row.LifecycleAction != "" || row.Status == models.SoloStatusStarting || row.Status == models.SoloStatusRunning {
			return soloLifecycleChanged()
		}
		count, err := countSoloCompute(tx, 0)
		if err != nil {
			return err
		}
		if count >= int64(s.sbx.MaxSoloSandboxes()) {
			return soloErr(http.StatusConflict, "solo_limit", "%s", ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error())
		}
		var messages int64
		if err := tx.Model(&models.SoloMessage{}).Where("session_id = ? AND role <> ?", row.ID, "system").Count(&messages).Error; err != nil {
			return err
		}
		now, attempt := time.Now().UTC(), uuid.NewString()
		result := soloLifecycleQuery(tx, row).Where("status = ? AND lifecycle_action = ''", row.Status).Updates(map[string]any{
			"lifecycle_epoch": row.LifecycleEpoch + 1, "lifecycle_attempt_id": attempt, "lifecycle_action": soloRestartCreate,
			"lifecycle_source_sandbox_id": 0, "status": models.SoloStatusStarting, "error": "",
			"restarts": gorm.Expr("restarts + 1"), "needs_preamble": messages > 0, "closed_at": nil, "last_active_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return soloLifecycleChanged()
		}
		row.LifecycleEpoch++
		row.LifecycleAttemptID, row.LifecycleAction = attempt, soloRestartCreate
		row.LifecycleSourceSandboxID = 0
		row.Status, row.Error, row.ClosedAt, row.LastActiveAt = models.SoloStatusStarting, "", nil, &now
		row.Restarts++
		row.NeedsPreamble = messages > 0
		claimed = row
		return nil
	})
	return claimed, err
}

func (s *SoloService) advanceSoloRestart(ctx context.Context, row *models.SoloSession) error {
	if row.LifecycleAction != soloRestartCreate || row.SandboxID != 0 {
		return soloLifecycleChanged()
	}
	if err := s.openSandbox(ctx, row); err != nil {
		// The known, unbound intent remains a reservation for explicit retry
		// or startup recovery. Never overwrite a newer close/delete/attempt.
		soloLifecycleQuery(s.db, row).Where("status = ? AND lifecycle_action = ?", models.SoloStatusStarting, soloRestartCreate).
			Updates(map[string]any{"error": truncErr(err)})
		return err
	}
	return nil
}

// reopenRetainedSolo admits compute for the same confirmed-stopped resource.
// Its committed epoch/attempt + desired=running is the restart receipt. Provider
// calls happen afterward under the same physical lease and current auth gate.
func (s *SoloService) reopenRetainedSolo(ctx context.Context, expected *models.SoloSession) error {
	ctx, unlock, err := s.sbx.acquireSandboxOperation(ctx, expected.SandboxID, "reopen")
	if err != nil {
		return err
	}
	defer unlock()
	err = s.sbx.commitSandboxOperationPrepared(ctx, expected.SandboxID, func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		if err := s.durable.Reopen(tx, soloQueueScope(expected.ID)); err != nil {
			return err
		}
		row, err := s.requireSoloLifecycle(tx, expected)
		if err != nil {
			return err
		}
		if row.Status != models.SoloStatusClosed || row.LifecycleAction != "" || row.SandboxID == 0 {
			return soloLifecycleChanged()
		}
		count, err := countSoloCompute(tx, row.SandboxID)
		if err != nil {
			return err
		}
		if count >= int64(s.sbx.MaxSoloSandboxes()) {
			return soloErr(http.StatusConflict, "solo_limit", "%s", ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error())
		}
		return nil
	}, func(tx *gorm.DB) error {
		var environment models.Sandbox
		if err := tx.First(&environment, expected.SandboxID).Error; err != nil {
			return err
		}
		if environment.Purpose != SandboxPurposeSolo || environment.ThreadID != expected.ID || environment.Status != "stopped" || environment.DesiredState != "stopped" {
			return soloErr(http.StatusConflict, "environment_stop_pending", "原环境停止尚未确认，已保留文件和关联，请先核对状态")
		}
		if err := s.sbx.updateSandboxOperation(ctx, tx, environment.ID, map[string]any{"desired_state": "running", "destroy_at": nil}); err != nil {
			return err
		}
		result := soloLifecycleQuery(tx, expected).Where("status = ? AND lifecycle_action = ''", models.SoloStatusClosed).
			Updates(map[string]any{"lifecycle_epoch": expected.LifecycleEpoch + 1, "lifecycle_attempt_id": uuid.NewString(),
				"lifecycle_source_sandbox_id": expected.SandboxID, "status": models.SoloStatusStarting,
				"closed_at": nil, "error": "", "last_active_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return soloLifecycleChanged()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.sbx.EnsureSoloRunning(ctx, expected.SandboxID)
}

// Restart reopens retained files on the same resource. Only sessions that have
// no resource association may admit one new, persistently fenced allocation.
func (s *SoloService) Restart(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if s.sbx == nil || s.agents == nil {
		return nil, errors.New("solo unavailable")
	}
	if soloCleanupPending(row.LifecycleAction) {
		return nil, soloErr(http.StatusConflict, "environment_stop_pending", "原环境操作尚未确认，已保留文件和关联，请先核对状态")
	}
	if row.LifecycleAction != soloRestartCreate {
		if row.Status == models.SoloStatusStarting || row.Status == models.SoloStatusRunning {
			return nil, soloErr(http.StatusConflict, "session_active", "会话仍在运行，无需重新启动")
		}
		if row.SandboxID != 0 {
			if row.Status != models.SoloStatusClosed {
				return s.ResumeEnvironment(ctx, id, user)
			}
			if err := s.reopenRetainedSolo(ctx, row); err != nil {
				s.publishStatus(id)
				return nil, err
			}
			s.appendSystem(id, "会话已重新打开，继续沿用保留的工作区。")
			s.publishStatus(id)
			return s.Get(ctx, id, user)
		}
		if _, ok := s.agents.Get(row.AgentName); !ok {
			return nil, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 已不存在，无法重新启动", row.AgentName)
		}
		row, err = s.claimSoloRestart(ctx, row)
		if err != nil {
			return nil, err
		}
	}
	if err := s.advanceSoloRestart(ctx, row); err != nil {
		s.publishStatus(id)
		return nil, err
	}
	s.appendSystem(id, "正在新沙箱中重新启动，聊天历史已保留")
	s.publishStatus(id)
	return s.Get(ctx, id, user)
}

func (s *SoloService) claimSoloRemoval(ctx context.Context, expected *models.SoloSession, action string) (*models.SoloSession, error) {
	var claimed *models.SoloSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		if err := s.durable.Retire(tx, soloQueueScope(expected.ID)); err != nil {
			return err
		}
		row, err := s.requireSoloLifecycle(tx, expected)
		if err != nil {
			return err
		}
		if action == soloDeleteCleanup {
			if row.SandboxID != 0 {
				return soloErr(http.StatusConflict, "environment_retained", "工作区仍保留在原环境中，请先明确释放环境后再删除聊天")
			}
			if row.Status == models.SoloStatusStarting || row.Status == models.SoloStatusRunning {
				return soloErr(http.StatusConflict, "session_active", "请先关闭会话再删除")
			}
		}
		if row.LifecycleAction == soloDeleteCleanup && action != soloDeleteCleanup {
			return soloLifecycleChanged()
		}
		now, attempt := time.Now().UTC(), uuid.NewString()
		result := soloLifecycleQuery(tx, row).Where("status = ? AND lifecycle_action = ?", row.Status, row.LifecycleAction).Updates(map[string]any{
			"lifecycle_epoch": row.LifecycleEpoch + 1, "lifecycle_attempt_id": attempt, "lifecycle_action": action,
			"lifecycle_source_sandbox_id": row.SandboxID, "status": models.SoloStatusClosed, "closed_at": now, "error": "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return soloLifecycleChanged()
		}
		if action == soloCloseCleanup && row.SandboxID != 0 {
			// The parent receipt authorizes stopping this binding only. A create
			// already in flight must adopt its result, stop it, and retain disk.
			if err := tx.Model(&models.Sandbox{}).Where("id = ? AND purpose = ? AND thread_id = ?", row.SandboxID, SandboxPurposeSolo, row.ID).
				Updates(map[string]any{"desired_state": "stopped", "destroy_at": nil}).Error; err != nil {
				return err
			}
		}
		row.LifecycleEpoch++
		row.LifecycleAttemptID, row.LifecycleAction, row.LifecycleSourceSandboxID = attempt, action, row.SandboxID
		row.Status, row.ClosedAt = models.SoloStatusClosed, &now
		claimed = row
		return nil
	})
	return claimed, err
}

func (s *SoloService) recordSoloCloseFailure(row *models.SoloSession, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := soloLifecycleQuery(s.db.WithContext(ctx), row).Where("status = ? AND lifecycle_action = ?", models.SoloStatusClosed, soloCloseCleanup).
		Update("error", "环境停止尚未确认，聊天已关闭，工作区和环境关联已保留，请核对后重试")
	if result.Error != nil {
		return result.Error
	}
	return soloErr(http.StatusConflict, "environment_stop_pending", "环境停止尚未确认，聊天已关闭，工作区和环境关联已保留，请核对后重试：%s", truncErr(cause))
}

// stopClosedSolo carries the accepted Close receipt through a potentially
// long physical-lease wait. Even cancelling ACP before this fence would let a
// delayed old Close interrupt a new turn on the same retained resource.
func (s *SoloService) stopClosedSolo(ctx context.Context, expected *models.SoloSession) error {
	stop, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.sbx.stopSoloEnvironment(stop, expected.SandboxID, func(tx *gorm.DB) error {
		_, err := s.requireSoloLifecycle(tx, expected)
		return err
	})
}

// Close retires the chat queue and stops compute. It never deletes the only
// writable workspace. A pending stop retains its binding and compute quota.
func (s *SoloService) Close(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status == models.SoloStatusClosed && row.LifecycleAction == "" {
		if row.SandboxID == 0 {
			return s.Get(ctx, id, user)
		}
		var environment models.Sandbox
		if err := s.db.WithContext(ctx).First(&environment, row.SandboxID).Error; err != nil {
			return nil, soloErr(http.StatusConflict, "environment_stop_pending", "原环境当前无法核对，聊天和环境关联已保留")
		}
		if environment.Status == "stopped" && environment.DesiredState == "stopped" {
			return s.Get(ctx, id, user)
		}
	}
	if row.LifecycleAction != soloCloseCleanup {
		row, err = s.claimSoloRemoval(ctx, row, soloCloseCleanup)
		if err != nil {
			return nil, err
		}
	}
	if row.SandboxID != 0 {
		if s.sbx == nil {
			return nil, s.recordSoloCloseFailure(row, errors.New("solo unavailable"))
		}
		if err := s.stopClosedSolo(ctx, row); err != nil {
			return nil, s.recordSoloCloseFailure(row, err)
		}
	} else {
		result := soloLifecycleQuery(s.db.WithContext(ctx), row).Where("status = ? AND lifecycle_action = ?", models.SoloStatusClosed, soloCloseCleanup).
			Updates(map[string]any{"lifecycle_action": "", "error": ""})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, soloLifecycleChanged()
		}
	}
	s.appendSystem(id, "会话已关闭，计算已停止，聊天和工作区已保留。")
	s.publishStatus(id)
	return s.Get(ctx, id, user)
}

// Delete removes history only after no environment remains bound. Releasing a
// retained workspace requires a separate explicit, loss-aware confirmation.
func (s *SoloService) Delete(ctx context.Context, id, user string) error {
	row, err := s.Require(id, user)
	if err != nil {
		return err
	}
	if row.SandboxID != 0 {
		return soloErr(http.StatusConflict, "environment_retained", "工作区仍保留在原环境中，请先明确释放环境后再删除聊天")
	}
	if row.LifecycleAction != soloDeleteCleanup {
		row, err = s.claimSoloRemoval(ctx, row, soloDeleteCleanup)
		if err != nil {
			return err
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		if err := s.durable.Retire(tx, soloQueueScope(id)); err != nil {
			return err
		}
		if _, err := s.requireSoloLifecycle(tx, row); err != nil {
			return err
		}
		for _, deletion := range []struct {
			model any
			where string
			value string
		}{
			{&models.ChatPendingTurn{}, "surface = ? AND session_key = ?", id},
			{&models.ChatEventChunk{}, "queue_id = ?", soloQueueScope(id).ID()},
			{&models.ChatQueueTurn{}, "queue_id = ?", soloQueueScope(id).ID()},
			{&models.ChatQueue{}, "id = ?", soloQueueScope(id).ID()},
			{&models.SoloMessage{}, "session_id = ?", id},
		} {
			query := tx.Where(deletion.where, deletion.value)
			if _, pending := deletion.model.(*models.ChatPendingTurn); pending {
				query = tx.Where(deletion.where, soloPendingSurface, deletion.value)
			}
			if err := query.Delete(deletion.model).Error; err != nil {
				return err
			}
		}
		return soloLifecycleQuery(tx, row).Where("lifecycle_action = ?", soloDeleteCleanup).Delete(&models.SoloSession{}).Error
	})
	if err == nil {
		s.mu.Lock()
		delete(s.chats, id)
		s.mu.Unlock()
	}
	return err
}

// Recovery advances only an explicitly accepted fresh, unbound restart. Legacy
// cleanup receipts stay blocked; neither startup nor history reads discard disks.
func (s *SoloService) restoreSoloRestartIntents() {
	var rows []models.SoloSession
	if err := s.db.Where("lifecycle_action = ? AND sandbox_id = 0 AND status IN ?", soloRestartCreate, []string{models.SoloStatusStarting, models.SoloStatusError}).Limit(16).Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = s.advanceSoloRestart(ctx, &rows[i])
		cancel()
	}
}
