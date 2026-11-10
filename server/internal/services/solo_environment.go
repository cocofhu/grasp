package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const soloStopPendingMessage = "环境停止尚未确认，聊天和工作区已保留，系统会继续核对停止状态。"

// StopEnvironment stops compute, retaining the session, artifacts, gateway
// identity and writable workspace. Active turns must be stopped separately.
func (s *SoloService) StopEnvironment(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if s.sbx == nil || row.SandboxID == 0 {
		return nil, soloErr(http.StatusConflict, "environment_missing", "当前会话没有可停止的环境")
	}
	if row.Status == models.SoloStatusStarting {
		return nil, soloErr(http.StatusConflict, "environment_starting", "环境正在启动，请就绪后再停止")
	}
	if err := s.sbx.stopSoloEnvironment(ctx, row.SandboxID, func(tx *gorm.DB) error {
		// Admit and Claim take this same queue lock before inspecting the
		// environment. Commit the stop intent under it so either an accepted
		// message blocks stopping or a later message sees desired=stopped.
		if err := s.durable.GuardIdle(tx, soloQueueScope(id)); err != nil {
			if errors.Is(err, chatsession.ErrQueueBusy) {
				return soloErr(http.StatusConflict, "session_busy", "请先停止当前轮次并处理待发送消息")
			}
			if errors.Is(err, chatsession.ErrQueueRetired) {
				return soloErr(http.StatusConflict, "session_closed", "会话已关闭，请重新加载")
			}
			return err
		}
		var current models.SoloSession
		if err := tx.Select("id", "sandbox_id", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action").First(&current, "id = ?", id).Error; err != nil {
			return err
		}
		if current.SandboxID != row.SandboxID || current.LifecycleEpoch != row.LifecycleEpoch || current.LifecycleAttemptID != row.LifecycleAttemptID || current.LifecycleAction != "" || current.Status == models.SoloStatusStarting || current.Status == models.SoloStatusClosed {
			return soloErr(http.StatusConflict, "session_changed", "会话状态已变化，请重新加载")
		}
		return nil
	}); err != nil {
		s.publishStatus(id)
		return nil, err
	}
	s.appendSystem(id, "环境已停止，聊天和工作区已保留。继续执行前可恢复环境。")
	s.publishStatus(id)
	return s.Get(ctx, id, user)
}

func (s *SandboxService) stopSoloEnvironment(ctx context.Context, id uint, prepare ...func(*gorm.DB) error) error {
	return s.stopSoloEnvironmentIntent(ctx, id, false, prepare...)
}

// retrySoloStop may advance only a persisted stop intent. A stale background
// scan must not turn a later successful reopen into a new stop request.
func (s *SandboxService) retrySoloStop(ctx context.Context, id uint) error {
	return s.stopSoloEnvironmentIntent(ctx, id, true)
}

