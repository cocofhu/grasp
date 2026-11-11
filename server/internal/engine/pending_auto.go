package engine

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/services"
)

// certainAutoSuccessor is the one automatic agent a positive confirm enters
// when the graph already says so. Empty when the outlet is not settled
// (a verdict that has not been chosen, an unknown guard, a rollback, more
// than one destination) or the one destination is not an automatic agent.
// input/output are skipped; they are not the next real node.
func certainAutoSuccessor(g models.Graph, node *models.Node) string {
	if node == nil || node.Type != "agent" || nodereg.Gated(node.Caps) {
		return ""
	}
	return walkCertainAuto(g, node.ID, "")
}

func walkCertainAuto(g models.Graph, from, handle string) string {
	seen := map[string]bool{}
	cursor, hopHandle := from, handle
	limit := len(g.Nodes)
	if limit < 1 {
		limit = 1
	}
	for hop := 0; hop <= limit; hop++ {
		key := cursor + "\x00" + hopHandle
		if seen[key] {
			return ""
		}
		seen[key] = true
		target, settled := certainSuccessTarget(g, cursor, hopHandle)
		if !settled {
			return ""
		}
		n := g.FindNode(target)
		if n == nil {
			return ""
		}
		if n.Type == "input" || n.Type == "output" {
			cursor = n.ID
			hopHandle = ""
			continue
		}
		if n.Type != "agent" || n.Caps == nil || n.Caps.Interaction != models.InteractionAuto || n.Caps.Clarify() {
			return ""
		}
		return n.ID
	}
	return ""
}

// certainSuccessTarget is the first success edge on handle this confirm
// cannot rule out. An unknown guard or a rollback means the destination is
// not settled. A guard that is certainly false is skipped.
func certainSuccessTarget(g models.Graph, from, handle string) (string, bool) {
	for _, ed := range g.OutEdges(from) {
		if ed.SourceHandle != handle || ed.KindOrDefault() == models.EdgeFailure {
			continue
		}
		switch guardCertainty(ed.When) {
		case "fail":
			continue
		case "unknown":
			return "", false
		}
		if ed.KindOrDefault() == models.EdgeRollback {
			return "", false
		}
		return ed.Target, true
	}
	return "", false
}

func guardCertainty(when string) string {
	switch strings.TrimSpace(when) {
	case "", "default", "true":
		return "pass"
	case "false":
		return "fail"
	default:
		return "unknown"
	}
}

// notePendingAuto records the unique automatic successor at the moment a
// confirm marks the dialogue done, before the wrap-up reply returns.
// A successor that is not unique clears any earlier mark.
func (e *Engine) notePendingAuto(c *execCtx, from *models.Node) {
	if c == nil || c.run == nil || from == nil {
		return
	}
	next := certainAutoSuccessor(c.graph, from)
	if next == "" || next == from.ID {
		e.clearPendingAuto(c)
		return
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ? AND status = ?", c.run.ID, from.ID, "waiting_human").
		Order("id desc").First(&sr).Error; err != nil || sr.ID == 0 {
		e.clearPendingAuto(c)
		return
	}
	if c.run.Checkpoints == nil {
		c.run.Checkpoints = map[string]map[string]any{}
	}
	c.run.Checkpoints[services.PendingAutoCheckpoint] = map[string]any{
		"node":  next,
		"from":  from.ID,
		"visit": sr.ID,
	}
	e.saveRunCheckpoints(c.run)
}

// clearPendingAuto drops the early successor mark. Confirm failure, a
// reopened dialogue, and landing anywhere other than that successor call it.
func (e *Engine) clearPendingAuto(c *execCtx) {
	if c == nil || c.run == nil || c.run.Checkpoints == nil {
		return
	}
	if _, ok := c.run.Checkpoints[services.PendingAutoCheckpoint]; !ok {
		return
	}
	delete(c.run.Checkpoints, services.PendingAutoCheckpoint)
	e.saveRunCheckpoints(c.run)
}

// keepPendingAutoOnly drops the mark unless next is still that successor.
// The real visit has not started yet, so the list keeps reporting it.
func (e *Engine) keepPendingAutoOnly(c *execCtx, next string) {
	if c == nil || c.run == nil || c.run.Checkpoints == nil {
		return
	}
	cp := c.run.Checkpoints[services.PendingAutoCheckpoint]
	if cp == nil {
		return
	}
	node, _ := cp["node"].(string)
	if strings.TrimSpace(node) != "" && strings.TrimSpace(node) == next {
		return
	}
	e.clearPendingAuto(c)
}

func (e *Engine) saveRunCheckpoints(run *models.Run) {
	if e == nil || run == nil || run.ID == "" {
		return
	}
	if run.Checkpoints == nil {
		run.Checkpoints = map[string]map[string]any{}
	}
	// Updates skips a nil or empty map, which would leave a cleared mark in
	// the database. An empty object is the cleared value.
	if len(run.Checkpoints) == 0 {
		logDB(e.db.Model(&models.Run{}).Where("id = ?", run.ID).Update("checkpoints", "{}"), run.ID, "clear run checkpoints")
		return
	}
	logDB(e.db.Model(&models.Run{}).Where("id = ?", run.ID).
		Select("Checkpoints").Updates(&models.Run{Checkpoints: run.Checkpoints}), run.ID, "save run checkpoints")
}
