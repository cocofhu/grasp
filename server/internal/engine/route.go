package engine

import (
	"fmt"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
)

// routeSuccess selects the next state after a node succeeds: the first edge
// leaving through the outcome's handle whose when-guard passes.
func (e *Engine) routeSuccess(c *execCtx, node *models.Node, outcome nodeOutcome) string {
	if e.IsHalted() {
		e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit", Detail: "shutdown: scheduler halted"})
		e.finish(c.run.ID, "cancelled")
		return ""
	}
	ec := e.evalContext(c, outcomeAction(outcome))
	for _, ed := range c.graph.OutEdges(node.ID) {
		if ed.SourceHandle != outcome.handle || ed.KindOrDefault() == models.EdgeFailure {
			continue
		}
		if !guardPasses(ed.When, ec) {
			continue
		}
		if ed.KindOrDefault() == models.EdgeRollback {
			if target, ok := e.doRollback(c, ed); ok {
				return target
			}
			continue
		}
		e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "transition", To: ed.Target, Kind: ed.KindOrDefault()})
		return ed.Target
	}
	return ""
}

// routeFailure selects the next state after a node fails. An outcome leaving
// through a named handle (a failing verdict) follows that handle's edges;
// otherwise plain-outlet rollback edges, then failure edges, apply.
func (e *Engine) routeFailure(c *execCtx, node *models.Node, outcome nodeOutcome) string {
	edges := c.graph.OutEdges(node.ID)
	if outcome.handle != "" {
		for _, ed := range edges {
			if ed.SourceHandle != outcome.handle {
				continue
			}
			if ed.KindOrDefault() == models.EdgeRollback {
				if target, ok := e.doRollback(c, ed); ok {
					return target
				}
				continue
			}
			e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "transition", To: ed.Target, Kind: ed.KindOrDefault()})
			return ed.Target
		}
		return ""
	}
	for _, ed := range edges {
		if ed.SourceHandle == "" && ed.KindOrDefault() == models.EdgeRollback {
			if target, ok := e.doRollback(c, ed); ok {
				return target
			}
		}
	}
	for _, ed := range edges {
		if ed.KindOrDefault() == models.EdgeFailure {
			e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "transition", To: ed.Target, Kind: models.EdgeFailure})
			return ed.Target
		}
	}
	return ""
}

func outcomeAction(outcome nodeOutcome) map[string]any {
	if a, ok := outcome.outputs["action"]; ok {
		return map[string]any{"action": a}
	}
	return map[string]any{}
}

// doRollback performs a rollback transition: enforce the attempt cap, restore
// the target checkpoint's variable snapshot, and inject carried error context.
func (e *Engine) doRollback(c *execCtx, ed models.Edge) (string, bool) {
	c.run.Attempt++
	if ed.MaxAttempts > 0 && c.run.Attempt > ed.MaxAttempts {
		e.appendTrace(c, models.TraceEntry{NodeID: ed.Source, Event: "exit", Detail: "rollback attempts exhausted"})
		c.run.Attempt--
		return "", false
	}
	logDB(e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).UpdateColumn("attempt", c.run.Attempt), c.run.ID, "rollback attempt")

	if snap, ok := c.run.Checkpoints[ed.Target]; ok {
		for k, v := range snap {
			c.setVar(k, v)
			e.persistVar(c.run.ID, k, v)
		}
	}

	for _, name := range ed.Carry {
		if _, ok := c.vars[name]; !ok {
			c.setVar(name, "")
			e.persistVar(c.run.ID, name, "")
		}
	}
	e.appendTrace(c, models.TraceEntry{NodeID: ed.Source, Event: "rollback", To: ed.Target, Kind: models.EdgeRollback,
		Detail: fmt.Sprintf("attempt=%d 携带 %v 回滚到 checkpoint", c.run.Attempt, ed.Carry)})
	return ed.Target, true
}

func (e *Engine) snapshotCheckpoint(c *execCtx, nodeID string) {
	if c.run.Checkpoints == nil {
		c.run.Checkpoints = map[string]map[string]any{}
	}
	snap := map[string]any{}
	for k, v := range c.vars {
		snap[k] = blob.StripDataInValue(v)
	}
	c.run.Checkpoints[nodeID] = snap

	logDB(e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).
		Select("Checkpoints").Updates(&models.Run{Checkpoints: c.run.Checkpoints}), c.run.ID, "snapshot checkpoint")
}
