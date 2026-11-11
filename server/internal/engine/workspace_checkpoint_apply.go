package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/tokenledger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (e *Engine) applyWorkspaceCheckpoint(ctx context.Context, row models.WorkflowVisitCheckpoint, plan workspaceRoutePlan) error {
	graph, err := json.Marshal(plan.Graph)
	if err != nil || workflowSHA(graph) != row.GraphSHA256 {
		return services.ErrWorkspaceJobConflict
	}
	if row.FileState == "ready" {
		var run models.Run
		if err := e.db.WithContext(ctx).Select("id", "origin_solo_id", "workspace_actor_id").Take(&run, "id = ?", row.RunID).Error; err != nil {
			return err
		}
		actor, err := e.workspaceCheckpoints.Actor(ctx, run)
		if err != nil {
			return err
		}
		if actor != row.ActorID {
			return services.ErrWorkspaceJobAuthority
		}
	}
	plan.State.ID = row.StateRunID
	preparedState, err := e.prepareExecutionRow(ctx, &plan.State, false, []string{"Status", "OutputMd", "Outputs", "VarsSnapshot", "Events", "McpCalls", "Usage", "UsageByModel", "Error", "DurationSec"}, nil)
	if err != nil {
		return err
	}
	runChanges := models.Run{ID: row.RunID, Checkpoints: plan.Checkpoints, Trace: plan.Trace, Status: plan.Status, Attempt: plan.Attempt}
	preparedRun, err := e.prepareExecutionRow(ctx, &runChanges, false, []string{"Status", "Checkpoints", "Trace", "Attempt"}, nil)
	if err != nil {
		return err
	}
	var preparedVariables []*executionRow
	byName := map[string]workspaceCheckpointVariable{}
	for _, variable := range plan.Variables {
		if _, exists := byName[variable.Name]; exists {
			return services.ErrWorkspaceJobConflict
		}
		byName[variable.Name] = variable
	}
	for name, value := range plan.Changes {
		prior, exists := byName[name]
		variable := models.RunVariable{ID: prior.ID, RunID: row.RunID, Name: name, Type: prior.Type, Secret: prior.Secret, Value: value}
		columns := []string{"Value"}
		if !exists {
			variable.Type = inferType(value)
			columns = nil
		}
		prepared, err := e.prepareExecutionRow(ctx, &variable, !exists, columns, nil)
		if err != nil {
			return err
		}
		preparedVariables = append(preparedVariables, prepared)
	}
	err = e.executionTransaction(ctx, row.RunID, func(tx *gorm.DB) error {
		if err := e.checkpointPauseTx(tx, row); err != nil {
			return err
		}
		current, err := e.checkpointRun(tx.Statement.Context, tx, row.RunID, false)
		if err != nil {
			return err
		}
		if current.Graph != plan.Source.Graph || current.Checkpoints != plan.Source.Checkpoints {
			return services.ErrWorkspaceJobConflict
		}
		var state struct {
			RunID, NodeID, WorkspaceCheckpointID, Status string
			Iteration, Attempt, ProviderAttempt          int
		}
		if err := tx.Table("state_runs").Clauses(clause.Locking{Strength: "UPDATE"}).Select("run_id", "node_id", "workspace_checkpoint_id", "status", "iteration", "attempt", "provider_attempt").Where("id = ?", row.StateRunID).Take(&state).Error; err != nil {
			return err
		}
		if state.RunID != row.RunID || state.NodeID != row.NodeID || state.WorkspaceCheckpointID != row.ID || state.Iteration != row.Iteration || state.Attempt != row.RunAttempt || state.ProviderAttempt != row.ProviderAttempt || state.Status != "running" && state.Status != "waiting_human" {
			return services.ErrWorkspaceJobConflict
		}
		if err := e.workspaceCheckpoints.GuardRouteTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		var variables []struct {
			ID          uint
			Name, Value string
		}
		if err := tx.Table("run_variables").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name", "value").Where("run_id = ?", row.RunID).Find(&variables).Error; err != nil {
			return err
		}
		if len(variables) != len(plan.Variables) {
			return services.ErrWorkspaceJobConflict
		}
		for _, variable := range variables {
			prior, ok := byName[variable.Name]
			if !ok || prior.ID != variable.ID || prior.Raw != variable.Value {
				return services.ErrWorkspaceJobConflict
			}
		}
		if err := preparedState.write(tx, nil).Error; err != nil {
			return err
		}
		if err := tx.Table("state_runs").Where("id = ?", row.StateRunID).Update("provider_dispatch_state", "settled").Error; err != nil {
			return err
		}
		for _, prepared := range preparedVariables {
			if err := prepared.write(tx, nil).Error; err != nil {
				return err
			}
		}
		if err := preparedRun.write(tx, nil).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		updates := map[string]any{"pause_from_status": "", "last_activity_at": now}
		if plan.Status == "completed" {
			updates["progress"] = 1.0
		}
		if err := tx.Table("runs").Where("id = ?", row.RunID).Updates(updates).Error; err != nil {
			return err
		}
		if err := services.EnqueueWorkflowEvent(tx, row.RunID, plan.Status); err != nil {
			return err
		}
		result := tx.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ? AND route_state = ? AND outcome_ref = ? AND snapshot_id = ?", row.ID, "sealed", row.OutcomeRef, row.SnapshotID).Updates(map[string]any{"route_state": "applied", "reason": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return services.ErrWorkspaceJobConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !plan.UsageAlreadyRecorded {
		e.recordTokenUsage(tokenledger.Entry{ID: "workflow-checkpoint:" + row.ID, RunID: row.RunID, NodeID: row.NodeID, NodeType: plan.State.NodeType, Phase: models.TokenLedgerPhaseProduction, Status: tokenledger.StatusFromNode(plan.State.Status), Usage: plan.UsageDelta, ByModel: plan.UsageByModelDelta})
	}
	e.dialogueNodeEvent(row.RunID, row.NodeID)
	if plan.Status == "completed" || plan.Status == "failed" {
		e.afterRunFinish(row.RunID, plan.Status)
	} else {
		e.publishRunControl(row.RunID)
		e.signalDispatch()
	}
	return nil
}

func (e *Engine) workflowCheckpointPendingTx(tx *gorm.DB, runID string) error {
	if e.workspaceCheckpoints == nil {
		return nil
	}
	var row struct{ ID string }
	err := tx.Table("workflow_visit_checkpoints").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("run_id = ? AND "+services.WorkflowCheckpointOpenSQL("route_state"), runID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return services.ErrWorkflowCheckpointBlocked
}