func (s *SandboxService) stopSoloEnvironmentIntent(ctx context.Context, id uint, requireStoppedIntent bool, prepare ...func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(ctx, sandbox.GatewayStopTimeout)
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
		return errors.New("not a Solo environment")
	}
	if requireStoppedIntent && row.DesiredState != "stopped" {
		return soloLifecycleChanged()
	}
	var parent models.SoloSession
	if err := s.db.WithContext(ctx).Select("id", "user_id", "sandbox_id", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action").First(&parent, "id = ? AND sandbox_id = ?", row.ThreadID, id).Error; err != nil {
		return soloLifecycleChanged()
	}
	closing := parent.Status == models.SoloStatusClosed && parent.LifecycleAction == soloCloseCleanup
	if !closing && (parent.Status == models.SoloStatusClosed || parent.LifecycleAction != "") {
		return soloLifecycleChanged()
	}
	if s.isBusyRow(row) {
		return soloErr(http.StatusConflict, "session_busy", "环境仍在执行任务")
	}
	s.mu.Lock()
	live := s.live[id]
	local := live != nil && live.sb != nil && live.sb.IsLocal()
	s.mu.Unlock()
	if !local && (s.mgr == nil || s.mgr.Gateway() == nil) {
		return soloErr(http.StatusConflict, "environment_stop_unsupported", "本地进程环境暂不支持保留环境停机")
	}
	recovery := models.SandboxRecovery{History: "retained", Files: "unverified", NativeSession: "unverified", Process: "stop_requested", Reason: "停止请求待确认，文件可用性尚未验证"}
	if !local {
		if info, err := s.mgr.Gateway().InfoFor(ctx, row.Name); err == nil && info.Driver == "e2b" && info.Capabilities.StopMode == "pause" {
			recovery.Process = "pause_requested"
			recovery.Reason = "E2B 原生暂停；连接恢复后验证运行状态"
		}
	}
	recoveryJSON, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	// Keep compute -> native queue -> parent -> sandbox ordering for both
	// intent and confirmation. Closing already retired the queue; touching that
	// same queue again cannot cancel a successor because its parent CAS rolls
	// back this entire transaction first.
	var stoppedHead string
	guard := func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		queue := chatsession.NewDurableStore(s.db)
		if closing {
			if err := queue.Retire(tx, soloQueueScope(parent.ID)); err != nil {
				return err
			}
			if len(prepare) != 0 && prepare[0] != nil {
				if err := prepare[0](tx); err != nil {
					return err
				}
			}
		} else if len(prepare) != 0 && prepare[0] != nil {
			if err := prepare[0](tx); err != nil {
				return err
			}
		} else if err := queue.GuardIdle(tx, soloQueueScope(parent.ID)); err != nil {
			return err
		}
		var current models.SoloSession
		err := soloLifecycleQuery(tx, &parent).Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("status = ? AND lifecycle_action = ?", parent.Status, parent.LifecycleAction).Take(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return soloLifecycleChanged()
		}
		if err != nil {
			return err
		}
		// The provider observation belongs to this exact logical workspace.
		// Lock its metadata after the parent and before the physical fence.
		head, err := workspaceStoppedHeadIdentity(tx.Statement.Context, tx, id)
		if err != nil {
			return err
		}
		if stoppedHead != "" && stoppedHead != head {
			return soloLifecycleChanged()
		}
		stoppedHead = head
		return nil
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if err := s.commitSandboxOperationPrepared(ctx, id, guard, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(ctx, tx, id, map[string]any{"desired_state": "stopped", "destroy_at": nil, "recovery": string(recoveryJSON)})
	}); err != nil {
		return err
	}
	observed := "stopped"
	if local {
		if err := live.sb.StopLocal(ctx); err != nil {
			return s.unconfirmedSoloStop(ctx, id, err)
		}
	} else {
		// A previous stop may have completed after its HTTP acknowledgement
		// was lost. Confirm physical state before repeating the provider call.
		observed, err = s.mgr.ProbeStatus(ctx, row.Name)
		if err != nil || (observed != "stopped" && observed != "exited") {
			if err := s.mgr.Gateway().Stop(ctx, row.Name); err != nil {
				return s.unconfirmedSoloStop(ctx, id, err)
			}
			observed, err = s.mgr.ProbeStatus(ctx, row.Name)
			if err != nil {
				return s.unconfirmedSoloStop(ctx, id, err)
			}
		}
	}
	if observed != "stopped" && observed != "exited" {
		return soloErr(http.StatusConflict, "environment_stop_pending", "已请求停止，正在等待环境确认")
	}
	if recovery.Process == "pause_requested" {
		recovery.Process = "paused"
		recovery.Reason = "E2B 原生暂停已确认；恢复后验证连接状态"
	} else {
		recovery.Process = "restarted"
		recovery.Reason = "环境停止已确认；冷启动会重新启动服务，并使用已保存的聊天上下文"
	}
	recovery.Files = "retained"
	recoveryJSON, err = json.Marshal(recovery)
	if err != nil {
		return err
	}
	if err := s.commitSandboxOperationPrepared(ctx, id, guard, func(tx *gorm.DB) error {
		if err := s.updateSandboxOperation(ctx, tx, id, map[string]any{"status": "stopped", "error": "", "destroy_at": nil, "recovery": string(recoveryJSON)}); err != nil {
			return err
		}
		status := models.SoloStatusStopped
		if closing {
			status = models.SoloStatusClosed
		}
		result := soloLifecycleQuery(tx, &parent).Where("status = ? AND lifecycle_action = ?", parent.Status, parent.LifecycleAction).
			Updates(map[string]any{"status": status, "lifecycle_action": "", "error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return soloErr(http.StatusConflict, "session_changed", "会话状态已改变，请刷新后查看环境状态")
		}
		// A confirmed stop ends every connection to this same environment,
		// including disconnect receipts lost when an application exited.
		// Publish this only with the parent/head and physical-operation fences.
		return closeStoppedWorkspaceConsumersTx(tx.Statement.Context, tx, id)
	}); err != nil {
		return err
	}
	if local {
		// Keep the proved original local handle and its root/home for retry
		// and explicit resume. teardownLive calls Destroy/Release for locals.
		s.mu.Lock()
		acp := live.acp
		live.acp = nil
		s.mu.Unlock()
		if acp != nil {
			acp.Close()
		}
		s.codexLogins.Unregister(liveCodexKey(id))
	} else {
		s.teardownLive(id)
	}
	if closing && s.host != nil && row.RunID != "" {
		s.host.UnregisterRun(row.RunID)
	}
	return nil
}

