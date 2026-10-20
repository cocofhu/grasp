package engine

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// RunNotifyEvent carries confirmed waiting_human / failed context for an
// optional async IM side-effect. Engine never blocks on handlers.
type RunNotifyEvent struct {
	RunID        string
	WorkflowID   string
	WorkflowName string
	NodeID       string
	NodeLabel    string
	Iteration    int
	Kind         string // waiting_human | failed | completed
}

// RunNotifier is an optional async observer for Run lifecycle transitions.
// Implementations must not block meaningfully; Engine invokes them in a goroutine.
type RunNotifier interface {
	NotifyRunLifecycle(ev RunNotifyEvent)
}

// SetRunNotifier wires the Run→IM side-effect hook (nil disables).
func (e *Engine) SetRunNotifier(n RunNotifier) {
	e.runNotify = n
}

// fireRunNotify schedules a non-blocking notify after a confirmed lifecycle
// transition with node context. No-op when unset or kind/node invalid.
func (e *Engine) fireRunNotify(c *execCtx, node *models.Node, kind string) {
	if e.runNotify == nil || c == nil || node == nil {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind != models.NotifyKindWaitingHuman && kind != models.NotifyKindFailed && kind != models.NotifyKindCompleted {
		return
	}
	iter := 0
	if c.iter != nil {
		iter = c.iter[node.ID]
	}
	if iter < 1 {
		return
	}
	var wf models.WorkflowDef
	if err := e.db.Select("name").Where("id = ?", c.run.WorkflowID).First(&wf).Error; err != nil {
		log.Warn().Err(err).Str("run_id", c.run.ID).Str("workflow", c.run.WorkflowID).
			Msg("run-notify: resolve workflow failed")
		return
	}
	wfName := wf.Name
	if wfName == "" {
		wfName = c.run.WorkflowName
	}
	ev := RunNotifyEvent{
		RunID:        c.run.ID,
		WorkflowID:   c.run.WorkflowID,
		WorkflowName: wfName,
		NodeID:       node.ID,
		NodeLabel:    node.Label,
		Iteration:    iter,
		Kind:         kind,
	}
	n := e.runNotify
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Str("run_id", ev.RunID).Str("node_id", ev.NodeID).
					Interface("panic", r).Msg("run-notify: notify panic")
			}
		}()
		n.NotifyRunLifecycle(ev)
	}()
}

// fireCompletedRunNotify resolves the last output node (or graph/sentinel
// fallback) and fires a run-level completed notify. Dedup is the delivery
// receipt (runId, nodeId, iteration, kind) — finish(completed) may return true
// even when RowsAffected==0, so callers must not rely on that flag.
func (e *Engine) fireCompletedRunNotify(runID string) {
	if e.runNotify == nil || strings.TrimSpace(runID) == "" {
		return
	}
	c, err := e.loadCtx(runID)
	if err != nil || c == nil || c.run == nil {
		log.Warn().Err(err).Str("run_id", runID).Msg("run-notify: completed loadCtx failed")
		return
	}
	nodeID, label, iter := e.resolveCompletedNotifyTarget(c)
	if c.iter == nil {
		c.iter = map[string]int{}
	}
	if iter < 1 {
		iter = 1
	}
	c.iter[nodeID] = iter
	e.fireRunNotify(c, &models.Node{ID: nodeID, Label: label, Type: "output"}, models.NotifyKindCompleted)
}

// resolveCompletedNotifyTarget picks {node} + receipt key for completed:
// 1) last executed output node by startedAt (aligned with lastOutputNodeId)
// 2) first output node in the graph if none executed
// 3) sentinel _run + label「输出」 if the graph has no output node
func (e *Engine) resolveCompletedNotifyTarget(c *execCtx) (nodeID, label string, iter int) {
	outputByID := map[string]models.Node{}
	var graphOutputs []models.Node
	for _, n := range c.graph.Nodes {
		if n.Type == "output" {
			outputByID[n.ID] = n
			graphOutputs = append(graphOutputs, n)
		}
	}

	var states []models.StateRun
	if err := e.db.Where("run_id = ?", c.run.ID).Find(&states).Error; err != nil {
		log.Warn().Err(err).Str("run_id", c.run.ID).Msg("run-notify: load state runs for completed failed")
	}

	var best *models.StateRun
	var bestAt time.Time
	for i := range states {
		s := &states[i]
		_, inGraph := outputByID[s.NodeID]
		if !inGraph && s.NodeType != "output" {
			continue
		}
		at := time.Time{}
		if s.StartedAt != nil {
			at = *s.StartedAt
		}
		if best == nil || at.After(bestAt) || (at.Equal(bestAt) && s.ID > best.ID) {
			best = s
			bestAt = at
		}
	}
	if best != nil {
		lbl := ""
		if n, ok := outputByID[best.NodeID]; ok {
			lbl = n.Label
		}
		if strings.TrimSpace(lbl) == "" {
			lbl = best.NodeID
		}
		it := best.Iteration
		if it < 1 {
			it = 1
		}
		return best.NodeID, lbl, it
	}
	if len(graphOutputs) > 0 {
		n := graphOutputs[0]
		lbl := strings.TrimSpace(n.Label)
		if lbl == "" {
			lbl = n.ID
		}
		return n.ID, lbl, 1
	}
	return models.NotifyCompletedSentinelNodeID, models.NotifyCompletedFallbackLabel, 1
}
