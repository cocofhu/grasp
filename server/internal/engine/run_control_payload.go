package engine

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// A failed boundary operation retains the exact control generation and byte
// snapshot it observed. The caller cannot requeue a newer execution using it.
type runBoundaryFailure struct {
	cause      error
	attempt    int
	control    uint64
	checkpoint []byte
	deadline   time.Time
}

func (e *runBoundaryFailure) Error() string { return e.cause.Error() }
func (e *runBoundaryFailure) Unwrap() error { return e.cause }

func (e *Engine) boundaryPauseWasWithdrawn(ctx context.Context, c *execCtx, next string, intent models.Run) bool {
	if !e.isExecOwner(c.run.ID, c.execGen) {
		return false
	}
	node, ok := e.admissionFromNode(*c.run)
	if !ok || node != next {
		return false
	}
	var current models.Run
	if err := e.db.WithContext(ctx).Select("status", "pause_requested", "pause_from_status", "attempt", "control_version").First(&current, "id = ?", c.run.ID).Error; err != nil {
		return false
	}
	// Only PauseRun/ResumePausedRun advance ControlVersion. While a pause is
	// pending, PauseRun is a no-op: precisely one advance to running/unpaused
	// with no pause origin therefore proves a withdrawal by ResumePausedRun.
	return current.Attempt == intent.Attempt && current.Status == "running" && !current.PauseRequested && current.PauseFromStatus == "" &&
		intent.ControlVersion != ^uint64(0) && current.ControlVersion == intent.ControlVersion+1 && e.isExecOwner(c.run.ID, c.execGen)
}

// This is deliberately scoped to the pause observer's error path. It does not
// call the legacy requeueRun loader/serializer or adopt a new control version.
func (e *Engine) requeueFailedRunBoundary(c *execCtx, next string, failure error) bool {
	var captured *runBoundaryFailure
	if errors.Is(failure, errRunBoundaryChanged) || !errors.As(failure, &captured) || len(captured.checkpoint) == 0 || !e.isExecOwner(c.run.ID, c.execGen) {
		return false
	}
	ctx, cancel := context.WithDeadline(context.Background(), captured.deadline)
	defer cancel()
	var current models.Run
	if err := e.db.WithContext(ctx).Select("status", "attempt", "control_version", "pause_requested").First(&current, "id = ?", c.run.ID).Error; err != nil ||
		current.Status != "running" || current.Attempt != captured.attempt || current.ControlVersion != captured.control || !current.PauseRequested {
		return false
	}
	files, err := filestore.FromDB(e.db)
	if err != nil {
		return false
	}
	prepared, err := files.Prepare(ctx, bytes.NewReader(captured.checkpoint), filestore.JSON)
	if err != nil {
		return false
	}
	queued := false
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
		result := tx.Table("runs").Where("id = ? AND status = ? AND attempt = ? AND control_version = ? AND pause_requested = ?", c.run.ID, "running", captured.attempt, captured.control, true).
			Updates(map[string]any{"status": "queued", "checkpoints": ref})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errRunBoundaryChanged
		}
		if err := services.EnqueueWorkflowEvent(tx, c.run.ID, "queued"); err != nil {
			return err
		}
		queued = true
		return nil
	})
	if err != nil || !queued {
		return false
	}
	e.publishRunControl(c.run.ID)
	log.Info().Str("run_id", c.run.ID).Str("node_id", next).Msg("run requeued from its original pause boundary")
	return true
}
