package runtime

import "github.com/cocofhu/grasp/internal/models"

var (
	testClarifyCaps = &models.AgentCapabilities{
		Interaction: models.InteractionClarify,
		Tools:       []string{models.ToolAskQuestion, models.ToolSetArtifactPreview, models.ToolSetPreview},
		Reads:       []string{"*"},
		Writes: []models.ProductWrite{
			{Schema: models.SchemaClarifiedRequirement, Required: true}, {Schema: models.SchemaPlan, Required: true},
			{Schema: models.SchemaResearch}, {Schema: models.SchemaPage},
		},
	}
	testImplementCaps = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools:  []string{models.ToolSetPreview, models.ToolUpdatePlanStatus},
		Reads:  []string{"*"},
		Writes: []models.ProductWrite{{Schema: models.SchemaImplementationResult, Required: true}},
	}
	testTestReviewCaps = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools: []string{models.ToolSetPreview},
		Reads: []string{"*"},
		Writes: []models.ProductWrite{
			{Schema: models.SchemaTestResult, Required: true}, {Schema: models.SchemaReview, Required: true},
		},
	}
	testPreviewCaps = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools: []string{models.ToolSetPreview},
		Reads: []string{"*"},
	}
	testPlainCaps = &models.AgentCapabilities{Interaction: models.InteractionAuto, Reads: []string{"*"}}
)

func testCapsWriting(schemas ...string) *models.AgentCapabilities {
	c := &models.AgentCapabilities{Interaction: models.InteractionAuto, Reads: []string{"*"}}
	for _, s := range schemas {
		c.Writes = append(c.Writes, models.ProductWrite{Schema: s, Required: true})
	}
	return c
}
