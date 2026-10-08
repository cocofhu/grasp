package models

import (
	"errors"
	"fmt"
	"strings"
)

// Graph is the workflow definition body (mirrors the frontend WFNode/WFEdge
// shape). It is stored as a JSON column on WorkflowDef / WorkflowVersion.
type Graph struct {
	Nodes     []Node     `json:"nodes"`
	Edges     []Edge     `json:"edges"`
	Variables []Variable `json:"variables,omitempty"`
}

// Node is a single FSM state.
type Node struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Label      string         `json:"label"`
	Position   Position       `json:"position"`
	Config     map[string]any `json:"config"`
	Checkpoint bool           `json:"checkpoint,omitempty"`
	// Caps is the agent node's Agent capabilities, snapshotted at run start.
	// Workflow definitions never carry it.
	Caps *AgentCapabilities `json:"caps,omitempty"`
}

// Position is the canvas coordinate (carried for round-tripping the UI).
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// EdgeKind enumerates FSM transition semantics.
type EdgeKind string

const (
	EdgeSuccess  EdgeKind = "success"
	EdgeFailure  EdgeKind = "failure"
	EdgeRollback EdgeKind = "rollback"
)

// Edge is an FSM transition between two states.
//
// SourceHandle names the source node outlet the edge leaves from. Nodes with
// named outlets (branch cases, gate agents' pass/fail, human gate actions)
// report the taken outlet as outputs.action; the engine follows the edge whose
// SourceHandle equals it. Edges without a handle leave the default outlet.
type Edge struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	SourceHandle string   `json:"sourceHandle,omitempty"`
	Target       string   `json:"target"`
	When         string   `json:"when,omitempty"`
	Label        string   `json:"label,omitempty"`
	Kind         EdgeKind `json:"kind,omitempty"`
	Carry        []string `json:"carry,omitempty"`
	MaxAttempts  int      `json:"maxAttempts,omitempty"`
}

// KindOrDefault returns the transition kind; an omitted kind means success.
func (e Edge) KindOrDefault() EdgeKind {
	if e.Kind == "" {
		return EdgeSuccess
	}
	return e.Kind
}

// Variable is a workflow-level global variable definition. All run state is a
// global variable: those with Ask=true are collected at run start (the former
// "input fields"), the rest are engine-seeded working state mutated by set_var.
type Variable struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // string | paragraph | number | bool | select
	Value    any    `json:"value,omitempty"`
	Desc     string `json:"desc,omitempty"`     // label shown when collected at start
	Ask      bool   `json:"ask,omitempty"`      // collect from the launcher at run start
	Required bool   `json:"required,omitempty"` // ask: must be provided
	Editable bool   `json:"editable,omitempty"` // ask: launcher may override the default
	Options  string `json:"options,omitempty"`  // select: comma-separated choices
}

// AcpEvent is one streamed agent event (mirrors the frontend AcpEvent).
type AcpEvent struct {
	T        int           `json:"t"`
	Kind     string        `json:"kind"` // message|thought|plan|tool_call|commands|segment|prompt|turn_end
	Title    string        `json:"title,omitempty"`
	Text     string        `json:"text,omitempty"`
	Status   string        `json:"status,omitempty"`
	Artifact *ArtifactMeta `json:"artifact,omitempty"`
	// At is the RFC3339 wall time; set on prompt (turn start) and turn_end.
	At string `json:"at,omitempty"`
	// Usage is the turn's token accounting; set on turn_end only.
	Usage *TokenUsage `json:"usage,omitempty"`
	// Truncated marks a prompt whose Text was cut (storage cap or DTO preview).
	Truncated bool `json:"truncated,omitempty"`
	// Parts is set on kind=timeline only: the open agent row as it happened
	// (thought, tool and message steps interleaved).
	Parts []AcpPart `json:"parts,omitempty"`
}

// AcpKindTimeline carries the ordered steps of the open agent row. It follows
// the flat thought/tool_call/message events, which stay for older consumers.
const AcpKindTimeline = "timeline"

// AcpPart is one step of an agent reply.
type AcpPart struct {
	Kind string `json:"kind"` // thought|message|tool
	Text string `json:"text,omitempty"`
	// Tool steps only. Summary is the redacted one-line argument (command,
	// path, URL…); Input / Output are redacted, truncated details.
	Title   string `json:"title,omitempty"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
	Input   string `json:"input,omitempty"`
	Output  string `json:"output,omitempty"`
	// DurationMs is how long a finished tool ran (0 = unknown).
	DurationMs int64 `json:"durationMs,omitempty"`
}

// PartsForReply returns the steps of the last timeline event (the row the
// tools and narration of a persisted agent turn belong to), or nil. When the
// stored reply text differs from the streamed narration (the engine cleaned
// it), message steps are replaced by one final step carrying text, so the
// timeline never shows what the reply itself dropped.
func PartsForReply(events []AcpEvent, text string) []AcpPart {
	var parts []AcpPart
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == AcpKindTimeline {
			parts = events[i].Parts
			break
		}
	}
	if len(parts) == 0 {
		return nil
	}
	var said strings.Builder
	for _, p := range parts {
		if p.Kind == "message" {
			said.WriteString(p.Text)
		}
	}
	if strings.TrimSpace(said.String()) == strings.TrimSpace(text) {
		return parts
	}
	out := make([]AcpPart, 0, len(parts)+1)
	for _, p := range parts {
		if p.Kind != "message" {
			out = append(out, p)
		}
	}
	if strings.TrimSpace(text) != "" {
		out = append(out, AcpPart{Kind: "message", Text: text})
	}
	return out
}

// Transcript-only event kinds appended around each persisted chat turn.
const (
	AcpKindPrompt  = "prompt"
	AcpKindTurnEnd = "turn_end"
)

// ArtifactMeta marks an event as an artifact-store write.
type ArtifactMeta struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// TraceEntry is one FSM state-trace event (enter/exit/transition/rollback).
// Iteration is the per-node execution index at the time of the event (set on
// enter), letting the UI label repeated visits ("第 N 次执行").
type TraceEntry struct {
	At        string   `json:"at"`
	NodeID    string   `json:"nodeId"`
	Event     string   `json:"event"`
	Iteration int      `json:"iteration,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	Kind      EdgeKind `json:"kind,omitempty"`
	To        string   `json:"to,omitempty"`
}

