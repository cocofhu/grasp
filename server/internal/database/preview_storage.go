package database

import (
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

type legacyPreviewRegistration struct {
	ID           uint
	RunID        string
	NodeID       string
	RegisteredAt time.Time
}

// migrateLegacyPreviewStorage collapses the old multi-preview rows before the
// active-preview unique index is created by AutoMigrate. Keeping the newest
// registration preserves the page that was last selected by the Agent.
func migrateLegacyPreviewStorage(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("run_preview_ports") {
		return nil
	}
	var rows []legacyPreviewRegistration
	if err := db.Table("run_preview_ports").
		Select("id, run_id, node_id, registered_at").
		Order("run_id ASC, node_id ASC, registered_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(rows))
	remove := make([]uint, 0)
	for _, row := range rows {
		key := row.RunID + "\x00" + row.NodeID
		if _, ok := seen[key]; ok {
			remove = append(remove, row.ID)
			continue
		}
		seen[key] = struct{}{}
	}
	if len(remove) > 0 {
		if err := db.Exec("DELETE FROM run_preview_ports WHERE id IN ?", remove).Error; err != nil {
			return err
		}
	}
	if db.Migrator().HasIndex(&models.RunPreviewPort{}, "idx_preview_run_node_item") {
		if err := db.Migrator().DropIndex(&models.RunPreviewPort{}, "idx_preview_run_node_item"); err != nil {
			return err
		}
	}
	return nil
}
