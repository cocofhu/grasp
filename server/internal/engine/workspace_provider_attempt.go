package engine

import (
	"context"
	"encoding/json"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/google/uuid"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (e *Engine) installProviderAttempt(c *execCtx, req *runtime.NodeReq) {
	if e.workspaceCheckpoints == nil {
		return
	}
	runID, nodeID, stateID, iteration, attempt := req.RunID, req.NodeID, req.StateRunID, req.Iteration, req.RunAttempt
	req.PrepareProviderAttempt = func(ctx context.Context) (sandbox.OperationIdentity, error) {
		operation := sandbox.OperationIdentity{ID: "g-" + uuid.NewString()}
		allocated := 0
		err := e.executionTransaction(ctx, runID, func(tx *gorm.DB) error {
			if stateID == 0 || c.execGen != 0 && !e.isExecOwner(runID, c.execGen) {
				return services.ErrWorkspaceJobConflict
			}
			run, err := e.checkpointRun(tx.Statement.Context, tx, runID, true)
			if err != nil {
				return err
			}
			if run.Attempt != attempt || run.Status != "running" && run.Status != "waiting_human" || run.PauseRequested {
				return services.ErrWorkflowCheckpointBlocked
			}
			if err := e.workflowCheckpointPendingTx(tx, runID); err != nil {
				return err
			}
			var state struct {
				RunID, NodeID, Status, ProviderDispatchState, WorkspaceCheckpointID string
				Iteration, Attempt, ProviderAttempt                                 int
			}
			if err := tx.Table("state_runs").Clauses(clause.Locking{Strength: "UPDATE"}).Select("run_id", "node_id", "status", "provider_dispatch_state", "workspace_checkpoint_id", "iteration", "attempt", "provider_attempt").Where("id = ?", stateID).Take(&state).Error; err != nil {
				return err
			}
			if state.RunID != runID || state.NodeID != nodeID || state.Iteration != iteration || state.Attempt != attempt || state.WorkspaceCheckpointID != "" || state.Status != "running" && state.Status != "waiting_human" {
				return services.ErrWorkspaceJobConflict
			}
			if state.ProviderDispatchState == "dispatched" && state.Status != "waiting_human" || state.ProviderDispatchState == "unknown" {
				return runtime.ErrProviderDispatchUnknown
			}
			allocated = state.ProviderAttempt + 1
			operation.Attempt = allocated
			return tx.Table("state_runs").Where("id = ?", stateID).Updates(map[string]any{"provider_attempt": allocated, "provider_dispatch_state": "preparing", "provider_dispatched_at": nil, "provider_operation_id": operation.ID, "provider_operation_receipt": ""}).Error
		})
		return operation, err
	}
	req.BeforeProviderDispatch = func(ctx context.Context, operation sandbox.OperationIdentity) error {
		if !operation.Valid() || c.execGen != 0 && !e.isExecOwner(runID, c.execGen) {
			return runtime.ErrProviderDispatchUnknown
		}
		providerAttempt := operation.Attempt
		return e.executionTransaction(ctx, runID, func(tx *gorm.DB) error {
			run, err := e.checkpointRun(tx.Statement.Context, tx, runID, true)
			if err != nil {
				return err
			}
			if run.Attempt != attempt || run.Status != "running" || run.PauseRequested {
				return services.ErrWorkflowCheckpointBlocked
			}
			if err := e.workflowCheckpointPendingTx(tx, runID); err != nil {
				return err
			}
			if err := services.GuardWorkflowNativeTx(tx.Statement.Context, tx, runID); err != nil {
				return err
			}
			var resources []struct {
				ID                         uint
				Name, Status, DesiredState string
				Generation                 uint64
			}
			if err := tx.Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name", "status", "desired_state", "generation").Where("name = ? AND purpose = ? AND run_id = ? AND node_id = ? AND state_run_id = ? AND iteration = ? AND run_attempt = ? AND provider_attempt = ?", operation.Resource, "run", runID, nodeID, stateID, iteration, attempt, providerAttempt).Limit(2).Find(&resources).Error; err != nil {
				return err
			}
			if len(resources) != 1 || resources[0].Status != "running" || resources[0].DesiredState == "stopped" {
				return runtime.ErrProviderDispatchUnknown
			}
			operation.SandboxID, operation.Generation = resources[0].ID, resources[0].Generation
			raw, err := json.Marshal(operation)
			if err != nil {
				return err
			}
			result := tx.Table("state_runs").Where("id = ? AND run_id = ? AND node_id = ? AND iteration = ? AND attempt = ? AND provider_attempt = ? AND provider_dispatch_state = ? AND (workspace_checkpoint_id = '' OR workspace_checkpoint_id IS NULL)", stateID, runID, nodeID, iteration, attempt, providerAttempt, "preparing").Where("provider_operation_id = ? AND status = ?", operation.ID, "running").Updates(map[string]any{"provider_dispatch_state": "dispatched", "provider_dispatched_at": time.Now().UTC(), "provider_operation_receipt": string(raw)})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return runtime.ErrProviderDispatchUnknown
			}
			return nil
		})
	}
}

// Startup retains visits without a verifiable native operation receipt. The
// original disk can be inspected; no prompt is automatically replayed.
func (e *Engine) parkUnknownWorkspaceVisit(sr models.StateRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lineage, err := e.execution.Current(ctx)
	if err != nil {
		return err
	}
	return e.executionTransaction(ctx, sr.RunID, func(tx *gorm.DB) error {
		run, err := e.checkpointRun(tx.Statement.Context, tx, sr.RunID, true)
		if err != nil {
			return err
		}
		if run.Status != "running" && run.Status != "waiting_human" || run.Attempt != sr.Attempt {
			return services.ErrWorkspaceJobConflict
		}
		if sr.WorkspaceCheckpointID != "" {
			return nil
		}
		id := services.WorkflowCheckpointID(sr.RunID, sr.ID, run.ControlVersion)
		row := models.WorkflowVisitCheckpoint{ID: id, RunID: sr.RunID, StateRunID: sr.ID, NodeID: sr.NodeID, Iteration: sr.Iteration, RunAttempt: sr.Attempt, ProviderAttempt: sr.ProviderAttempt, ControlVersion: run.ControlVersion, Lineage: lineage, FileState: "unknown", RouteState: "unknown", Reason: "provider_outcome_unconfirmed"}
		var sources []struct {
			ID   uint
			Name string
		}
		if err := tx.Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name").Where("purpose = ? AND run_id = ? AND node_id = ? AND state_run_id = ? AND iteration = ? AND run_attempt = ? AND provider_attempt = ?", "run", sr.RunID, sr.NodeID, sr.ID, sr.Iteration, sr.Attempt, sr.ProviderAttempt).Limit(2).Find(&sources).Error; err != nil {
			return err
		}
		if len(sources) == 1 {
			row.SandboxID, row.Resource = sources[0].ID, sources[0].Name
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Table("state_runs").Where("id = ?", sr.ID).Updates(map[string]any{"workspace_checkpoint_id": id, "provider_dispatch_state": "unknown", "error": "执行结果尚未确认；保留原工作区，未自动重跑 Agent"}).Error; err != nil {
			return err
		}
		if err := tx.Table("runs").Where("id = ?", sr.RunID).Updates(map[string]any{"status": "paused", "pause_from_status": workspaceCheckpointPause + id}).Error; err != nil {
			return err
		}
		return services.EnqueueWorkflowEvent(tx, sr.RunID, "paused")
	})
}
