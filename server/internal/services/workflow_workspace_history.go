package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func (s *WorkflowWorkspaceCheckpoints) List(ctx context.Context, actor, runID string) ([]models.WorkflowVisitCheckpoint, error) {
	if err := s.options.ReadScope(ctx, s.db.WithContext(ctx), actor, "run", runID); err != nil {
		return nil, err
	}
	var rows []models.WorkflowVisitCheckpoint
	err := s.db.WithContext(ctx).Omit("outcome_ref", "outcome_sha256", "graph_sha256", "resource", "lineage", "actor_id", "capture_nonce", "binding_fingerprint").Where("run_id = ?", runID).Order("state_run_id DESC,id").Limit(1000).Find(&rows).Error
	return rows, err
}

func (s *WorkflowWorkspaceCheckpoints) Read(ctx context.Context, actor, runID, id string) (models.WorkflowVisitCheckpoint, error) {
	if err := s.options.ReadScope(ctx, s.db.WithContext(ctx), actor, "run", runID); err != nil {
		return models.WorkflowVisitCheckpoint{}, err
	}
	row, err := s.Get(ctx, id)
	if err != nil {
		return row, err
	}
	if row.RunID != runID {
		return models.WorkflowVisitCheckpoint{}, gorm.ErrRecordNotFound
	}
	return row, nil
}
