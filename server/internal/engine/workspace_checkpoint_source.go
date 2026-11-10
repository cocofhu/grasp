package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

// Explicit projections close their SQL cursor before any referenced body is
// opened. A blocked checkpoint PUT/GET cannot monopolize the SQL connection.
type checkpointStateSource struct {
	ID                                                                                                                                                      uint
	RunID, NodeID, NodeType, Status, WorkspaceCheckpointID, OutputMd, Outputs, VarsBefore, VarsSnapshot, Events, McpCalls, Usage, UsageByModel, Error, Mode string
	Iteration, Attempt, ProviderAttempt, DurationSec                                                                                                        int
	StartedAt, SandboxReadyAt                                                                                                                               *time.Time
}

func (e *Engine) checkpointState(ctx context.Context, c *execCtx, node *models.Node, oc nodeOutcome) (models.StateRun, error) {
	var rows []checkpointStateSource
	err := e.db.WithContext(ctx).Table("state_runs").Select("id", "run_id", "node_id", "node_type", "status", "workspace_checkpoint_id", "output_md", "outputs", "vars_before", "vars_snapshot", "events", "mcp_calls", "usage", "usage_by_model", "error", "mode", "iteration", "attempt", "provider_attempt", "duration_sec", "started_at", "sandbox_ready_at").Where("run_id = ? AND node_id = ? AND iteration = ? AND attempt = ?", c.run.ID, node.ID, c.iter[node.ID], c.run.Attempt).Limit(2).Find(&rows).Error
	if err != nil {
		return models.StateRun{}, err
	}
	if len(rows) != 1 {
		return models.StateRun{}, services.ErrWorkspaceJobConflict
	}
	raw := rows[0]
	if raw.WorkspaceCheckpointID != "" {
		return models.StateRun{}, services.ErrWorkflowCheckpointBlocked
	}
	if raw.Status != "running" && raw.Status != "waiting_human" {
		return models.StateRun{}, services.ErrWorkspaceJobConflict
	}
	files, err := filestore.FromDB(e.db)
	if err != nil {
		return models.StateRun{}, err
	}
	state := models.StateRun{ID: raw.ID, RunID: raw.RunID, NodeID: raw.NodeID, NodeType: raw.NodeType, Iteration: raw.Iteration, Attempt: raw.Attempt, ProviderAttempt: raw.ProviderAttempt, Status: oc.status, StartedAt: raw.StartedAt, SandboxReadyAt: raw.SandboxReadyAt, OutputMd: oc.outputMd, Error: oc.err, Mode: raw.Mode, Outputs: oc.outputs}
	for _, part := range []struct {
		raw      string
		target   any
		external bool
	}{{raw.VarsBefore, &state.VarsBefore, true}, {raw.McpCalls, &state.McpCalls, true}, {raw.Usage, &state.Usage, false}, {raw.UsageByModel, &state.UsageByModel, false}} {
		body := []byte(part.raw)
		if part.external {
			body, err = files.LoadBounded(ctx, part.raw, filestore.JSON, services.WorkflowCheckpointOutcomeLimit)
			if err != nil {
				return state, err
			}
		}
		if len(body) > 0 && string(body) != "null" {
			if err := json.Unmarshal(body, part.target); err != nil {
				return state, err
			}
		}
	}
	if state.Outputs == nil && raw.Outputs != "" {
		body, err := files.LoadBounded(ctx, raw.Outputs, filestore.JSON, services.WorkflowCheckpointOutcomeLimit)
		if err != nil {
			return state, err
		}
		if len(body) > 0 && string(body) != "null" {
			if err := json.Unmarshal(body, &state.Outputs); err != nil {
				return state, err
			}
		}
	}
	if len(oc.events) > 0 {
		state.Events = oc.events
	} else if raw.Events != "" {
		body, err := files.LoadBounded(ctx, raw.Events, filestore.JSON, services.WorkflowCheckpointOutcomeLimit)
		if err != nil {
			return state, err
		}
		if len(body) > 0 && string(body) != "null" {
			if err := json.Unmarshal(body, &state.Events); err != nil {
				return state, err
			}
		}
	}
	state.McpCalls = append(state.McpCalls, e.host.TakeMcpCalls(c.run.ID, node.ID)...)
	state.Usage = models.AddTokenUsage(state.Usage, oc.usage)
	state.UsageByModel = models.AddTokenUsageByModel(state.UsageByModel, oc.usageByModel)
	if state.StartedAt != nil {
		state.DurationSec = max(0, int(time.Since(*state.StartedAt).Seconds()))
	}
	return state, nil
}

func (e *Engine) checkpointVariables(ctx context.Context, c *execCtx) ([]workspaceCheckpointVariable, error) {
	var rows []struct {
		ID                uint
		Name, Type, Value string
		Secret            bool
	}
	if err := e.db.WithContext(ctx).Table("run_variables").Select("id", "name", "type", "value", "secret").Where("run_id = ?", c.run.ID).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	files, err := filestore.FromDB(e.db)
	if err != nil {
		return nil, err
	}
	var variables []workspaceCheckpointVariable
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Name] {
			return nil, services.ErrWorkspaceJobConflict
		}
		seen[row.Name] = true
		body, err := files.LoadBounded(ctx, row.Value, filestore.JSON, services.WorkflowCheckpointOutcomeLimit)
		if err != nil {
			return nil, err
		}
		var value any
		if len(body) > 0 && string(body) != "null" {
			if err := json.Unmarshal(body, &value); err != nil {
				return nil, err
			}
		}
		current, _ := json.Marshal(value)
		expected, marshalErr := json.Marshal(c.vars[row.Name])
		if marshalErr != nil || string(current) != string(expected) {
			return nil, fmt.Errorf("workflow variable %q changed before route sealing", row.Name)
		}
		variables = append(variables, workspaceCheckpointVariable{ID: row.ID, Name: row.Name, Type: row.Type, Secret: row.Secret, Value: value, Raw: row.Value})
	}
	return variables, nil
}

func (e *Engine) checkpointSource(ctx context.Context, state models.StateRun) (models.Sandbox, string, error) {
	if !e.workspaceCheckpoints.Enabled() {
		return models.Sandbox{}, "skipped", nil
	}
	var sources []models.Sandbox
	if err := e.db.WithContext(ctx).Select("id", "name", "purpose", "run_id", "node_id", "state_run_id", "iteration", "run_attempt", "provider_attempt", "status").Where("purpose = ? AND run_id = ? AND node_id = ? AND state_run_id = ? AND iteration = ? AND run_attempt = ? AND provider_attempt = ?", "run", state.RunID, state.NodeID, state.ID, state.Iteration, state.Attempt, state.ProviderAttempt).Limit(2).Find(&sources).Error; err != nil {
		return models.Sandbox{}, "blocked", err
	}
	if len(sources) == 1 && state.ProviderAttempt > 0 && sources[0].Name != "" && sources[0].Status == "running" {
		return sources[0], "pending", nil
	}
	if len(sources) == 0 && state.SandboxReadyAt == nil {
		return models.Sandbox{}, "not_created", nil
	}
	if len(sources) > 0 {
		return models.Sandbox{}, "blocked", services.ErrWorkspaceJobConflict
	}
	return models.Sandbox{}, "unavailable", errors.New("the original visit's workspace source is unavailable")
}
