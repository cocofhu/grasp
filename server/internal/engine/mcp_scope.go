package engine

import "github.com/cocofhu/grasp/internal/models"

// runScope is the workflow run's artifact MCP rule: the token persisted on the
// run is valid while the run can still execute nodes. Destroying one of the
// run's sandboxes, retries and restarts do not change it; reaching a terminal
// status ends it, and resuming a failed run makes it valid again.
func (e *Engine) runScope(runID string) (string, bool, bool) {
	var run models.Run
	if e.db.Select("mcp_token", "status").Where("id = ?", runID).First(&run).Error != nil {
		return "", false, false
	}
	switch run.Status {
	case "queued", "running", "waiting_human":
		return run.McpToken, true, true
	}
	return run.McpToken, false, true
}

// runDownstream resolves an Agent node's handoff consumers from the run graph,
// so node_complete enforces the handoff after a restart or on another replica.
func (e *Engine) runDownstream(runID, nodeID string) []models.HandoffTarget {
	var run models.Run
	if e.db.Select("graph").Where("id = ?", runID).First(&run).Error != nil {
		return nil
	}
	node := run.Graph.FindNode(nodeID)
	if node == nil || node.Type != "agent" {
		return nil
	}
	return run.Graph.HandoffTargets(node)
}
