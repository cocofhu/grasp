package database

import (
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

type legacyPreviewRegistration struct {
	ID          uint
	RunID       string
	NodeID      string
	SandboxName string
}

// migrateLegacyPreviewStorage collapses the old multi-preview rows to the
// single-active invariant (one row per run/node and per sandbox) before
// AutoMigrate creates the run/node unique index. The newest registration wins:
// it is the page the Agent selected last.
func migrateLegacyPreviewStorage(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("run_preview_ports") {
		return nil
	}
	var rows []legacyPreviewRegistration
	if err := db.Table("run_preview_ports").
		Select("id, run_id, node_id, COALESCE(sandbox_name, '') AS sandbox_name").
		Order("registered_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return err
	}
	nodes := make(map[string]struct{}, len(rows))
	sandboxes := make(map[string]struct{}, len(rows))
	remove := make([]uint, 0)
	for _, row := range rows {
		node := row.RunID + "\x00" + row.NodeID
		_, nodeTaken := nodes[node]
		_, sandboxTaken := sandboxes[row.SandboxName]
		if nodeTaken || (row.SandboxName != "" && sandboxTaken) {
			remove = append(remove, row.ID)
			continue
		}
		nodes[node] = struct{}{}
		if row.SandboxName != "" {
			sandboxes[row.SandboxName] = struct{}{}
		}
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
