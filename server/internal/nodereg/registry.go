// Package nodereg is the single source of truth for workflow node types: how
// each type executes, which structured product it writes, which sandbox rules
// and runtime prompt contracts apply, and which quality gates block the flow.
package nodereg

import (
	"github.com/cocofhu/grasp/internal/mcp"
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
	ExecPlan
	ExecReact
	ExecStructured
	ExecStructuredGated
	ExecProposalSelect
	ExecSubmitMR
	ExecVisual
	ExecHumanGate
	ExecAppPreview
)

// GateKind selects a structured quality gate (ExecStructuredGated only).
type GateKind int

const (
	GateNone GateKind = iota
	GateTest
	GateReview
)

// PromptContract names the runtime prompt clause injected for agent-class nodes.
type PromptContract int

const (
	PromptNone PromptContract = iota
	PromptPlan
	PromptImplement
	PromptReact
	PromptResearch
	PromptTest
	PromptReview
	PromptProposal
	PromptSubmitMR
	PromptVisual
	PromptAppPreview
	PromptApprove
	PromptPreflight
)

// RenderKind selects the markdown renderer for a structured product.
type RenderKind int

const (
	RenderNone RenderKind = iota
	RenderClarifiedRequirement
	RenderResearch
	RenderProposals
	RenderTestResult
	RenderReview
	RenderImplementationResult
	RenderPlan
	RenderPreflight
)

// Spec describes one workflow node type.
type Spec struct {
	Type     string
	Label    string
	Category string
	Exec     ExecKind

	// Structured product (when Exec is ExecStructured* or finalize path uses it).
	ArtifactName string
	OutputKey    string
	SetTool      string
	Render       RenderKind
	Gate         GateKind

	// EmbeddedRules lists platform rule files (under skills_embed/) for the node.
	EmbeddedRules []string

	Prompt PromptContract

	// ReviewVar names the default global variable that controls the optional
	// post-run ReAct review phase for this node type. A non-empty value marks
	// the node type as review-capable: after its automated run satisfies the
	// product contract, the engine may (per the variable's value) park the live
	// sandbox session and enter an interactive review phase where a human
	// annotates fields/elements and the agent rewrites the product in place.
	// Empty ⇒ the node type has no review phase (e.g. react has its own
	// multi-turn path; gates/control nodes never review).
	ReviewVar string

	// Products lists reserved deliverables when a node writes more than one
	// (Approve). Empty ⇒ fall back to ArtifactName/SetTool/OutputKey/Render.
	Products []ProductRef
}

// defaultReviewVar is the conventional control variable name for the post-run
// review phase (overridable per node via config["review_var"]).
const defaultReviewVar = "review"

var registry = map[string]Spec{
	"input": {
		Type: "input", Label: "输入", Category: "控制", Exec: ExecInput,
	},
	"output": {
		Type: "output", Label: "输出", Category: "控制", Exec: ExecOutput,
	},
	"set_var": {
		Type: "set_var", Label: "赋值", Category: "控制", Exec: ExecSetVar,
	},
	"branch": {
		Type: "branch", Label: "分支", Category: "控制", Exec: ExecBranch,
	},
	"agent": {
		Type: "agent", Label: "通用", Category: "Agent", Exec: ExecAgent,
	},
	"approve": {
		Type: "approve", Label: "Grasp", Category: "Agent", Exec: ExecReact,
		EmbeddedRules: []string{"rules/approve.md"},
		Prompt:        PromptApprove,
		Products:      approveProducts(),
	},
	"plan": {
		Type: "plan", Label: "计划", Category: "Agent", Exec: ExecPlan,
		ArtifactName: mcp.PlanArtifactName, OutputKey: "plan", SetTool: "set_plan",
		EmbeddedRules: []string{"rules/plan.md"},
		Prompt:        PromptPlan,
		ReviewVar:     defaultReviewVar,
	},
	"implement": {
		Type: "implement", Label: "实现", Category: "Agent", Exec: ExecAgent,
		ArtifactName:  mcp.ImplementationResultArtifactName,
		OutputKey:     "implementation_result",
		SetTool:       "set_implementation_result",
		Render:        RenderImplementationResult,
		EmbeddedRules: []string{"rules/implement.md"},
		Prompt:        PromptImplement,
		ReviewVar:     defaultReviewVar,
	},
	"react": {
		Type: "react", Label: "需求澄清", Category: "Agent", Exec: ExecReact,
		ArtifactName:  mcp.ClarifiedRequirementArtifactName,
		OutputKey:     "clarified_requirement",
		SetTool:       "set_clarified_requirement",
		Render:        RenderClarifiedRequirement,
		EmbeddedRules: []string{"rules/react.md"},
		Prompt:        PromptReact,
	},
	"preflight": {
		Type: "preflight", Label: "环境确认", Category: "Agent", Exec: ExecReact,
		ArtifactName:  mcp.PreflightArtifactName,
		OutputKey:     "preflight",
		SetTool:       "set_preflight",
		Render:        RenderPreflight,
		EmbeddedRules: []string{"rules/preflight.md"},
		Prompt:        PromptPreflight,
	},
	"research": {
		Type: "research", Label: "调研", Category: "Agent", Exec: ExecStructured,
		ArtifactName:  mcp.ResearchArtifactName,
		OutputKey:     "research",
		SetTool:       "set_research",
		Render:        RenderResearch,
		EmbeddedRules: []string{"rules/research.md"},
		Prompt:        PromptResearch,
		ReviewVar:     defaultReviewVar,
	},
	"test": {
		Type: "test", Label: "测试", Category: "Agent", Exec: ExecStructuredGated,
		ArtifactName:  mcp.TestResultArtifactName,
		OutputKey:     "test_result",
		SetTool:       "set_test_result",
		Render:        RenderTestResult,
		Gate:          GateTest,
		EmbeddedRules: []string{"rules/test.md"},
		Prompt:        PromptTest,
	},
	"review": {
		Type: "review", Label: "评审", Category: "Agent", Exec: ExecStructuredGated,
		ArtifactName:  mcp.ReviewArtifactName,
		OutputKey:     "review",
		SetTool:       "set_review",
		Render:        RenderReview,
		Gate:          GateReview,
		EmbeddedRules: []string{"rules/review.md"},
		Prompt:        PromptReview,
		ReviewVar:     defaultReviewVar,
	},
	"proposal": {
		Type: "proposal", Label: "方案", Category: "Agent", Exec: ExecStructured,
		ArtifactName:  mcp.ProposalsArtifactName,
		OutputKey:     "proposals",
		SetTool:       "set_proposals",
		Render:        RenderProposals,
		EmbeddedRules: []string{"rules/proposal.md"},
		Prompt:        PromptProposal,
		ReviewVar:     defaultReviewVar,
	},
	"proposal_select": {
		Type: "proposal_select", Label: "方案确认", Category: "门禁", Exec: ExecProposalSelect,
		ArtifactName: mcp.ProposalArtifactName, OutputKey: "proposal",
	},
	"submit_mr": {
		Type: "submit_mr", Label: "提交 MR", Category: "Agent", Exec: ExecSubmitMR,
		Prompt: PromptSubmitMR,
	},
	"visual": {
		Type: "visual", Label: "视觉网页", Category: "Agent", Exec: ExecVisual,
		ArtifactName: "page.html", OutputKey: "page",
		Prompt:    PromptVisual,
		ReviewVar: defaultReviewVar,
	},
	"human_gate": {
		Type: "human_gate", Label: "人工门禁", Category: "门禁", Exec: ExecHumanGate,
	},
	"app_preview": {
		Type: "app_preview", Label: "应用预览", Category: "Agent", Exec: ExecAppPreview,
		Prompt:    PromptAppPreview,
		ReviewVar: defaultReviewVar,
	},
}

