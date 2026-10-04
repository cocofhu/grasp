package engine

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// GateAutoInvokeEvent carries the gate-pause context for an optional async
// side-effect (e.g. auto-invoke PM Leader). Engine never blocks on handlers.
type GateAutoInvokeEvent struct {
	ProjectID   string
	RunID       string
	WorkflowID  string
	NodeID      string
	NodeType    string
	NodeLabel   string
	GateID      uint
	GateTitle   string
	GateBodyMd  string
	GateActions []models.GateAction
	Vars        map[string]any
	PathSummary string
}

// GateAutoInvoker is an optional async observer for gate pauses. Implementations
// must not block meaningfully; Engine invokes them in a new goroutine.
type GateAutoInvoker interface {
	NotifyGatePaused(ev GateAutoInvokeEvent)
}

// SetGateAutoInvoker wires the paused-gate side-effect hook (nil disables).
func (e *Engine) SetGateAutoInvoker(inv GateAutoInvoker) {
	e.gateAuto = inv
}

// fireGateAutoInvoke schedules a non-blocking notify after a confirmed pending
// gate pause. No-op when unset or for non-gate node types (react / review).
func (e *Engine) fireGateAutoInvoke(c *execCtx, node *models.Node) {
	if e.gateAuto == nil || c == nil || node == nil {
		return
	}
	switch node.Type {
	case "human_gate", "proposal_select":
	default:
		return
	}
	var wf models.WorkflowDef
	if err := e.db.Select("project_id").Where("id = ?", c.run.WorkflowID).First(&wf).Error; err != nil {
		log.Warn().Err(err).Str("run_id", c.run.ID).Str("workflow", c.run.WorkflowID).
			Msg("gate-auto: resolve project failed")
		return
	}
	projectID := strings.TrimSpace(wf.ProjectID)
	if projectID == "" {
		log.Warn().Str("run_id", c.run.ID).Str("workflow", c.run.WorkflowID).
			Msg("gate-auto: workflow has empty project_id")
		return
	}
	var gate models.Gate
	if err := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, node.ID).
		Order("iteration desc, id desc").First(&gate).Error; err != nil {
		log.Warn().Err(err).Str("run_id", c.run.ID).Str("node_id", node.ID).
			Msg("gate-auto: load gate failed")
		return
	}
	if gate.Resolved {
		return
	}
	varsCopy := make(map[string]any, len(c.vars))
	for k, v := range c.vars {
		varsCopy[k] = v
	}
	actionsCopy := append([]models.GateAction(nil), gate.Actions...)
	ev := GateAutoInvokeEvent{
		ProjectID:   projectID,
		RunID:       c.run.ID,
		WorkflowID:  c.run.WorkflowID,
		NodeID:      node.ID,
		NodeType:    node.Type,
		NodeLabel:   node.Label,
		GateID:      gate.ID,
		GateTitle:   gate.Title,
		GateBodyMd:  gate.BodyMd,
		GateActions: actionsCopy,
		Vars:        varsCopy,
		PathSummary: gatePathSummary(c.graph, node.ID),
	}
	inv := e.gateAuto
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Str("run_id", ev.RunID).Str("node_id", ev.NodeID).
					Interface("panic", r).Msg("gate-auto: notify panic")
			}
		}()
		inv.NotifyGatePaused(ev)
	}()
}

// gatePathSummary builds a short "A → B → C" label path from the graph start to
// upToNodeID (best-effort BFS parents). Falls back to the target node id/label.
func gatePathSummary(g models.Graph, upToNodeID string) string {
	byID := make(map[string]models.Node, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	labelOf := func(id string) string {
		n, ok := byID[id]
		if !ok {
			return id
		}
		if strings.TrimSpace(n.Label) != "" {
			return n.Label
		}
		if strings.TrimSpace(n.Type) != "" {
			return n.Type
		}
		return id
	}
	if upToNodeID == "" {
		return ""
	}
	// Prefer a path from the first input/start-like node.
	start := ""
	for _, n := range g.Nodes {
		if n.Type == "input" {
			start = n.ID
			break
		}
	}
	if start == "" && len(g.Nodes) > 0 {
		start = g.Nodes[0].ID
	}
	if start == "" {
		return labelOf(upToNodeID)
	}
	if start == upToNodeID {
		return labelOf(upToNodeID)
	}
	out := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind == models.EdgeFailure || e.Kind == models.EdgeRollback {
			continue
		}
		out[e.Source] = append(out[e.Source], e.Target)
	}
	parent := map[string]string{}
	queue := []string{start}
	seen := map[string]bool{start: true}
	found := false
	for len(queue) > 0 && !found {
		cur := queue[0]
		queue = queue[1:]
		for _, nxt := range out[cur] {
			if seen[nxt] {
				continue
			}
			seen[nxt] = true
			parent[nxt] = cur
			if nxt == upToNodeID {
				found = true
				break
			}
			queue = append(queue, nxt)
		}
	}
	if !found {
		return labelOf(upToNodeID)
	}
	var chain []string
	for id := upToNodeID; id != ""; id = parent[id] {
		chain = append(chain, labelOf(id))
		if id == start {
			break
		}
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return strings.Join(chain, " → ")
}
