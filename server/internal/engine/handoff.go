package engine

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// passThroughNode reports node types that only route or set variables; the
// handoff passes through them to the next real consumer.
func passThroughNode(t string) bool { return t == "branch" || t == "set_var" }

// handoffTargets lists the nodes that consume node's handoff: each outgoing
// edge's target, looking through branch / set_var nodes, ignoring the
// terminal output sink. Outlet is the first hop's handle (or edge kind).
func handoffTargets(g models.Graph, node *models.Node) []models.HandoffTarget {
	if node == nil {
		return nil
	}
	var out []models.HandoffTarget
	seen := map[string]bool{}
	var walk func(from, outlet string, visited map[string]bool)
	walk = func(from, outlet string, visited map[string]bool) {
		for _, ed := range g.OutEdges(from) {
			o := outlet
			if o == "" {
				o = ed.SourceHandle
				if o == "" {
					o = string(ed.KindOrDefault())
				}
			}
			tn := g.FindNode(ed.Target)
			if tn == nil || tn.Type == "output" || visited[tn.ID] {
				continue
			}
			if passThroughNode(tn.Type) {
				next := map[string]bool{tn.ID: true}
				for k := range visited {
					next[k] = true
				}
				walk(tn.ID, o, next)
				continue
			}
			key := tn.ID + "|" + o
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, models.HandoffTarget{NodeID: tn.ID, Label: tn.Label, Type: tn.Type, Outlet: o})
		}
	}
	walk(node.ID, "", map[string]bool{node.ID: true})
	return out
}

// routedFrom returns the node that most recently transitioned into nodeID,
// looking back through pass-through nodes. "" when nodeID was entered first.
func routedFrom(c *execCtx, nodeID string) string {
	cur := nodeID
	for hops := 0; hops < 16; hops++ {
		src := ""
		for i := len(c.run.Trace) - 1; i >= 0; i-- {
			t := c.run.Trace[i]
			if t.Event == "transition" && t.To == cur {
				src = t.NodeID
				break
			}
		}
		if src == "" {
			return ""
		}
		n := c.graph.FindNode(src)
		if n == nil || !passThroughNode(n.Type) {
			return src
		}
		cur = src
	}
	return ""
}

// upstreamHandoff loads the handoff the routing upstream node left on its
// latest execution (failed executions included, so a fail edge still carries
// the explanation).
func (e *Engine) upstreamHandoff(c *execCtx, node *models.Node) *models.UpstreamHandoff {
	src := routedFrom(c, node.ID)
	if src == "" || e.db == nil {
		return nil
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, src).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		return nil
	}
	text := strings.TrimSpace(str(sr.Outputs[models.OutputHandoff]))
	if text == "" {
		return nil
	}
	label := src
	if n := c.graph.FindNode(src); n != nil && strings.TrimSpace(n.Label) != "" {
		label = n.Label
	}
	return &models.UpstreamHandoff{
		NodeID: src, Label: label, Text: text,
		Source: str(sr.Outputs[models.OutputHandoffSource]), Status: sr.Status,
	}
}

// ensureHandoff guarantees an Agent node with a downstream leaves a handoff
// even when the agent never reached node_complete (crash, timeout, failed
// checks): the platform writes one from the failure so the next node and the
// run canvas still see why.
func ensureHandoff(c *execCtx, node *models.Node, oc *nodeOutcome) {
	if node == nil || node.Type != "agent" || (oc.status != "failed" && oc.status != "completed") {
		return
	}
	if strings.TrimSpace(str(oc.outputs[models.OutputHandoff])) != "" {
		return
	}
	targets := handoffTargets(c.graph, node)
	if len(targets) == 0 {
		return
	}
	if oc.outputs == nil {
		oc.outputs = map[string]any{}
	}
	oc.outputs[models.OutputHandoff] = platformHandoff(oc)
	oc.outputs[models.OutputHandoffSource] = models.HandoffSourcePlatform
}

func platformHandoff(oc *nodeOutcome) string {
	if oc.status == "failed" {
		reason := strings.TrimSpace(oc.err)
		if reason == "" {
			reason = "未知原因"
		}
		return truncateRunes(fmt.Sprintf("上游节点执行失败,未留下交接说明。失败原因:%s", reason), models.HandoffMaxRunes)
	}
	if sum := strings.TrimSpace(str(oc.outputs["outcome_summary"])); sum != "" {
		return truncateRunes("上游节点未留下交接说明。完成摘要:"+sum, models.HandoffMaxRunes)
	}
	return "上游节点已完成,但未留下交接说明。"
}
