package engine

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

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
		if n == nil || !models.PassThroughNode(n.Type) {
			return src
		}
		cur = src
	}
	return ""
}

// upstreamHandoff loads the handoff the routing upstream node left on its
// latest execution (failed executions included, so a fail edge still carries
// the explanation), looking past approval gates.
func (e *Engine) upstreamHandoff(c *execCtx, node *models.Node) *models.UpstreamHandoff {
	if e.db == nil {
		return nil
	}
	src := routedFrom(c, node.ID)
	var sr models.StateRun
	var text string
	// An approval gate leaves no handoff of its own: look past it to the
	// node whose result it released.
	for hops := 0; src != "" && hops < 16; hops++ {
		sr = models.StateRun{}
		if err := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, src).
			Order("iteration desc, id desc").First(&sr).Error; err != nil {
			return nil
		}
		if text = strings.TrimSpace(str(sr.Outputs[models.OutputHandoff])); text != "" {
			break
		}
		if n := c.graph.FindNode(src); n == nil || n.Type != "human_gate" {
			return nil
		}
		src = routedFrom(c, src)
	}
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

// priorHandoff returns the handoff text/source recorded on the node's current
// execution (latest iteration), so a rebuild can carry it forward.
func (e *Engine) priorHandoff(c *execCtx, node *models.Node) (string, string) {
	if e.db == nil || c == nil || node == nil {
		return "", ""
	}
	q := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, node.ID)
	if iter := c.iter[node.ID]; iter > 0 {
		q = q.Where("iteration = ?", iter)
	} else {
		q = q.Order("iteration desc, id desc")
	}
	var sr models.StateRun
	if err := q.First(&sr).Error; err != nil {
		return "", ""
	}
	text := strings.TrimSpace(str(sr.Outputs[models.OutputHandoff]))
	if text == "" {
		return "", ""
	}
	return text, str(sr.Outputs[models.OutputHandoffSource])
}

// retainHandoff keeps a node's node_complete handoff across a rebuild: when the
// freshly derived outputs lost it (review confirm / revise refresh / react
// resume), the previous execution's handoff is copied back before the platform
// fallback runs. This mirrors execute.go's ensureHandoff so every review/resume
// entry that reconstructs outputs preserves the handoff instead of dropping it.
func (e *Engine) retainHandoff(c *execCtx, node *models.Node, oc *nodeOutcome) {
	if node == nil || oc == nil {
		return
	}
	if strings.TrimSpace(str(oc.outputs[models.OutputHandoff])) == "" {
		if text, src := e.priorHandoff(c, node); text != "" {
			if oc.outputs == nil {
				oc.outputs = map[string]any{}
			}
			oc.outputs[models.OutputHandoff] = text
			if src != "" {
				oc.outputs[models.OutputHandoffSource] = src
			}
		}
	}
	ensureHandoff(c, node, oc)
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
	targets := c.graph.HandoffTargets(node)
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
