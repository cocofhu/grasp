package handlers_test

import "github.com/cocofhu/grasp/internal/models"

var (
	testClarifyCaps = &models.AgentCapabilities{
		Interaction: models.InteractionClarify,
		Tools:       []string{models.ToolAskQuestion, models.ToolSetPreview},
		Reads:       []string{"*"},
		Writes: []models.ProductWrite{
			{Schema: models.SchemaClarifiedRequirement, Required: true}, {Schema: models.SchemaPlan, Required: true},
		},
	}
	testPreviewCaps = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools: []string{models.ToolSetPreview},
		Reads: []string{"*"},
	}
	testReviewCaps = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Reads:  []string{"*"},
		Writes: []models.ProductWrite{{Schema: models.SchemaResearch, Required: true}},
	}
	testAutoCaps = &models.AgentCapabilities{Interaction: models.InteractionAuto, Reads: []string{"*"}}
)
