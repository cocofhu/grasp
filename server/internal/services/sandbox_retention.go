package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// stopRetainedSandbox has the same data-preserving boundary as Solo stop.
// Use a database operation lease to serialize provider effects across replicas.
func (s *SandboxService) stopRetainedSandbox(ctx context.Context, id uint) error {
	ctx, unlock, err := s.acquireSandboxOperation(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	row, err := s.Get(id)
	if err != nil {
		return err
	}
	if s.isBusyRow(row) {
		return fmt.Errorf("sandbox 正在执行任务,无法停止")
	}
	if s.mgr == nil || s.mgr.Gateway() == nil {
		return fmt.Errorf("环境暂不支持保留工作区停机")
	}
	recovery := models.SandboxRecovery{History: "retained", Files: "retained", NativeSession: "unverified", Process: "restarted", Reason: "工作区已保留；恢复环境后重新启动服务"}
	if info, err := s.mgr.Gateway().InfoFor(ctx, row.Name); err == nil && info.Driver == "e2b" && info.Capabilities.StopMode == "pause" {
		recovery.Process = "pause_requested"
	}
	encoded, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	if err := s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(ctx, tx, id, map[string]any{"desired_state": "stopped", "recovery": string(encoded)})
	}); err != nil {
		return err
	}
	if err := s.mgr.Gateway().Stop(ctx, row.Name); err != nil {
		return errors.Join(err, s.recordSoloStopFailure(ctx, id, err))
	}
	status, err := s.mgr.ProbeStatus(ctx, row.Name)
	if err != nil {
		return errors.Join(err, s.recordSoloStopFailure(ctx, id, err))
	}
	if status != "stopped" && status != "exited" {
		return fmt.Errorf("已请求停止，等待环境确认（%s）", status)
	}
	if recovery.Process == "pause_requested" {
		recovery.Process = "paused"
		encoded, err = json.Marshal(recovery)
		if err != nil {
			return err
		}
	}
	if err := s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(ctx, tx, id, map[string]any{"status": "stopped", "error": "", "destroy_at": nil, "recovery": string(encoded)})
	}); err != nil {
		return err
	}
	s.teardownLive(id)
	return nil
}
