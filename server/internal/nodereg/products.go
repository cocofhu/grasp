package nodereg

import "github.com/cocofhu/grasp/internal/mcp"

// ProductRef describes one deliverable a node type may write.
type ProductRef struct {
	ArtifactName string
	OutputKey    string
	SetTool      string // empty when the artifact is written via write_artifact
	Render       RenderKind
	Required     bool
}

const visualPageName = "page.html"

// ClarifyInteractive reports whether the node type runs a multi-turn ReAct
// clarify dialogue (ask_question + waiting_human inbox), as opposed to an
// autonomous agent or a post-run review phase.
func ClarifyInteractive(nodeType string) bool {
	return nodeType == "react" || IsGrasp(nodeType) || nodeType == "preflight"
}

// RequiredProducts returns the structured/visual deliverables that must exist
// before the node can complete. Empty when the type has no reserved product.
func RequiredProducts(nodeType string) []ProductRef {
	return filterProducts(nodeType, true)
}

// OptionalProducts returns deliverables the node may write; missing ones do
// not fail the node. Written artifacts are still lifted into outputs.
func OptionalProducts(nodeType string) []ProductRef {
	return filterProducts(nodeType, false)
}

func filterProducts(nodeType string, required bool) []ProductRef {
	s, ok := Get(nodeType)
	if !ok {
		return nil
	}
	src := s.Products
	if len(src) == 0 && s.ArtifactName != "" && s.SetTool != "" {
		src = []ProductRef{{
			ArtifactName: s.ArtifactName,
			OutputKey:    s.OutputKey,
			SetTool:      s.SetTool,
			Render:       s.Render,
			Required:     true,
		}}
	}
	var out []ProductRef
	for _, p := range src {
		if p.Required == required {
			out = append(out, p)
		}
	}
	return out
}

func graspProducts() []ProductRef {
	return []ProductRef{
		{
			ArtifactName: mcp.ClarifiedRequirementArtifactName,
			OutputKey:    "clarified_requirement",
			SetTool:      "set_clarified_requirement",
			Render:       RenderClarifiedRequirement,
			Required:     true,
		},
		{
			ArtifactName: mcp.PlanArtifactName,
			OutputKey:    "plan",
			SetTool:      "set_plan",
			Render:       RenderPlan,
			Required:     true,
		},
		{
			ArtifactName: mcp.ResearchArtifactName,
			OutputKey:    "research",
			SetTool:      "set_research",
			Render:       RenderResearch,
			Required:     false,
		},
		{
			// Conditionally required when clarified work_kind=bug; never permanent
			// Required so non-bug Grasp runs are not blocked.
			ArtifactName: mcp.RootCauseArtifactName,
			OutputKey:    "root_cause",
			SetTool:      "set_root_cause",
			Render:       RenderRootCause,
			Required:     false,
		},
		{
			ArtifactName: mcp.ProposalsArtifactName,
			OutputKey:    "proposals",
			SetTool:      "set_proposals",
			Render:       RenderProposals,
			Required:     false,
		},
		{
			ArtifactName: visualPageName,
			OutputKey:    "page",
			SetTool:      "write_artifact",
			Render:       RenderNone,
			Required:     false,
		},
	}
}