// Get returns the spec for a node type.
func Get(nodeType string) (Spec, bool) {
	s, ok := registry[nodeType]
	return s, ok
}

// StructuredProduct returns the reserved artifact name and set_* tool for a
// node type that declares a structured deliverable. Empty strings when none.
func StructuredProduct(nodeType string) (artifactName, setTool string) {
	s, ok := Get(nodeType)
	if !ok || s.SetTool == "" {
		return "", ""
	}
	return s.ArtifactName, s.SetTool
}

// ReviewCapable reports whether a node type supports the post-run ReAct review
// phase (i.e. declares a default review control variable).
func ReviewCapable(nodeType string) bool {
	s, ok := Get(nodeType)
	return ok && s.ReviewVar != ""
}

// DefaultReviewVar returns the node type's default review control variable name
// (empty when the type is not review-capable).
func DefaultReviewVar(nodeType string) string {
	s, ok := Get(nodeType)
	if !ok {
		return ""
	}
	return s.ReviewVar
}

// EmbeddedRuleFiles returns platform rule paths to embed for a node type.
func EmbeddedRuleFiles(nodeType string) []string {
	s, ok := Get(nodeType)
	if !ok {
		return nil
	}
	return append([]string(nil), s.EmbeddedRules...)
}

// Renderer returns the markdown renderer for a structured product, or nil.
func Renderer(kind RenderKind) func(string) string {
	switch kind {
	case RenderClarifiedRequirement:
		return mcp.RenderClarifiedRequirementMarkdown
	case RenderResearch:
		return mcp.RenderResearchMarkdown
	case RenderProposals:
		return mcp.RenderProposalsMarkdown
	case RenderTestResult:
		return mcp.RenderTestResultMarkdown
	case RenderReview:
		return mcp.RenderReviewMarkdown
	case RenderImplementationResult:
		return mcp.RenderImplementationResultMarkdown
	case RenderPlan:
		return mcp.RenderPlanMarkdown
	case RenderPreflight:
		return mcp.RenderPreflightMarkdown
	default:
		return nil
	}
}

// PromptContractText returns the runtime prompt clause for a node type.
func PromptContractText(p *models.AgentPrompts, nodeType, sourceBranch, targetBranch string) string {
	s, ok := Get(nodeType)
	if !ok {
		return ""
	}
	switch s.Prompt {
	case PromptPlan:
		return p.PlanContractText()
	case PromptImplement:
		return p.ImplementContractText() + p.ImplementResultContractText()
	case PromptReact:
		return p.ClarifiedRequirementContractText()
	case PromptResearch:
		return p.ResearchContractText()
	case PromptTest:
		return p.TestContractText()
	case PromptReview:
		return p.ReviewContractText()
	case PromptProposal:
		return p.ProposalContractText()
	case PromptSubmitMR:
		return p.MRContractFor(sourceBranch, targetBranch)
	case PromptVisual:
		return p.VisualContractText()
	case PromptAppPreview:
		return p.PreviewContractText()
	case PromptApprove:
		return p.ApproveContractText()
	case PromptPreflight:
		return p.PreflightContractText()
	default:
		return ""
	}
}
