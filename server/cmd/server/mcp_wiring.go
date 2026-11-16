package main

import (
	"errors"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

// wireArtifactMCPPersistence restores active-node capabilities when a sandbox
// calls another replica or the server restarts. Solo namespaces have no Run
// row: resolve their current resident sandbox and session instead.
// Authorization itself is owned by each namespace's ScopeSource.
func wireArtifactMCPPersistence(host *mcp.Host, db *gorm.DB, runs *services.RunService, preview *services.PreviewService) {
	host.SetActiveNodeSource(func(runID string) (string, *models.AgentCapabilities, bool) {
		var run models.Run
		if err := db.Select("id", "status", "graph").First(&run, "id = ?", runID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if _, caps, ok := preview.SoloMCPBinding(runID); ok {
					return "solo", caps, true
				}
			}
			return "", nil, false
		}
		// Prefer the active visit. Between visits (the next node is still
		// queued) attribute the call to the node owning the latest sandbox.
		nodeID := runs.CurrentNodeIDs([]models.Run{run})[runID]
		if nodeID == "" {
			var sb models.Sandbox
			if db.Where("run_id = ? AND purpose = ?", runID, "run").
				Order("updated_at desc").First(&sb).Error != nil || strings.TrimSpace(sb.NodeID) == "" {
				return "", nil, false
			}
			nodeID = sb.NodeID
		}
		var caps *models.AgentCapabilities
		if n := services.EffectiveNode(db, run, nodeID); n != nil {
			caps = n.Caps
		}
		return nodeID, caps, true
	})
}
