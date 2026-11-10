package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
)

func (s *Store) ColdStart(ctx context.Context, resource, request string) (models.ColdStart, error) {
	var row models.ColdStart
	err := s.sess(ctx).Where("resource = ? AND request_id = ?", resource, request).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	return row, err
}

func (s *Store) ReserveColdStart(ctx context.Context, in models.ColdStart, env map[string]string) (models.ColdStart, bool, error) {
	var row models.ColdStart
	admitted := false
	err := s.sess(ctx).Transaction(func(tx *gorm.DB) error {
		// Match whole-environment release's Fence -> Sandbox -> operation order.
		fence := models.EnvironmentReleaseFence{Resource: in.Resource, Lineage: env["GRASP_EXECUTION_LINEAGE"]}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&fence).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("resource = ?", in.Resource).Take(&fence).Error; err != nil {
			return err
		}
		if fence.ActiveReleaseID != "" {
			return driver.ErrEnvironmentReleasePending
		}
		if fence.Lineage != env["GRASP_EXECUTION_LINEAGE"] {
			return driver.ErrColdStartConflict
		}
		var sb models.Sandbox
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", in.Resource).Take(&sb).Error; err != nil {
			return err
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("resource = ?", in.Resource).Take(&row).Error
		if err == nil {
			if row.RequestID != in.RequestID || row.InputSHA256 != in.InputSHA256 || row.PayloadSHA256 != in.PayloadSHA256 {
				return driver.ErrColdStartConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if sb.Status != models.StatusStopped {
			return driver.ErrColdStartConflict
		}
		row = in
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		sb.SetEnv(env)
		updated := tx.Model(&models.Sandbox{}).Where("id = ? AND status = ?", sb.ID, models.StatusStopped).Update("env", sb.EnvJSON)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return driver.ErrColdStartConflict
		}
		admitted = true
		return nil
	})
	return row, admitted, err
}

func (s *Store) ConfirmColdStart(ctx context.Context, row models.ColdStart, native string) error {
	changed := s.sess(ctx).Model(&models.ColdStart{}).Where("resource = ? AND request_id = ? AND input_sha256 = ? AND payload_sha256 = ? AND (native_identity = '' OR native_identity = ?)", row.Resource, row.RequestID, row.InputSHA256, row.PayloadSHA256, native).Updates(map[string]any{"native_identity": native, "state": "started"})
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return driver.ErrColdStartConflict
	}
	return nil
}

func (s *Store) GuardColdStart(ctx context.Context, resource string) error {
	var count int64
	if err := s.sess(ctx).Model(&models.ColdStart{}).Where("resource = ? AND state <> 'started'", resource).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return driver.ErrColdStartUnknown
	}
	return nil
}
