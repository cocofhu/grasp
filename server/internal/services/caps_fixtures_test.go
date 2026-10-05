package services

import "github.com/cocofhu/grasp/internal/models"

var (
	testClarifyCaps = &models.AgentCapabilities{
		Interaction: models.InteractionClarify,
		Tools:       []string{models.ToolAskQuestion, models.ToolSetPreview},
		Reads:       []string{"*"},
		Writes:      []models.ProductWrite{{Schema: models.SchemaClarifiedRequirement, Required: true}},
	}
	testReviewCaps = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools:  []string{models.ToolSetPreview},
		Reads:  []string{"*"},
		Writes: []models.ProductWrite{{Schema: models.SchemaResearch, Required: true}},
	}
	testAutoCaps = &models.AgentCapabilities{Interaction: models.InteractionAuto, Reads: []string{"*"}}
)

func agentNode(id, label string, caps *models.AgentCapabilities) models.Node {
	return models.Node{ID: id, Type: "agent", Label: label, Caps: caps}
}
