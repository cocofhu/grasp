package nodereg

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestPromptContractTextAllKinds(t *testing.T) {
	var p *models.AgentPrompts
	types := []string{
		"plan", "implement", "react", "preflight", "research", "test", "review",
		"proposal", "submit_mr", "visual", "app_preview", "agent", "nope",
	}
	for _, typ := range types {
		_ = PromptContractText(p, typ, "feat", "main")
	}
	p = &models.AgentPrompts{PlanContract: "P"}
	if PromptContractText(p, "plan", "", "") != "P" {
		t.Fatal("plan override")
	}
	for _, kind := range []RenderKind{
		RenderClarifiedRequirement, RenderResearch, RenderProposals,
		RenderTestResult, RenderReview, RenderImplementationResult, RenderPreflight, RenderKind(99),
	} {
		_ = Renderer(kind)
	}
}
