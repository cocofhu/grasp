package services

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func (s *PmService) SetExecutionController(c *executionlineage.Controller) { s.execution = c }

func (s *PmService) executionEntities(ctx context.Context, threadID string) ([]executionlineage.Entity, error) {
	var draft models.ChatTurnDraft
	err := s.db.WithContext(ctx).Select("id").Where("thread_id = ?", threadID).Take(&draft).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	entities := []executionlineage.Entity{}
	if draft.ID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityPMDraft, ID: draft.ID})
	}
	var jobs []struct{ ID string }
	if err := s.db.WithContext(ctx).Table("agent_cron_jobs").Select("id").Where("thread_id = ?", threadID).Find(&jobs).Error; err != nil {
		return nil, err
	}
	for _, job := range jobs {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityCronJob, ID: job.ID})
	}
	return entities, nil
}

func (s *PmService) executionCheck(ctx context.Context, threadID string) error {
	if s.execution == nil {
		return nil
	}
	entities, err := s.executionEntities(ctx, threadID)
	if err != nil {
		return err
	}
	return s.execution.Check(ctx, entities...)
}

func (s *PmService) executionTransaction(ctx context.Context, threadID string, body func(*gorm.DB) error) error {
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	entities, err := s.executionEntities(ctx, threadID)
	if err != nil {
		return err
	}
	return s.execution.Transaction(ctx, s.db, entities, body)
}
