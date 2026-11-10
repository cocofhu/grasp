package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const workspaceCheckpointPause = "workspace_checkpoint:"

// Raw payload identities are compared under SQL locks, never hydrated there.
// The logical graph and outcome are independently included in OutcomeRef.
type checkpointRunHeader struct {
	Status, PauseFromStatus, Graph, Checkpoints, Trace string
	Attempt                                            int
	ControlVersion                                     uint64
	PauseRequested                                     bool
}

func (e *Engine) checkpointRun(ctx context.Context, db *gorm.DB, id string, lock bool) (checkpointRunHeader, error) {
	var row checkpointRunHeader
	query := db.WithContext(ctx).Table("runs")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Select("status", "pause_from_status", "graph", "checkpoints", "trace", "attempt", "control_version", "pause_requested").Take(&row, "id = ?", id).Error
	return row, err
}

func workflowSHA(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (e *Engine) finishWorkspaceCheckpoint(c *execCtx, node *models.Node, oc nodeOutcome, opts finishOpts) finishResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := e.sealWorkspaceCheckpoint(ctx, c, node, oc, opts)
	if err != nil {
		log.Error().Err(err).Str("run_id", c.run.ID).Str("node_id", node.ID).Msg("workflow outcome retained at workspace checkpoint; Agent must not be replayed")
		e.retainFailedCheckpoint(c, node)
	}
	// The ordinary dispatcher is the only continuation scheduler. Even a ready
	// capture yields this driver before the next visit can change the disk.
	e.signalDispatch()
	return finishResult{ended: true}
}

func (e *Engine) retainFailedCheckpoint(c *execCtx, node *models.Node) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var state models.StateRun
	if err := e.db.WithContext(ctx).Select("id", "run_id", "node_id", "node_type", "iteration", "attempt", "provider_attempt", "workspace_checkpoint_id").Where("run_id = ? AND node_id = ? AND iteration = ? AND attempt = ?", c.run.ID, node.ID, c.iter[node.ID], c.run.Attempt).Take(&state).Error; err != nil {
		return
	}
	if state.WorkspaceCheckpointID == "" {
		_ = e.parkUnknownWorkspaceVisit(state)
		e.publishRunControl(c.run.ID)
		return
	}
	_ = e.executionTransaction(ctx, c.run.ID, func(tx *gorm.DB) error {
		return tx.Table("workflow_visit_checkpoints").Where("id = ? AND route_state = ? AND (outcome_ref = '' OR outcome_ref IS NULL)", state.WorkspaceCheckpointID, "sealing").Updates(map[string]any{"route_state": "blocked", "reason": "outcome_sealing_failed"}).Error
	})
}

