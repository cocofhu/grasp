package services

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GuardWorkflowNativeTx precedes an Agent/native setup, including shared-root
// re-prime and branch checkout. It is pure SQL, after the Run parent and before
// any Sandbox mutation. Capturing the checkpoint uses the separate file-job
// protocol and cannot mint another Agent admission through this read.
func GuardWorkflowNativeTx(ctx context.Context, tx *gorm.DB, runID string) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceJobInvalid
	}
	var parent struct{ ID string }
	if err := tx.WithContext(ctx).Table("runs").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Take(&parent, "id = ?", runID).Error; err != nil {
		return err
	}
	var pending struct{ ID string }
	err := tx.Table("workflow_visit_checkpoints").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("run_id = ? AND route_state <> ?", runID, "applied").Take(&pending).Error
	if err == nil {
		return ErrWorkflowCheckpointBlocked
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var resources []struct{ ID uint }
	if err := tx.Table("sandboxes").Select("id").Where("purpose = ? AND run_id = ?", "run", runID).Order("id").Find(&resources).Error; err != nil {
		return err
	}
	for _, resource := range resources {
		if err := GuardWorkspaceBindingTx(tx, resource.ID); err != nil {
			return err
		}
		if err := GuardWorkflowWorkspaceReuseTx(ctx, tx, resource.ID); err != nil {
			return err
		}
	}
	return nil
}
