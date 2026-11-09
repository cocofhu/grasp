package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

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
		if err := tx.Select("id", "sandbox_id", "status").First(&current, "id = ?", id).Error; err != nil {
			return err
		}
		if current.SandboxID != row.SandboxID || current.Status == models.SoloStatusStarting || current.Status == models.SoloStatusClosed {
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
		return errors.New("not a Solo environment")
	}
	if s.isBusyRow(row) {
		return soloErr(http.StatusConflict, "session_busy", "环境仍在执行任务")
	}
	s.mu.Lock()
	live := s.live[id]
	local := live != nil && live.sb != nil && live.sb.IsLocal()
	s.mu.Unlock()
	if local || s.mgr == nil || s.mgr.Gateway() == nil {
		return soloErr(http.StatusConflict, "environment_stop_unsupported", "本地进程环境暂不支持保留环境停机")
	}
	recovery := models.SandboxRecovery{History: "retained", Files: "retained", NativeSession: "unverified", Process: "restarted", Reason: "冷启动会重新启动服务，并使用已保存的聊天上下文"}
	if info, err := s.mgr.Gateway().InfoFor(ctx, row.Name); err == nil && info.Driver == "e2b" && info.Capabilities.StopMode == "pause" {
		recovery.Process = "pause_requested"
		recovery.Reason = "E2B 原生暂停；连接恢复后验证运行状态"
	}
	recoveryJSON, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	var admit func(*gorm.DB) error
	if len(prepare) != 0 {
		admit = prepare[0]
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if err := s.commitSandboxOperationPrepared(ctx, id, admit, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(ctx, tx, id, map[string]any{"desired_state": "stopped", "destroy_at": nil, "recovery": string(recoveryJSON)})
	}); err != nil {
		return err
	}
	if err := s.mgr.Gateway().Stop(ctx, row.Name); err != nil {
		return errors.Join(err, s.recordSoloStopFailure(ctx, id, err))
	}
	observed, err := s.mgr.ProbeStatus(ctx, row.Name)
	if err != nil {
		return errors.Join(err, s.recordSoloStopFailure(ctx, id, err))
	}
	if observed != "stopped" && observed != "exited" {
		return soloErr(http.StatusConflict, "environment_stop_pending", "已请求停止，正在等待环境确认")
	}
	if recovery.Process == "pause_requested" {
		recovery.Process = "paused"
		recoveryJSON, err = json.Marshal(recovery)
		if err != nil {
			return err
		}
	}
	if err := s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
		if err := s.updateSandboxOperation(ctx, tx, id, map[string]any{"status": "stopped", "error": "", "destroy_at": nil, "recovery": string(recoveryJSON)}); err != nil {
			return err
		}
		// Close retires the session before it tears down its environment. A
		// stop finishing later must leave that closed state authoritative.
		result := tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND status <> ?", row.ThreadID, id, models.SoloStatusClosed).
			Updates(map[string]any{"status": models.SoloStatusStopped, "error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return soloErr(http.StatusConflict, "session_changed", "会话状态已改变，请刷新后查看环境状态")
		}
		return nil
	}); err != nil {
		return err
	}
	s.teardownLive(id)
	return nil
}

func (s *SandboxService) recordSoloStopFailure(ctx context.Context, id uint, failure error) error {
	return s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(ctx, tx, id, map[string]any{"status": "error", "error": "环境停止尚未确认，工作区已保留，可重试：" + truncErr(failure), "destroy_at": nil})
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
		result := tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND status <> ?", id, row.SandboxID, models.SoloStatusClosed).
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
