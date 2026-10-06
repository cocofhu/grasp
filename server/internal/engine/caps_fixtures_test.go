package engine

import "github.com/cocofhu/grasp/internal/models"

func writes(required bool, schemas ...string) []models.ProductWrite {
	out := make([]models.ProductWrite, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, models.ProductWrite{Schema: s, Required: required})
	}
	return out
}

var (
	capsClarify = &models.AgentCapabilities{
		Interaction: models.InteractionClarify,
		Tools:       []string{models.ToolAskQuestion, models.ToolSetPreview},
		Reads:       []string{"*"},
		Writes: append(writes(true, models.SchemaClarifiedRequirement, models.SchemaPlan),
			writes(false, models.SchemaResearch)...),
	}
	capsResearch = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Reads: []string{"*"}, Writes: writes(true, models.SchemaResearch),
	}
	capsPage = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Reads: []string{"*"}, Writes: writes(true, models.SchemaPage),
	}
	capsPreview = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Review: true,
		Tools: []string{models.ToolSetPreview}, Reads: []string{"*"},
	}
	capsPlan = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Reads: []string{"*"}, Writes: writes(true, models.SchemaPlan),
	}
	capsImplement = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Reads: []string{"*"}, Writes: writes(true, models.SchemaImplementationResult),
	}
	capsTest = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Reads: []string{"*"}, Writes: writes(true, models.SchemaTestResult),
	}
	capsCodeReview = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Reads: []string{"*"}, Writes: writes(true, models.SchemaReview),
	}
	capsPlain = &models.AgentCapabilities{Interaction: models.InteractionAuto, Reads: []string{"*"}}
)

// Auto variants of the review-enabled fixtures, for flows that must run
// through without parking for human review.
var (
	capsResearchAuto = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Reads: []string{"*"}, Writes: writes(true, models.SchemaResearch),
	}
	capsPageAuto = &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Reads: []string{"*"}, Writes: writes(true, models.SchemaPage),
	}
)
