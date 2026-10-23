package services

import (
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// runNodeSandboxRow returns the latest run sandbox row of a run's node. When
// the node has none of its own and the run uses a shared sandbox (sandbox mode
// "shared"), the run's shared sandbox serves every node and is returned
// instead. Runs without a shared sandbox see the plain run+node lookup.
func runNodeSandboxRow(db *gorm.DB, runID, nodeID string) (models.Sandbox, error) {
	var row models.Sandbox
	err := db.Where("run_id = ? AND node_id = ? AND purpose = ?", runID, nodeID, "run").
		Order("updated_at desc").First(&row).Error
	if err == nil || runID == "" {
		return row, err
	}
	var shared models.Sandbox
	if db.Where("run_id = ? AND purpose = ? AND shared = ?", runID, "run", true).
		Order("updated_at desc").First(&shared).Error != nil {
		return row, err
	}
	return shared, nil
}