func (s *SandboxService) unconfirmedSoloStop(ctx context.Context, id uint, failure error) error {
	return errors.Join(soloErr(http.StatusConflict, "environment_stop_pending", soloStopPendingMessage), failure, s.recordSoloStopFailure(ctx, id, failure))
}

func (s *SandboxService) recordSoloStopFailure(ctx context.Context, id uint, failure error) error {
	log.Warn().Err(failure).Uint("sandbox_id", id).Msg("environment stop unconfirmed; retaining workspace and compute reservation")
	reason := soloStopPendingMessage
	recovery := models.SandboxRecovery{History: "retained", Files: "unverified", NativeSession: "unverified", Process: "unverified", Reason: reason}
	if sandbox.GatewayRecordMissing(failure) {
		reason = "原环境记录未找到，聊天和原关联已保留；文件不可确认，未自动创建替代环境"
		recovery.Files, recovery.NativeSession, recovery.Process, recovery.Reason = "unavailable", "unavailable", "unavailable", reason
	}
	body, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.commitSandboxOperation(cleanup, id, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(cleanup, tx, id, map[string]any{"status": "error", "error": reason, "recovery": string(body), "destroy_at": nil})
	})
}

func (s *SoloService) ResumeEnvironment(ctx context.Context, id, user string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status == models.SoloStatusClosed {
		return s.Restart(ctx, id, user)
	}
	if row.SandboxID == 0 || s.sbx == nil {
		return nil, soloErr(http.StatusConflict, "environment_missing", "环境已释放，请重新启动会话")
	}
	operationCtx, unlock, err := s.sbx.acquireSandboxOperation(ctx, row.SandboxID)
	if err != nil {
		return nil, err
	}
	s.sbx.mu.Lock()
	local := s.sbx.live[row.SandboxID]
	isLocal := local != nil && local.sb != nil && local.sb.IsLocal()
	s.sbx.mu.Unlock()
	if isLocal {
		var current models.Sandbox
		err = s.db.WithContext(operationCtx).Select("status", "desired_state").First(&current, row.SandboxID).Error
		if err == nil && current.DesiredState == "stopped" && current.Status != "stopped" {
			err = soloErr(http.StatusConflict, "environment_stop_pending", "原本地运行进程停止尚未确认，请先核对原停止请求")
		}
		if err != nil {
			unlock()
			return nil, err
		}
	}
	err = s.sbx.commitSandboxOperationPrepared(operationCtx, row.SandboxID, func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		count, err := countSoloCompute(tx, row.SandboxID)
		if err != nil {
			return err
		}
		if count >= int64(s.sbx.MaxSoloSandboxes()) {
			return &SoloError{Status: http.StatusConflict, Code: "solo_limit", Msg: ErrSoloSandboxLimit{Max: s.sbx.MaxSoloSandboxes()}.Error()}
		}
		return nil
	}, func(tx *gorm.DB) error {
		if err := s.sbx.updateSandboxOperation(operationCtx, tx, row.SandboxID, map[string]any{"desired_state": "running", "destroy_at": nil}); err != nil {
			return err
		}
		result := soloLifecycleQuery(tx, row).Where("status <> ? AND lifecycle_action = ''", models.SoloStatusClosed).
			Updates(map[string]any{"status": models.SoloStatusStarting, "closed_at": nil, "error": "", "last_active_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return soloErr(http.StatusConflict, "session_changed", "会话状态已改变，请刷新后查看环境状态")
		}
		return nil
	})
	unlock()
	if err != nil {
		return nil, err
	}
	if err = s.sbx.EnsureSoloRunning(ctx, row.SandboxID); err != nil {
		s.publishStatus(id)
		return nil, err
	}
	s.appendSystem(id, "环境已恢复，继续沿用保留的工作区。")
	s.publishStatus(id)
	return s.Get(ctx, id, user)
}