func (e *Engine) sealWorkspaceCheckpoint(ctx context.Context, c *execCtx, node *models.Node, oc nodeOutcome, opts finishOpts) error {
	state, err := e.checkpointState(ctx, c, node, oc)
	if err != nil {
		return err
	}
	variables, err := e.checkpointVariables(ctx, c)
	if err != nil {
		return err
	}
	source, err := e.checkpointRun(ctx, e.db, c.run.ID, false)
	if err != nil {
		return err
	}
	if source.Attempt != c.run.Attempt || source.Status != "running" && source.Status != "waiting_human" {
		return services.ErrWorkspaceJobConflict
	}
	graph, err := json.Marshal(c.graph)
	if err != nil {
		return err
	}
	var persisted models.Graph
	if json.Unmarshal([]byte(source.Graph), &persisted) != nil {
		return services.ErrWorkspaceJobConflict
	}
	logicalGraph, err := json.Marshal(persisted)
	if err != nil || workflowSHA(logicalGraph) != workflowSHA(graph) {
		return services.ErrWorkspaceJobConflict
	}
	plan, err := e.planWorkspaceRoute(c, node, oc, opts, state, variables)
	if err != nil {
		return err
	}
	plan.Source = source
	if opts.trigger == triggerConfirm {
		if cp := plan.Checkpoints[services.PendingAutoCheckpoint]; cp != nil && str(cp["node"]) != plan.Next {
			delete(plan.Checkpoints, services.PendingAutoCheckpoint)
		}
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	lineage, err := e.execution.Current(ctx)
	if err != nil {
		return err
	}
	resource, fileState, sourceErr := e.checkpointSource(ctx, state)
	actor, actorErr := e.workspaceCheckpoints.Actor(ctx, *c.run)
	reason := ""
	if sourceErr != nil {
		reason = "workspace_source_unavailable"
	}
	if fileState == "pending" && actorErr != nil {
		fileState, reason = "blocked", "workspace_actor_unavailable"
	}
	if fileState == "skipped" {
		reason = "workspace_operations_disabled"
	}
	row := models.WorkflowVisitCheckpoint{ID: services.WorkflowCheckpointID(c.run.ID, state.ID, source.ControlVersion), RunID: c.run.ID, StateRunID: state.ID, NodeID: node.ID, Iteration: state.Iteration, RunAttempt: state.Attempt, ProviderAttempt: state.ProviderAttempt, SandboxID: resource.ID, Resource: resource.Name, Lineage: lineage, ActorID: actor, ControlVersion: source.ControlVersion, OriginalStatus: source.Status, GraphSHA256: workflowSHA(graph), OutcomeSHA256: workflowSHA(body), FileState: fileState, RouteState: "sealing", Reason: reason, CaptureDurationMS: e.workspaceCheckpoints.CaptureDuration().Milliseconds()}
	err = e.executionTransaction(ctx, c.run.ID, func(tx *gorm.DB) error {
		current, err := e.checkpointRun(tx.Statement.Context, tx, c.run.ID, true)
		if err != nil {
			return err
		}
		if current != source || c.execGen != 0 && !e.isExecOwner(c.run.ID, c.execGen) {
			return services.ErrWorkspaceJobConflict
		}
		var sr struct {
			ID                                           uint
			RunID, NodeID, Status, WorkspaceCheckpointID string
			Iteration, Attempt, ProviderAttempt          int
		}
		if err := tx.Table("state_runs").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "run_id", "node_id", "status", "workspace_checkpoint_id", "iteration", "attempt", "provider_attempt").Take(&sr, state.ID).Error; err != nil {
			return err
		}
		if sr.RunID != row.RunID || sr.NodeID != row.NodeID || sr.Iteration != row.Iteration || sr.Attempt != row.RunAttempt || sr.ProviderAttempt != row.ProviderAttempt || sr.WorkspaceCheckpointID != "" || sr.Status != "running" && sr.Status != "waiting_human" {
			return services.ErrWorkspaceJobConflict
		}
		if row.SandboxID != 0 {
			if err := e.validateCheckpointSandbox(tx, row); err != nil {
				return err
			}
		}
		if err := e.workspaceCheckpoints.ReserveTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		if err := tx.Table("state_runs").Where("id = ? AND (workspace_checkpoint_id = '' OR workspace_checkpoint_id IS NULL)", state.ID).Update("workspace_checkpoint_id", row.ID).Error; err != nil {
			return err
		}
		trace := append([]models.TraceEntry{}, c.run.Trace...)
		trace = append(trace, models.TraceEntry{NodeID: node.ID, Event: "pause", Detail: "正在保存本次节点的工作区版本", At: time.Now().UTC().Format(time.RFC3339)})
		traceJSON, err := json.Marshal(trace)
		if err != nil {
			return err
		}
		if err := tx.Table("runs").Where("id = ?", row.RunID).Updates(map[string]any{"status": "paused", "pause_from_status": workspaceCheckpointPause + row.ID, "trace": string(traceJSON)}).Error; err != nil {
			return err
		}
		return services.EnqueueWorkflowEvent(tx, row.RunID, "paused")
	})
	if err != nil {
		return err
	}
	e.publishRunControl(row.RunID)
	prepared, err := e.workspaceCheckpoints.PrepareOutcome(ctx, row, body)
	if err != nil {
		return err
	}
	err = e.executionTransaction(ctx, row.RunID, func(tx *gorm.DB) error {
		if err := e.checkpointPauseTx(tx, row); err != nil {
			return err
		}
		return e.workspaceCheckpoints.PublishOutcomeTx(tx.Statement.Context, tx, row, prepared)
	})
	if err != nil {
		return err
	}
	row, err = e.workspaceCheckpoints.Get(ctx, row.ID)
	if err != nil {
		return err
	}
	if row.FileState == "pending" {
		return e.workspaceCheckpoints.RequestCapture(ctx, row)
	}
	return nil
}

