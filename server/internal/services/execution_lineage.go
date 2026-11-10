package services

import (
	"context"
	"errors"
	"strconv"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func (s *SoloService) SetExecutionController(c *executionlineage.Controller) {
	s.execution = c
	s.durable.SetExecutionController(c)
}

func (s *SoloService) executionEntities(id string) []executionlineage.Entity {
	if id == "" {
		return nil // A new explicit Solo identity still checks the active dataset.
	}
	return []executionlineage.Entity{{Kind: executionlineage.EntitySolo, ID: id}, {Kind: executionlineage.EntityQueue, ID: soloQueueScope(id).ID()}}
}

func (s *SoloService) executionTransaction(ctx context.Context, id string, body func(*gorm.DB) error, extra ...executionlineage.Entity) error {
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	return s.execution.Transaction(ctx, s.db, append(s.executionEntities(id), extra...), body)
}

func (s *SoloService) executionCheck(ctx context.Context, id string, extra ...executionlineage.Entity) error {
	if s.execution == nil {
		return nil
	}
	return s.execution.Check(ctx, append(s.executionEntities(id), extra...)...)
}

func (s *SandboxService) guardExecutionTx(ctx context.Context, tx *gorm.DB, id uint) error {
	if s.execution == nil {
		return nil
	}
	if err := s.execution.GuardContextTx(ctx, tx); err != nil {
		return err
	}
	var row models.Sandbox
	if err := tx.Select("id", "purpose", "run_id", "thread_id", "state_run_id").First(&row, id).Error; err != nil {
		return err
	}
	entities := []executionlineage.Entity{{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(id), 10)}}
	switch row.Purpose {
	case SandboxPurposeSolo:
		if row.ThreadID == "" {
			return executionlineage.ErrConflict
		}
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: row.ThreadID}, executionlineage.Entity{Kind: executionlineage.EntityQueue, ID: soloQueueScope(row.ThreadID).ID()})
	case "run":
		if row.RunID == "" {
			return executionlineage.ErrConflict
		}
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityRun, ID: row.RunID})
		var run models.Run
		if err := tx.Select("id", "origin_solo_id").Take(&run, "id = ?", row.RunID).Error; err != nil {
			return err
		}
		if run.OriginSoloID != "" {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
		}
		if row.StateRunID != 0 {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(row.StateRunID), 10)})
		}
	}
	return s.execution.GuardContextTx(ctx, tx, entities...)
}

func (s *SandboxService) executionTransaction(ctx context.Context, id uint, body func(*gorm.DB) error) error {
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	ctx, err := s.execution.PrepareContext(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.guardExecutionTx(ctx, tx, id); err != nil {
			return err
		}
		if err := body(tx); err != nil {
			return err
		}
		return s.execution.GuardContextTx(ctx, tx)
	})
}

func (s *SandboxService) executionCheck(ctx context.Context, id uint) error {
	if s.execution == nil {
		return nil
	}
	return s.executionTransaction(ctx, id, func(*gorm.DB) error { return nil })
}

func (s *SandboxService) executionCreateTransaction(ctx context.Context, entities []executionlineage.Entity, body func(*gorm.DB) error) error {
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	return s.execution.Transaction(ctx, s.db, entities, body)
}

func sandboxExecutionLeaseError(err error) error {
	if errors.Is(err, executionlineage.ErrInactive) || errors.Is(err, executionlineage.ErrConflict) || errors.Is(err, executionlineage.ErrUnbound) || errors.Is(err, executionlineage.ErrQuarantined) || errors.Is(err, executionlineage.ErrUninitialized) {
		return errors.Join(errSandboxOperationLost, err)
	}
	return err
}
