package models

import (
	"fmt"
	"strings"
)

// Handoff is what an Agent node tells the node after it. The agent writes it
// with node_complete (required whenever the node has a downstream, on success
// and on failure); when the agent never gets there the platform writes one
// from the failure. Stored on the node's outputs and shown on the run canvas.
const (
	OutputHandoff       = "handoff"
	OutputHandoffSource = "handoff_source"

	HandoffSourceAgent    = "agent"
	HandoffSourcePlatform = "platform"

	HandoffMaxRunes = 2000
)

// HandoffTarget is one downstream consumer of a node's handoff. Outlet is the
// source handle (pass / fail / gate action) or edge kind the path leaves by.
type HandoffTarget struct {
	NodeID string `json:"nodeId"`
	Label  string `json:"label,omitempty"`
	Type   string `json:"type,omitempty"`
	Outlet string `json:"outlet,omitempty"`
}

// UpstreamHandoff is the handoff a node receives from the node that routed to it.
type UpstreamHandoff struct {
	NodeID string
	Label  string
	Text   string
	Source string
	Status string // the upstream node's status: completed | failed
}

// HandoffTargetsText renders targets for prompts / tool errors.
func HandoffTargetsText(targets []HandoffTarget) string {
	parts := make([]string, 0, len(targets))
	for _, t := range targets {
		name := strings.TrimSpace(t.Label)
		if name == "" {
			name = t.NodeID
		}
		s := fmt.Sprintf("「%s」(%s)", name, t.NodeID)
		if t.Outlet != "" {
			s += " 经 " + t.Outlet + " 出口"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "、")
}

// PassThroughNode reports node types that only route or set variables; the
// handoff passes through them to the next real consumer.
func PassThroughNode(t string) bool { return t == "branch" || t == "set_var" }

// HandoffTargets lists the nodes that consume node's handoff: each outgoing
// edge's target, looking through branch / set_var nodes and past approval
// gates (which are listed themselves), ignoring the terminal output sink.
// Outlet is the first hop's handle (or edge kind).
func (g Graph) HandoffTargets(node *Node) []HandoffTarget {
	if node == nil {
		return nil
	}
	var out []HandoffTarget
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
			if PassThroughNode(tn.Type) {
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
			out = append(out, HandoffTarget{NodeID: tn.ID, Label: tn.Label, Type: tn.Type, Outlet: o})
			// An approval gate forwards the result it judged: the nodes it
			// releases consume the handoff too.
			if tn.Type == "human_gate" {
				next := map[string]bool{tn.ID: true}
				for k := range visited {
					next[k] = true
				}
				walk(tn.ID, o, next)
			}
		}
	}
	walk(node.ID, "", map[string]bool{node.ID: true})
	return out
}
