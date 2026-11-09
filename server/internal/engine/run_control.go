package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errRunBoundaryChanged = errors.New("workflow pause boundary changed while preparing its checkpoint")

func (e *Engine) PauseRun(runID string, version uint64) error {
	err := e.executionTransaction(context.Background(), runID, func(tx *gorm.DB) error {
		var run models.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status", "pause_requested", "control_version").First(&run, "id = ?", runID).Error; err != nil {
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
	err := e.executionTransaction(context.Background(), runID, func(tx *gorm.DB) error {
		var run models.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status", "pause_requested", "pause_from_status", "control_version").First(&run, "id = ?", runID).Error; err != nil {
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
	if e.db.Select("id").Where("status = ? AND pause_requested = ?", "waiting_human", true).Limit(128).Find(&runs).Error != nil {
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
		err := e.executionTransaction(context.Background(), run.ID, func(tx *gorm.DB) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, inside := e.db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return false, errors.New("checkpoint preparation requires an outer transaction-free storage handle")
	}
	var intent models.Run
	if err := e.db.WithContext(ctx).Select("status", "pause_requested", "attempt", "control_version").First(&intent, "id = ?", c.run.ID).Error; err != nil {
		return false, err
	}
	if intent.Status == "paused" {
		return true, nil
	}
	if !intent.PauseRequested {
		return false, nil
	}
	if intent.Attempt != c.run.Attempt {
		return false, errRunBoundaryChanged
	}
	e.setAdmissionTarget(c, next)
	// Marshal a private immutable byte snapshot before publication. Preparing
	// the checkpoint must never hold the Run row or an outer SQL transaction.
	checkpoint, err := json.Marshal(c.run.Checkpoints)
	if err != nil {
		return false, err
	}
	files, err := filestore.FromDB(e.db)
	if err != nil {
		return false, err
	}
	prepared, err := files.Prepare(ctx, bytes.NewReader(checkpoint), filestore.JSON)
	if err != nil {
		deadline, _ := ctx.Deadline()
		return false, &runBoundaryFailure{cause: err, attempt: intent.Attempt, control: intent.ControlVersion, checkpoint: checkpoint, deadline: deadline}
	}
	parked := false
	err = e.executionTransaction(ctx, c.run.ID, func(tx *gorm.DB) error {
		if !e.isExecOwner(c.run.ID, c.execGen) {
			return errRunBoundaryChanged
		}
		store, err := filestore.FromDB(tx)
		if err != nil {
			return err
		}
		ref, err := store.PreparedRef(prepared)
		if err != nil {
			return err
		}
		result := tx.Table("runs").Where("id = ? AND status = ? AND pause_requested = ? AND attempt = ? AND control_version = ?", c.run.ID, "running", true, intent.Attempt, intent.ControlVersion).
			Updates(map[string]any{"status": "paused", "checkpoints": ref, "pause_requested": false, "pause_from_status": "queued"})
		if result.Error != nil {
			return result.Error
		}
		parked = result.RowsAffected > 0
		if parked {
			return services.EnqueueWorkflowEvent(tx, c.run.ID, "paused")
		}
		return errRunBoundaryChanged
	})
	if errors.Is(err, errRunBoundaryChanged) && e.boundaryPauseWasWithdrawn(ctx, c, next, intent) {
		return false, nil // the same live driver may continue after a proven withdrawal
	}
	if err != nil && !errors.Is(err, errRunBoundaryChanged) {
		deadline, _ := ctx.Deadline()
		err = &runBoundaryFailure{cause: err, attempt: intent.Attempt, control: intent.ControlVersion, checkpoint: checkpoint, deadline: deadline}
	}
	if parked && err == nil {
		e.publishRunControl(c.run.ID)
	}
	return parked, err
}

func (e *Engine) checkRunNotPaused(runID string) error {
	if err := e.executionCheck(context.Background(), runID); err != nil {
		return err
	}
	var run models.Run
	if err := e.db.Select("status", "pause_requested").First(&run, "id = ?", runID).Error; err != nil {
		return err
	}
	if run.Status == "paused" || run.PauseRequested {
		return &services.AdmissionError{Code: "run_paused", Message: "工作流已请求暂停，请先恢复该任务", Status: 409}
	}
	return nil
}
