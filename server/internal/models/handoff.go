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
