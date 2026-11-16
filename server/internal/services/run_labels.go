package services

import (
	"github.com/cocofhu/grasp/internal/models"
)

// CurrentNodeIDs returns the current node id of every active run, as decided
// by CurrentNodes. An entering successor has no visit yet, so it is omitted.
func (s *RunService) CurrentNodeIDs(runs []models.Run) map[string]string {
	nodes := s.CurrentNodes(runs)
	if nodes == nil {
		return nil
	}
	ids := make(map[string]string, len(nodes))
	for id, cn := range nodes {
		if cn.State != CurrentStateEntering {
			ids[id] = cn.ID
		}
	}
	return ids
}

// RunFailedError returns the latest error message from failed StateRuns for a
// failed run, or "" when none.
func (s *RunService) RunFailedError(runID string) string {
	var sr models.StateRun
	if err := s.db.Where("run_id = ? AND status = ? AND error != ''", runID, "failed").
		Order("id desc").First(&sr).Error; err != nil {
		return ""
	}
	return sr.Error
}
