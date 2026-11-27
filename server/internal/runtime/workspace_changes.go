package runtime

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacechanges"
)

func workspaceChangeScope(req NodeReq, workspace string) workspacechanges.Scope {
	return workspacechanges.Scope{
		Kind: models.WorkspaceChangesRun, OwnerID: req.RunID, Workspace: workspace,
		NodeID: req.NodeID, Iteration: req.WorkspaceChangeIteration, Repos: req.Repos,
	}
}

type workspaceTurn struct {
	service *workspacechanges.Service
	scope   workspacechanges.Scope
	sb      *sandbox.Sandbox
	before  workspacechanges.Trees
}

func (c *acpProvider) trackWorkspaceTurn(ctx context.Context, req *NodeReq) func(bool) {
	if c.opts.WorkspaceChanges == nil || req.WorkspaceChangeTurnID == "" || len(req.Repos) == 0 {
		return func(bool) {}
	}
	turn := &workspaceTurn{service: c.opts.WorkspaceChanges, scope: workspaceChangeScope(*req, "")}
	req.workspaceTurn = turn
	id := req.WorkspaceChangeTurnID
	return func(interrupted bool) {
		turn.service.Finish(ctx, turn.sb, turn.scope, id, turn.before, interrupted)
	}
}

// Capture before a restored session replays history or contacts the model.
// A replacement workspace starts its own comparison.
func (t *workspaceTurn) capture(ctx context.Context, sb *sandbox.Sandbox) {
	if t == nil || sb == nil {
		return
	}
	t.sb = sb
	if t.scope.Workspace == sb.Name {
		return
	}
	t.scope.Workspace = sb.Name
	t.before = t.service.Begin(ctx, sb, t.scope)
}
