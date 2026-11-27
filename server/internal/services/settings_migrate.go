package services

import (
	"strconv"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// migrateSandboxSettings consumes the former per-purpose settings once. They
// are not aliases: after this transaction only the shared policy is consulted.
// The larger saved capacity avoids silently reducing an operator's chosen
// limit; an explicit new value always wins. Old expiry windows are deliberately
// not carried over because they meant destructive retirement, not saved idle
// reclamation. The shared idle window starts from the configured 60m default.
func (s *SettingsService) migrateSandboxSettings() error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		keys := []string{"max_test_sandboxes", "max_solo_sandboxes", "run_sandbox_ttl_minutes",
			"test_sandbox_ttl_minutes", "run_sandbox_max_retention_hours"}
		var rows []models.Setting
		if err := tx.Where(map[string]any{"key": keys}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		var existing int64
		if err := tx.Model(&models.Setting{}).Where(map[string]any{"key": KeyMaxSandboxes}).Count(&existing).Error; err != nil {
			return err
		}
		if existing == 0 {
			limit := 0
			for _, row := range rows {
				if row.Key != "max_test_sandboxes" && row.Key != "max_solo_sandboxes" {
					continue
				}
				if n, err := strconv.Atoi(row.Value); err == nil && n > limit {
					limit = n
				}
			}
			if limit > 0 {
				if err := setSetting(tx, KeyMaxSandboxes, strconv.Itoa(limit)); err != nil {
					return err
				}
			}
		}
		return tx.Where(map[string]any{"key": keys}).Delete(&models.Setting{}).Error
	})
}
