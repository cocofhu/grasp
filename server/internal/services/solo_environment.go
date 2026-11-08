package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
	if c := s.lookup(id); c != nil {
		snapshot := c.sess.Snapshot()
		if snapshot.Busy || snapshot.Waiting > 0 {
			return nil, soloErr(http.StatusConflict, "session_busy", "请先停止当前轮次并处理待发送消息")
		}
	}
	if err := s.sbx.stopSoloEnvironment(ctx, row.SandboxID); err != nil {
		s.publishStatus(id)
		return nil, err
	}
	s.appendSystem(id, "环境已停止，聊天和工作区已保留。继续执行前可恢复环境。")
	s.publishStatus(id)
	return s.Get(ctx, id, user)
}

func (s *SandboxService) stopSoloEnvironment(ctx context.Context, id uint) error {
	ctx, cancel := context.WithTimeout(ctx, soloRecoveryTimeout)
	defer cancel()
	unlock, err := s.lockSoloRecovery(ctx, id)
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
	if err := s.db.Model(&models.Sandbox{}).Where("id = ? AND operation_owner = ?", id, row.OperationOwner).
		Updates(map[string]any{"desired_state": "stopped", "destroy_at": nil, "recovery": string(recoveryJSON)}).Error; err != nil {
		return err
	}
	if err := s.mgr.Gateway().Stop(ctx, row.Name); err != nil {
		s.recordSoloStopFailure(id, err)
		return err
	}
	observed, err := s.mgr.ProbeStatus(ctx, row.Name)
	if err != nil {
		s.recordSoloStopFailure(id, err)
		return err
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
	s.teardownLive(id)
	if err := s.db.Model(&models.Sandbox{}).Where("id = ? AND operation_owner = ?", id, row.OperationOwner).
		Updates(map[string]any{"status": "stopped", "error": "", "destroy_at": nil, "recovery": string(recoveryJSON)}).Error; err != nil {
		return err
	}
	return s.db.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ?", row.ThreadID, id).
		Updates(map[string]any{"status": models.SoloStatusStopped, "error": ""}).Error
}

func (s *SandboxService) recordSoloStopFailure(id uint, err error) {
	_ = s.db.Model(&models.Sandbox{}).Where("id = ? AND desired_state = ?", id, "stopped").Updates(map[string]any{"status": "error", "error": "环境停止尚未确认，工作区已保留，可重试：" + truncErr(err), "destroy_at": nil}).Error
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
	unlock, err := s.sbx.lockSoloRecovery(ctx, row.SandboxID)
	if err != nil {
		return nil, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
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
		if err := tx.Model(&models.Sandbox{}).Where("id = ?", row.SandboxID).Updates(map[string]any{"desired_state": "running", "destroy_at": nil}).Error; err != nil {
			return err
		}
		return tx.Model(&models.SoloSession{}).Where("id = ?", id).Updates(map[string]any{"status": models.SoloStatusStarting, "closed_at": nil, "error": "", "last_active_at": time.Now().UTC()}).Error
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
