package services

import (
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// EffectiveNode is the manual-aware accessor for a run node read from the
// persisted graph: when the node's latest visit is a live user takeover it
// returns a copy whose caps are models.ManualReviewCaps (review semantics);
// otherwise the graph node itself. nil when the node does not exist. Use it
// wherever a reader decides "is this a review session" from raw graph caps
// (share links, embed, MCP active-node fallback) so a takeover session
// behaves exactly like a review node.
func EffectiveNode(db *gorm.DB, run models.Run, nodeID string) *models.Node {
	node := run.Graph.FindNode(nodeID)
	if node == nil || db == nil || node.Caps == nil || node.Caps.Clarify() || node.Caps.Review {
		return node
	}
	var sr models.StateRun
	if db.Select("id", "iteration", "status", "mode").
		Where("run_id = ? AND node_id = ?", run.ID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error != nil {
		return node
	}
	if !models.ManualVisitLive(sr, sr.Iteration) {
		return node
	}
	cp := *node
	cp.Caps = models.ManualReviewCaps(node.Caps)
	return &cp
}

// EffectiveNode is the RunService form of services.EffectiveNode.
func (s *RunService) EffectiveNode(run models.Run, nodeID string) *models.Node {
	return EffectiveNode(s.db, run, nodeID)
}
