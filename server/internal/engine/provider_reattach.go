package engine

import (
	"context"
	"encoding/json"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"strconv"
)

// A live handle is not provenance. Strict recovery uses the exact write-ahead
// operation and the resource of that provider attempt, never the newest row.
func (e *Engine) exactReattachSandbox(run *models.Run, state models.StateRun) (string, bool) {
	if run.Attempt != state.Attempt || state.IsManual() || state.WorkspaceCheckpointID != "" || state.ProviderDispatchState != "dispatched" || state.ProviderOperationID == "" {
		return "", false
	}
	node := run.Graph.FindNode(state.NodeID)
	if node == nil || node.Caps == nil || node.Caps.Clarify() {
		return "", false
	}
	var op sandbox.OperationIdentity
	if json.Unmarshal([]byte(state.ProviderOperationReceipt), &op) != nil || !op.Valid() || op.ID != state.ProviderOperationID || op.Attempt != state.ProviderAttempt || op.SandboxID == 0 {
		return "", false
	}
	var count int64
	if err := e.db.Table("state_runs").Where("run_id = ? AND node_id = ? AND iteration = ? AND attempt = ?", state.RunID, state.NodeID, state.Iteration, state.Attempt).Count(&count).Error; err != nil || count != 1 {
		return "", false
	}
	err := e.db.Table("sandboxes").Where("id = ? AND name = ? AND purpose = ? AND run_id = ? AND node_id = ? AND state_run_id = ? AND iteration = ? AND run_attempt = ? AND provider_attempt = ? AND generation = ? AND status = ? AND COALESCE(desired_state,'') <> ?", op.SandboxID, op.Resource, "run", state.RunID, state.NodeID, state.ID, state.Iteration, state.Attempt, state.ProviderAttempt, op.Generation, "running", "stopped").Count(&count).Error
	if err != nil || count != 1 {
		return "", false
	}
	if e.executionCheck(context.Background(), state.RunID, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(op.SandboxID), 10)}) != nil {
		return "", false
	}
	return op.Resource, true
}

func (e *Engine) reserveReattach(state models.StateRun, resource string) {
	e.reattachMu.Lock()
	e.pendingReattach = append(e.pendingReattach, reattachInfo{runID: state.RunID, nodeID: state.NodeID, iteration: state.Iteration, sandbox: resource})
	e.adoptedSandboxes[resource] = true
	e.reattachMu.Unlock()
	e.sem.ForceAcquire()
}
