// Package nodereg is the single source of truth for workflow node types and
// the product schemas Agents write: how each node type executes, how each
// product renders, and which products carry a pass/fail verdict.
package nodereg

import (
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
)

// ExecKind selects the engine executor for a node type.
type ExecKind int

const (
	ExecInput ExecKind = iota
	ExecOutput
	ExecSetVar
	ExecBranch
	ExecAgent
	ExecHumanGate
	ExecProposalSelect
)

// Spec describes one workflow node type.
type Spec struct {
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Category string   `json:"category"`
	Exec     ExecKind `json:"-"`
}

var specs = []Spec{
	{Type: "input", Label: "输入", Category: "控制", Exec: ExecInput},
	{Type: "output", Label: "输出", Category: "控制", Exec: ExecOutput},
	{Type: "set_var", Label: "赋值", Category: "控制", Exec: ExecSetVar},
	{Type: "branch", Label: "分支", Category: "控制", Exec: ExecBranch},
	{Type: "agent", Label: "Agent", Category: "Agent", Exec: ExecAgent},
	{Type: "human_gate", Label: "人工门禁", Category: "门禁", Exec: ExecHumanGate},
	{Type: "proposal_select", Label: "方案确认", Category: "门禁", Exec: ExecProposalSelect},
}

// Get returns the spec for a node type.
func Get(nodeType string) (Spec, bool) {
	for _, s := range specs {
		if s.Type == nodeType {
			return s, true
		}
	}
	return Spec{}, false
}

// Specs returns every node type in palette order.
func Specs() []Spec {
	return append([]Spec(nil), specs...)
}

// ValidateNodeTypes reports the first node whose type is not supported.
func ValidateNodeTypes(g *models.Graph) error {
	if g == nil {
		return nil
	}
	for _, n := range g.Nodes {
		if _, ok := Get(n.Type); !ok {
			return fmt.Errorf("未知节点类型 %s", n.Type)
		}
	}
	return nil
}
