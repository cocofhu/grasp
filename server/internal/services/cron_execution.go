package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"gorm.io/gorm"
)

func cronExecutionEntities(jobID, runID string) []executionlineage.Entity {
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityCronJob, ID: jobID}}
	if runID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityCronRun, ID: runID})
	}
	return entities
}

func (s *CronScheduler) executionTransaction(ctx context.Context, jobID, runID string, body func(*gorm.DB) error) error {
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	return s.execution.Transaction(ctx, s.db, cronExecutionEntities(jobID, runID), body)
}

func (s *CronScheduler) executionCheck(ctx context.Context, jobID, runID string) error {
	if s.execution == nil {
		return nil
	}
	return s.execution.Check(ctx, cronExecutionEntities(jobID, runID)...)
}