func (e *Engine) validateCheckpointSandbox(tx *gorm.DB, row models.WorkflowVisitCheckpoint) error {
	var source struct {
		ID, StateRunID                         uint
		Name, RunID, NodeID                    string
		Iteration, RunAttempt, ProviderAttempt int
	}
	if err := tx.Table("sandboxes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "state_run_id", "name", "run_id", "node_id", "iteration", "run_attempt", "provider_attempt").Take(&source, row.SandboxID).Error; err != nil {
		return err
	}
	if source.Name != row.Resource || source.RunID != row.RunID || source.NodeID != row.NodeID || source.StateRunID != row.StateRunID || source.Iteration != row.Iteration || source.RunAttempt != row.RunAttempt || source.ProviderAttempt != row.ProviderAttempt {
		return services.ErrWorkspaceJobConflict
	}
	return nil
}

func (e *Engine) checkpointPauseTx(tx *gorm.DB, row models.WorkflowVisitCheckpoint) error {
	current, err := e.checkpointRun(tx.Statement.Context, tx, row.RunID, true)
	if err != nil {
		return err
	}
	if current.Status != "paused" || current.PauseFromStatus != workspaceCheckpointPause+row.ID || current.Attempt != row.RunAttempt || current.ControlVersion != row.ControlVersion {
		return services.ErrWorkspaceJobConflict
	}
	return nil
}

// Called by the one engine dispatcher, without any SQL transaction or cursor.
// Recovery only hydrates the sealed decision; neither Agent nor git/handoff
// are run again. Unknown captures require explicit original-job reconciliation.
func (e *Engine) reconcileWorkspaceCheckpoints() {
	if e.workspaceCheckpoints == nil {
		return
	}
	e.haltMu.RLock()
	if e.halted {
		e.haltMu.RUnlock()
		return
	}
	e.checkpointActive.Add(1)
	e.haltMu.RUnlock()
	defer e.checkpointActive.Add(-1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	now := time.Now().UTC()
	var rows []models.WorkflowVisitCheckpoint
	if err := e.db.WithContext(ctx).Where("route_state = ? AND file_state IN ?", "sealed", []string{"pending", "ready", "skipped", "not_created"}).Where("next_probe_at IS NULL OR next_probe_at <= ?", now).Order("COALESCE(next_probe_at, created_at),id").Limit(8).Find(&rows).Error; err != nil {
		return
	}
	for _, row := range rows {
		if e.IsHalted() || ctx.Err() != nil {
			return
		}
		var claimed *gorm.DB
		claimErr := e.executionTransaction(ctx, row.RunID, func(tx *gorm.DB) error {
			if err := e.workspaceCheckpoints.GuardTx(tx.Statement.Context, tx, row); err != nil {
				return err
			}
			claimed = tx.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ? AND route_state = ? AND (next_probe_at IS NULL OR next_probe_at <= ?)", row.ID, "sealed", now).Update("next_probe_at", now.Add(dispatchPoll))
			return claimed.Error
		})
		if claimErr != nil || claimed == nil || claimed.RowsAffected != 1 {
			continue
		}
		err := e.advanceWorkspaceCheckpoint(ctx, row)
		if err != nil && !errors.Is(err, services.ErrWorkflowCheckpointBlocked) && !errors.Is(err, services.ErrWorkspaceJobConflict) {
			log.Warn().Err(err).Str("checkpoint_id", row.ID).Msg("workflow workspace checkpoint remains paused")
		}
	}
}

func (e *Engine) advanceWorkspaceCheckpoint(ctx context.Context, row models.WorkflowVisitCheckpoint) error {
	if row.FileState == "pending" && row.CaptureStartedAt == nil {
		if err := e.workspaceCheckpoints.RequestCapture(ctx, row); err != nil {
			return err
		}
		var err error
		row, err = e.workspaceCheckpoints.Get(ctx, row.ID)
		if err != nil {
			return err
		}
	}
	ready, err := e.workspaceCheckpoints.FilesReady(ctx, row)
	if err != nil || !ready {
		return err
	}
	row, err = e.workspaceCheckpoints.Get(ctx, row.ID)
	if err != nil {
		return err
	}
	body, err := e.workspaceCheckpoints.LoadOutcome(ctx, row)
	if err != nil {
		return err
	}
	var plan workspaceRoutePlan
	if err := json.Unmarshal(body, &plan); err != nil {
		return err
	}
	if plan.Version != 1 || plan.State.RunID != row.RunID || plan.State.NodeID != row.NodeID || plan.State.Iteration != row.Iteration || plan.State.Attempt != row.RunAttempt || plan.State.ProviderAttempt != row.ProviderAttempt {
		return fmt.Errorf("invalid sealed workflow checkpoint: %w", services.ErrWorkspaceJobConflict)
	}
	return e.applyWorkspaceCheckpoint(ctx, row, plan)
}
