package engine

import (
	"context"
	"errors"
	"strconv"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func (e *Engine) executionEntities(ctx context.Context, runID string) ([]executionlineage.Entity, error) {
	if runID == "" {
		return nil, nil
	}
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityRun, ID: runID}}
	var run models.Run
	err := e.db.WithContext(ctx).Select("origin_solo_id").First(&run, "id = ?", runID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if run.OriginSoloID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
	}
	// A terminal Run can still own an interrupted human/review visit. A new
	// child or manual resume must not bypass the restored visit's quarantine.
	var blocked struct{ ID uint }
	err = e.db.WithContext(ctx).Table("state_runs s").Select("s.id").
		Joins("JOIN execution_quarantines q ON q.entity_kind = ? AND q.entity_id = CAST(s.id AS CHAR) AND q.review_state = ?", executionlineage.EntityStateRun, "pending").
		Where("s.run_id = ?", runID).Take(&blocked).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(blocked.ID), 10)})
	}
	return entities, nil
}

func (e *Engine) executionTransaction(ctx context.Context, runID string, body func(*gorm.DB) error, extra ...executionlineage.Entity) error {
	if e.execution == nil {
		return e.db.WithContext(ctx).Transaction(body)
	}
	entities, err := e.executionEntities(ctx, runID)
	if err != nil {
		return err
	}
	return e.execution.Transaction(ctx, e.db, append(entities, extra...), body)
}

func (e *Engine) executionCheck(ctx context.Context, runID string, extra ...executionlineage.Entity) error {
	if e.execution == nil {
		return nil
	}
	entities, err := e.executionEntities(ctx, runID)
	if err != nil {
		return err
	}
	return e.execution.Check(ctx, append(entities, extra...)...)
}

func (e *Engine) executableRuns(query *gorm.DB) *gorm.DB {
	if e.execution == nil {
		return query
	}
	return query.Where("NOT EXISTS (SELECT 1 FROM execution_quarantines q WHERE q.entity_kind = 'run' AND q.entity_id = runs.id AND q.review_state = 'pending')").
		Where("NOT EXISTS (SELECT 1 FROM execution_quarantines q WHERE q.entity_kind = 'solo' AND q.entity_id = runs.origin_solo_id AND q.review_state = 'pending')").
		Where("NOT EXISTS (SELECT 1 FROM state_runs s JOIN execution_quarantines q ON q.entity_kind = 'state_run' AND q.entity_id = CAST(s.id AS CHAR) AND q.review_state = 'pending' WHERE s.run_id = runs.id)")
}
