package models

import (
	"fmt"
	"slices"
	"strings"
)

// Interaction modes an Agent can declare.
const (
	InteractionAuto    = "auto"
	InteractionClarify = "clarify"
)

// Platform tools an Agent may be granted through AgentCapabilities.Tools. The
// set_* product tools are never listed here: they follow from Writes.
const (
	ToolAskQuestion        = "ask_question"
	ToolAskForm            = "ask_form"
	ToolSetArtifactPreview = "set_artifact_preview"
	ToolSetPreview         = "set_preview"
	ToolUpdatePlanStatus   = "update_plan_status"
)

// GrantableTools lists every tool name accepted in AgentCapabilities.Tools.
var GrantableTools = []string{ToolAskQuestion, ToolAskForm, ToolSetArtifactPreview, ToolSetPreview, ToolUpdatePlanStatus}

// Product schema names an Agent may declare in AgentCapabilities.Writes.
const (
	SchemaClarifiedRequirement = "clarified_requirement"
	SchemaPlan                 = "plan"
	SchemaResearch             = "research"
	SchemaRootCause            = "root_cause"
	SchemaProposals            = "proposals"
	SchemaImplementationResult = "implementation_result"
	SchemaTestResult           = "test_result"
	SchemaReview               = "review"
	SchemaPreflight            = "preflight"
	SchemaPage                 = "page"
)

// ProductWrite declares one product an Agent writes.
type ProductWrite struct {
	Schema   string `json:"schema"`
	Required bool   `json:"required,omitempty"`
}

// AgentCapabilities is what an Agent is allowed and expected to do when a
// workflow node runs it. It lives in the Agent's agent.json; at run start the
// engine snapshots it onto each agent node of the run graph (Node.Caps), so
// every consumer reads the same frozen value for the whole run.
type AgentCapabilities struct {
	// Interaction is clarify (multi-turn dialogue with a human, the node stays
	// open until the human confirms) or auto (one autonomous run).
	Interaction string `json:"interaction"`
	// Review parks the session after an auto run so a human can review the
	// result, annotate it and ask for changes before the flow continues.
	Review bool `json:"review,omitempty"`
	// Tools are the grantable platform tools (see GrantableTools).
	Tools []string `json:"tools,omitempty"`
	// Reads lists the artifact names the Agent may read; "*" reads all.
	Reads []string `json:"reads,omitempty"`
	// Writes lists the structured products the Agent writes.
	Writes []ProductWrite `json:"writes,omitempty"`
	// MaxRounds caps the plan-progress re-prompts before finishing (0 = default 3).
	MaxRounds int `json:"maxRounds,omitempty"`
}

// Validate reports the first problem in caps, or nil.
func (c *AgentCapabilities) Validate(knownSchema func(string) bool) error {
	if c == nil {
		return fmt.Errorf("未声明能力")
	}
	switch c.Interaction {
	case InteractionAuto, InteractionClarify:
	default:
		return fmt.Errorf("交互方式 %q 无效(可选 auto / clarify)", c.Interaction)
	}
	for _, t := range c.Tools {
		if !slices.Contains(GrantableTools, t) {
			return fmt.Errorf("工具 %q 不可授权", t)
		}
	}
	if c.Interaction == InteractionClarify && !c.HasTool(ToolAskQuestion) {
		return fmt.Errorf("clarify 交互必须授予 %s", ToolAskQuestion)
	}
	if c.Interaction == InteractionClarify && c.Review {
		return fmt.Errorf("clarify 交互本身由人确认结束,不能再开启复审")
	}
	if c.MaxRounds < 0 {
		return fmt.Errorf("maxRounds 不能为负数")
	}
	seen := map[string]bool{}
	for _, w := range c.Writes {
		if knownSchema != nil && !knownSchema(w.Schema) {
			return fmt.Errorf("未知产物 %q", w.Schema)
		}
		if seen[w.Schema] {
			return fmt.Errorf("产物 %q 重复声明", w.Schema)
		}
		seen[w.Schema] = true
	}
	return nil
}

// Clarify reports whether the Agent runs a multi-turn human dialogue.
func (c *AgentCapabilities) Clarify() bool {
	return c != nil && c.Interaction == InteractionClarify
}

// ReviewEnabled reports whether an auto run parks for human review.
func (c *AgentCapabilities) ReviewEnabled() bool {
	return c != nil && !c.Clarify() && c.Review
}

// Interactive reports whether the node holds a human conversation at all
// (clarify dialogue or post-run review).
func (c *AgentCapabilities) Interactive() bool {
	return c.Clarify() || c.ReviewEnabled()
}

// HasTool reports whether a grantable tool is granted.
func (c *AgentCapabilities) HasTool(name string) bool {
	return c != nil && slices.Contains(c.Tools, name)
}

// CanPreview reports whether the Agent may register a running app preview.
func (c *AgentCapabilities) CanPreview() bool {
	return c.HasTool(ToolSetPreview)
}

// WritesSchema reports whether the Agent declares the product schema.
func (c *AgentCapabilities) WritesSchema(schema string) bool {
	if c == nil {
		return false
	}
	for _, w := range c.Writes {
		if w.Schema == schema {
			return true
		}
	}
	return false
}

// CommitsCode reports whether the Agent's job is to change repositories: it
// writes an implementation result. Every other Agent is a design/assessment
// Agent whose review-phase source edits are never committed.
func (c *AgentCapabilities) CommitsCode() bool {
	return c.WritesSchema(SchemaImplementationResult)
}

// TracksPlanProgress reports whether the Agent executes plan items: it must
// mark every plan leaf done before it may finish.
func (c *AgentCapabilities) TracksPlanProgress() bool {
	return c.HasTool(ToolUpdatePlanStatus)
}

// CanRead reports whether an artifact name is readable.
func (c *AgentCapabilities) CanRead(name string) bool {
	if c == nil {
		return false
	}
	for _, r := range c.Reads {
		r = strings.TrimSpace(r)
		if r == "*" || r == name {
			return true
		}
	}
	return false
}

// Clone returns a deep copy.
func (c *AgentCapabilities) Clone() *AgentCapabilities {
	if c == nil {
		return nil
	}
	out := *c
	out.Tools = slices.Clone(c.Tools)
	out.Reads = slices.Clone(c.Reads)
	out.Writes = slices.Clone(c.Writes)
	return &out
}
