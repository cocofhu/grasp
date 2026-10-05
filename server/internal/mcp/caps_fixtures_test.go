package mcp

import "github.com/cocofhu/grasp/internal/models"

func capsWriting(schemas ...string) *models.AgentCapabilities {
	c := &models.AgentCapabilities{Interaction: models.InteractionAuto, Reads: []string{"*"}}
	for _, s := range schemas {
		c.Writes = append(c.Writes, models.ProductWrite{Schema: s, Required: true})
	}
	return c
}

func capsReviewWriting(schemas ...string) *models.AgentCapabilities {
	c := capsWriting(schemas...)
	c.Review = true
	return c
}

var (
	capsPlain   = capsWriting()
	capsClarify = &models.AgentCapabilities{
		Interaction: models.InteractionClarify,
		Tools:       []string{models.ToolAskQuestion, models.ToolSetArtifactPreview, models.ToolSetPreview},
		Reads:       []string{"*"},
		Writes: []models.ProductWrite{
			{Schema: models.SchemaClarifiedRequirement, Required: true}, {Schema: models.SchemaPlan, Required: true},
			{Schema: models.SchemaResearch}, {Schema: models.SchemaProposals}, {Schema: models.SchemaPage},
		},
	}
	capsClarifyRootCause = func() *models.AgentCapabilities {
		c := capsClarify.Clone()
		c.Writes = append(c.Writes, models.ProductWrite{Schema: models.SchemaRootCause})
		return c
	}()
	capsPreflight = &models.AgentCapabilities{
		Interaction: models.InteractionClarify,
		Tools:       []string{models.ToolAskQuestion, models.ToolAskForm, models.ToolSetArtifactPreview},
		Reads:       []string{"*"},
		Writes:      []models.ProductWrite{{Schema: models.SchemaPreflight, Required: true}},
	}
	capsImplement = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools:  []string{models.ToolSetPreview, models.ToolUpdatePlanStatus},
		Reads:  []string{"*"},
		Writes: []models.ProductWrite{{Schema: models.SchemaImplementationResult, Required: true}},
	}
	capsTestReview = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools: []string{models.ToolSetPreview},
		Reads: []string{"*"},
		Writes: []models.ProductWrite{
			{Schema: models.SchemaTestResult, Required: true}, {Schema: models.SchemaReview, Required: true},
		},
	}
	capsPlanReview = func() *models.AgentCapabilities {
		c := capsWriting(models.SchemaPlan)
		c.Review = true
		return c
	}()
	capsPreview = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools: []string{models.ToolSetPreview},
		Reads: []string{"*"},
	}
)
