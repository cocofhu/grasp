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