// FindNode returns the node with the given id, or nil.
func (g Graph) FindNode(id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// OutEdges returns edges leaving the given node, preserving definition order.
func (g Graph) OutEdges(nodeID string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.Source == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// StartNode returns the entry state: the input node if present, else the
// node with no incoming edges, else the first node.
func (g Graph) StartNode() *Node {
	if n := g.firstOfType("input"); n != nil {
		return n
	}
	incoming := map[string]bool{}
	for _, e := range g.Edges {
		incoming[e.Target] = true
	}
	for i := range g.Nodes {
		if !incoming[g.Nodes[i].ID] {
			return &g.Nodes[i]
		}
	}
	if len(g.Nodes) > 0 {
		return &g.Nodes[0]
	}
	return nil
}

// Validate enforces the structural contract for a runnable workflow: it must
// have exactly one input node (the start) and at least one output node (the
// end), with the input having no incoming edges and outputs no outgoing edges,
// so the workflow always begins at the input and terminates at an output.
func (g Graph) Validate() error {
	if len(g.Nodes) == 0 {
		return errors.New("工作流为空:至少需要一个输入节点和一个输出节点")
	}
	var inputs, outputs []Node
	for _, n := range g.Nodes {
		switch n.Type {
		case "input":
			inputs = append(inputs, n)
		case "output":
			outputs = append(outputs, n)
		}
	}
	if len(inputs) == 0 {
		return errors.New("缺少输入节点:工作流必须有且仅有一个输入节点作为起点")
	}
	if len(inputs) > 1 {
		return errors.New("输入节点过多:工作流只能有一个输入节点")
	}
	if len(outputs) == 0 {
		return errors.New("缺少输出节点:工作流必须至少有一个输出节点作为终点")
	}
	incoming := map[string]bool{}
	outgoing := map[string]bool{}
	for _, e := range g.Edges {
		incoming[e.Target] = true
		outgoing[e.Source] = true
	}
	if incoming[inputs[0].ID] {
		return errors.New("输入节点不能有入边:工作流必须从输入节点开始")
	}
	for _, o := range outputs {
		if outgoing[o.ID] {
			return errors.New("输出节点不能有出边:工作流必须在输出节点结束")
		}
	}
	for _, e := range g.Edges {
		if g.FindNode(e.Source) == nil || g.FindNode(e.Target) == nil {
			return fmt.Errorf("连线 %s 指向不存在的节点", e.ID)
		}
	}
	for _, n := range g.Nodes {
		if err := validateNodeLimits(n); err != nil {
			return err
		}
	}
	return g.validateSuccessFanout()
}

// MaxNudgeRetries bounds a node's config.nudgeRetries.
const MaxNudgeRetries = 10

// validateNodeLimits checks the run-limit knobs a node may carry: timeout
// (node total minutes; 0 or absent = unlimited) and nudgeRetries (re-prompts
// per kind, 0..MaxNudgeRetries; absent = default).
func validateNodeLimits(n Node) error {
	name := n.Label
	if name == "" {
		name = n.ID
	}
	if v, ok := n.Config["timeout"]; ok && v != nil {
		f, isNum := v.(float64)
		if iv, isInt := v.(int); isInt {
			f, isNum = float64(iv), true
		}
		if !isNum || f < 0 {
			return fmt.Errorf("节点 %s 的总时限需为不小于 0 的分钟数", name)
		}
	}
	if v, ok := n.Config["nudgeRetries"]; ok && v != nil {
		f, isNum := v.(float64)
		if iv, isInt := v.(int); isInt {
			f, isNum = float64(iv), true
		}
		if !isNum || f != float64(int(f)) || f < 0 || f > MaxNudgeRetries {
			return fmt.Errorf("节点 %s 的催写次数需为 0-%d 之间的整数", name, MaxNudgeRetries)
		}
	}
	return nil
}

// validateSuccessFanout rejects an ambiguous success fan-out: more than one
// unconditional (no `when` guard) success edge leaving the same outlet. The
// FSM takes exactly one outgoing edge per node — it never forks — so two
// guardless edges on one outlet mean only the first ever runs.
func (g Graph) validateSuccessFanout() error {
	type outlet struct{ source, handle string }
	counts := map[outlet]int{}
	for _, e := range g.Edges {
		if e.KindOrDefault() != EdgeSuccess || strings.TrimSpace(e.When) != "" {
			continue
		}
		key := outlet{e.Source, e.SourceHandle}
		counts[key]++
		if counts[key] > 1 {
			name := e.Source
			if n := g.FindNode(e.Source); n != nil && strings.TrimSpace(n.Label) != "" {
				name = n.Label
			}
			return fmt.Errorf("节点「%s」的同一个出口有多条无条件连线:工作流一次只会走其中一条,其余永远不会执行(不是并行分叉)。请给连线设置条件,或删除多余的连线", name)
		}
	}
	return nil
}

func (g Graph) firstOfType(t string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].Type == t {
			return &g.Nodes[i]
		}
	}
	return nil
}
