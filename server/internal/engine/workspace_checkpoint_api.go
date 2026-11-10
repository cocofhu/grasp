package engine

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

// WorkspaceCheckpoints is historical metadata. The service authorizes the
// current read scope, never an old model credential or active binding.
func (e *Engine) WorkspaceCheckpoints(ctx context.Context, actor, runID string) ([]models.WorkflowVisitCheckpoint, error) {
	if e.workspaceCheckpoints == nil {
		return nil, services.ErrWorkspaceJobUnsupported
	}
	return e.workspaceCheckpoints.List(ctx, actor, runID)
}

// ReconcileWorkspaceCheckpoint is an explicit read of the original capture
// receipt. A completed upload can resume its immutable route, never its Agent.
func (e *Engine) ReconcileWorkspaceCheckpoint(ctx context.Context, actor, runID, id string) (models.WorkflowVisitCheckpoint, error) {
	if e.workspaceCheckpoints == nil {
		return models.WorkflowVisitCheckpoint{}, services.ErrWorkspaceJobUnsupported
	}
	row, err := e.workspaceCheckpoints.Read(ctx, actor, runID, id)
	if err != nil {
		return row, err
	}
	row, err = e.workspaceCheckpoints.Reconcile(ctx, actor, row.ID)
	if err == nil {
		e.signalDispatch()
	}
	return row, err
}
