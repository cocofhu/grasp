package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const diagnosticRunOwner = "grasp-doctor"

func diagnosticEntities(ids []string) ([]executionlineage.Entity, error) {
	if len(ids) != 2 || ids[0] == ids[1] {
		return nil, errors.New("two distinct diagnostic runs are required")
	}
	entities := make([]executionlineage.Entity, 0, len(ids))
	for _, id := range ids {
		if !(strings.HasPrefix(id, "doctor-a-") || strings.HasPrefix(id, "doctor-b-")) || uuid.Validate(id[len("doctor-a-"):]) != nil {
			return nil, errors.New("invalid diagnostic run identity")
		}
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityRun, ID: id})
	}
	return entities, nil
}

// DiagnosticRuns uses the ordinary SQL execution fence. Diagnostic namespaces
// are real terminal runs, never executable workflow submissions or RAM-only
// exceptions to artifact authorization.
func (s *ArtifactService) DiagnosticRuns(ctx context.Context, create bool, ids ...string) error {
	entities, err := diagnosticEntities(ids)
	if err != nil {
		return err
	}
	body := func(tx *gorm.DB) error {
		if create {
			now := time.Now().UTC()
			rows := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				rows = append(rows, map[string]any{"id": id, "status": "completed", "created_by": diagnosticRunOwner,
					"launch_source": "doctor", "trigger": "doctor", "title": "Release verification", "created_at": now, "started_at": now})
			}
			return tx.Table("runs").Create(&rows).Error
		}
		var count int64
		if err := tx.Model(&models.Run{}).Where("id IN ? AND created_by = ? AND launch_source = ? AND status = ?", ids, diagnosticRunOwner, "doctor", "completed").Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(ids)) {
			return errors.New("diagnostic run ownership changed; retained for review")
		}
		if err := tx.Where("run_id IN ?", ids).Delete(&models.ArtifactVersion{}).Error; err != nil {
			return err
		}
		if err := tx.Where("run_id IN ?", ids).Delete(&models.Artifact{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ? AND created_by = ? AND launch_source = ? AND status = ?", ids, diagnosticRunOwner, "doctor", "completed").Delete(&models.Run{}).Error
	}
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	return s.execution.Transaction(ctx, s.db, entities, body)
}
