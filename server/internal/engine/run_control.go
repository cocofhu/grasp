package engine

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (e *Engine) PauseRun(runID string, version uint64) error {
	err := e.db.Transaction(func(tx *gorm.DB) error {
		var run models.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		if version != run.ControlVersion {
			return &services.AdmissionError{Code: "control_conflict", Message: "任务状态已变化，请刷新后重试", Status: 409}
		}
		if run.Status == "paused" || run.PauseRequested {
			return nil
		}
		if run.Status != "running" && run.Status != "queued" && run.Status != "waiting_human" {
			return errors.New("当前任务不能暂停")
		}
		updates := map[string]any{"pause_requested": true, "control_version": run.ControlVersion + 1}
		if run.Status == "queued" {
			updates["status"] = "paused"
			updates["pause_from_status"] = "queued"
			updates["pause_requested"] = false
		}
		result := tx.Model(&models.Run{}).Where("id = ? AND control_version = ? AND status = ?", runID, version, run.Status).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("task changed during pause request")
		}
		if run.Status == "queued" {
			return services.EnqueueWorkflowEvent(tx, runID, "paused")
		}
		return nil
	})
	if err == nil {
		e.parkWaitingPauses()
		e.publishRunControl(runID)
		e.signalDispatch()
	}
	return err
}

func (e *Engine) ResumePausedRun(runID string, version uint64) error {
	err := e.db.Transaction(func(tx *gorm.DB) error {
		var run models.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		if version != run.ControlVersion {
			return &services.AdmissionError{Code: "control_conflict", Message: "任务状态已变化，请刷新后重试", Status: 409}
		}
		if run.Status != "paused" && !run.PauseRequested {
			return fmt.Errorf("任务没有待恢复的暂停")
		}
		status := run.Status
		if status == "paused" {
			status = run.PauseFromStatus
			if status != "waiting_human" {
				status = "queued"
			}
		}
		result := tx.Model(&models.Run{}).Where("id = ? AND control_version = ?", runID, version).Updates(map[string]any{"status": status, "pause_requested": false, "pause_from_status": "", "control_version": version + 1})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("task changed during resume request")
		}
		return services.EnqueueWorkflowEvent(tx, runID, status)
	})
	if err == nil {
		e.publishRunControl(runID)
		e.signalDispatch()
	}
	return err
}

func (e *Engine) publishRunControl(runID string) {
	var run models.Run
	if e.db.Select("id", "status", "pause_requested", "control_version").First(&run, "id = ?", runID).Error != nil {
		return
	}
	message, _ := json.Marshal(map[string]any{"type": "status", "runId": runID, "status": run.Status, "pauseRequested": run.PauseRequested, "controlVersion": run.ControlVersion})
	e.broker.Publish(runID, message)
}

// Waiting conversations stop admitting new turns as soon as pause_requested is
// saved. Their current FIFO drains before this observer acknowledges the pause.
func (e *Engine) parkWaitingPauses() {
	var runs []models.Run
	if e.db.Where("status = ? AND pause_requested = ?", "waiting_human", true).Limit(128).Find(&runs).Error != nil {
		return
	}
	for _, run := range runs {
		var conversations []models.ReactConversation
		if e.db.Select("node_id").Where("run_id = ? AND done = ?", run.ID, false).Find(&conversations).Error != nil {
			continue
		}
		busy := false
		for _, conversation := range conversations {
			waiting, thinking := e.ReviewSessionState(run.ID, conversation.NodeID)
			if waiting > 0 || thinking {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		err := e.db.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&models.Run{}).Where("id = ? AND status = ? AND pause_requested = ?", run.ID, "waiting_human", true).Updates(map[string]any{"status": "paused", "pause_from_status": "waiting_human", "pause_requested": false})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil
			}
			return services.EnqueueWorkflowEvent(tx, run.ID, "paused")
		})
		if err == nil {
			e.publishRunControl(run.ID)
		}
	}
}

// Called by the sole driver before entering the next node. The immutable next
// position and acknowledgement are persisted together; no global Halt is used.
func (e *Engine) parkRunBoundary(c *execCtx, next string) (bool, error) {
	var intent models.Run
	if err := e.db.Select("status", "pause_requested").First(&intent, "id = ?", c.run.ID).Error; err != nil {
		return false, err
	}
	if intent.Status == "paused" {
		return true, nil
	}
	if !intent.PauseRequested {
		return false, nil
	}
	e.setAdmissionTarget(c, next)
	parked := false
	err := e.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Run{}).Where("id = ? AND status = ? AND pause_requested = ?", c.run.ID, "running", true).
			Select("Status", "Checkpoints", "PauseRequested", "PauseFromStatus").Updates(&models.Run{Status: "paused", Checkpoints: c.run.Checkpoints, PauseFromStatus: "queued"})
		if result.Error != nil {
			return result.Error
		}
		parked = result.RowsAffected > 0
		if parked {
			return services.EnqueueWorkflowEvent(tx, c.run.ID, "paused")
		}
		return nil
	})
	if parked && err == nil {
		e.publishRunControl(c.run.ID)
	}
	return parked, err
}

func (e *Engine) checkRunNotPaused(runID string) error {
	var run models.Run
	if err := e.db.Select("status", "pause_requested").First(&run, "id = ?", runID).Error; err != nil {
		return err
	}
	if run.Status == "paused" || run.PauseRequested {
		return &services.AdmissionError{Code: "run_paused", Message: "工作流已请求暂停，请先恢复该任务", Status: 409}
	}
	return nil
}
