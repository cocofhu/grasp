package services

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacechanges"
	"gorm.io/gorm"
)

func SoloWorkspaceChangeScope(row *models.SoloSession) workspacechanges.Scope {
	return workspacechanges.Scope{
		Kind: models.WorkspaceChangesSolo, OwnerID: row.ID, Generation: row.Restarts,
		Repos: effectiveSessionRepos(row),
	}
}

func (s *SandboxService) SetWorkspaceChanges(changes *workspacechanges.Service) {
	s.changes = changes
}

func (s *SandboxService) initializeSoloWorkspaceChanges(ctx context.Context, sb *sandbox.Sandbox, sessionID string) {
	if s.changes == nil {
		return
	}
	var row models.SoloSession
	if s.db.WithContext(ctx).Where("id = ?", sessionID).Take(&row).Error == nil {
		scope := SoloWorkspaceChangeScope(&row)
		scope.Workspace = sb.Name
		s.changes.Initialize(ctx, sb, scope)
	}
}

func (s *SoloService) beginWorkspaceChanges(ctx context.Context, prep soloTurnPrep, turnID string) func(bool) {
	if s.sbx == nil || s.sbx.changes == nil {
		return func(bool) {}
	}
	scope := SoloWorkspaceChangeScope(prep.row)
	if len(scope.Repos) == 0 {
		return func(bool) {}
	}
	sb, err := s.sbx.soloHandle(ctx, prep.sandboxID)
	if err != nil {
		return func(bool) {}
	}
	scope.Workspace = sb.Name
	before := s.sbx.changes.Begin(ctx, sb, scope)
	return func(interrupted bool) {
		s.sbx.changes.Finish(ctx, sb, scope, turnID, before, interrupted)
	}
}

func (s *SandboxService) changesScope(ctx context.Context, row *models.Sandbox) workspacechanges.Scope {
	switch row.Purpose {
	case SandboxPurposeSolo:
		var session models.SoloSession
		if s.db.WithContext(ctx).Where("id = ? AND sandbox_id = ?", row.ThreadID, row.ID).Take(&session).Error == nil {
			scope := SoloWorkspaceChangeScope(&session)
			scope.Workspace = row.Name
			return scope
		}
	case "run":
		var run models.Run
		if s.db.WithContext(ctx).Select("id", "repos").Where("id = ?", row.RunID).Take(&run).Error == nil {
			return workspacechanges.Scope{Kind: models.WorkspaceChangesRun, OwnerID: row.RunID, Workspace: row.Name, NodeID: row.NodeID, Repos: run.Repos}
		}
	}
	return workspacechanges.Scope{}
}

func (s *SandboxService) archiveWorkspaceChanges(ctx context.Context, row *models.Sandbox, sb *sandbox.Sandbox) {
	if s.changes == nil || row.Status != "running" {
		return
	}
	scope := s.changesScope(ctx, row)
	if len(scope.Repos) == 0 {
		return
	}
	if sb == nil {
		var err error
		sb, err = s.soloHandle(ctx, row.ID)
		if err != nil {
			return
		}
	}
	s.changes.Finish(ctx, sb, scope, "", nil, false)
}

// WorkspaceForChanges resolves only workspaces owned by the requested run or
// Solo session. Looking up or refreshing a snapshot never wakes a sandbox.
func (s *SandboxService) WorkspaceForChanges(ctx context.Context, scope workspacechanges.Scope, workspace string) (*models.Sandbox, error) {
	query := s.db.WithContext(ctx).Model(&models.Sandbox{})
	if scope.Kind == models.WorkspaceChangesSolo {
		query = query.Where("purpose = ? AND thread_id = ?", SandboxPurposeSolo, scope.OwnerID)
		if workspace == "" {
			var session models.SoloSession
			if err := s.db.WithContext(ctx).Select("sandbox_id").Where("id = ?", scope.OwnerID).Take(&session).Error; err != nil {
				return nil, err
			}
			query = query.Where("id = ?", session.SandboxID)
		}
	} else {
		query = query.Where("purpose = ? AND run_id = ?", "run", scope.OwnerID)
	}
	var row models.Sandbox
	var err error
	switch {
	case workspace != "":
		err = query.Where("name = ?", workspace).Take(&row).Error
	case scope.Kind == models.WorkspaceChangesRun && scope.NodeID != "":
		row, err = runNodeSandboxRow(s.db.WithContext(ctx), scope.OwnerID, scope.NodeID)
	default:
		err = query.Order("updated_at DESC").Take(&row).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, workspacechanges.ErrUnavailable
	}
	return &row, err
}

func (s *SandboxService) RefreshWorkspaceChanges(ctx context.Context, scope workspacechanges.Scope, workspace string) error {
	row, err := s.WorkspaceForChanges(ctx, scope, workspace)
	if err != nil {
		return err
	}
	if row.Status != "running" || row.CleanupPending {
		return workspacechanges.ErrUnavailable
	}
	sb, err := s.soloHandle(ctx, row.ID)
	if err != nil {
		return workspacechanges.ErrUnavailable
	}
	scope.Workspace, scope.NodeID = sb.Name, row.NodeID
	return s.changes.Refresh(ctx, sb, scope)
}
