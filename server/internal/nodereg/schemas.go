package nodereg

import (
	"fmt"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/mcp/structured"
	"github.com/cocofhu/grasp/internal/models"
)

// Verdict judges a verdict product. planJSON is the run's plan (may be empty).
type Verdict func(content, planJSON string) (pass bool, reason string)

// Schema is one product schema an Agent may write: its MCP protocol, how it
// renders into node outputs, and whether it carries a pass/fail verdict.
type Schema struct {
	mcp.ProductSchema
	Label string
	// Render turns the stored JSON into markdown; nil keeps raw content (page).
	Render func(string) string
	// Verdict is non-nil for products that gate the flow (pass / fail outlets).
	Verdict Verdict
}

// OutputKey is the node output key the product is lifted into.
func (s Schema) OutputKey() string { return s.Name }

var schemaLabels = map[string]string{
	models.SchemaClarifiedRequirement: "需求规格",
	models.SchemaPlan:                 "计划",
	models.SchemaResearch:             "调研",
	models.SchemaRootCause:            "问题根因",
	models.SchemaImplementationResult: "实现结果",
	models.SchemaTestResult:           "测试结果",
	models.SchemaReview:               "评审结论",
	models.SchemaMergeRequest:         "合并请求",
	models.SchemaPreflight:            "环境确认",
	models.SchemaPage:                 "页面稿",
}

var schemaRenderers = map[string]func(string) string{
	models.SchemaClarifiedRequirement: mcp.RenderClarifiedRequirementMarkdown,
	models.SchemaPlan:                 mcp.RenderPlanMarkdown,
	models.SchemaResearch:             mcp.RenderResearchMarkdown,
	models.SchemaRootCause:            mcp.RenderRootCauseMarkdown,
	models.SchemaImplementationResult: mcp.RenderImplementationResultMarkdown,
	models.SchemaTestResult:           mcp.RenderTestResultMarkdown,
	models.SchemaReview:               mcp.RenderReviewMarkdown,
	models.SchemaMergeRequest:         mcp.RenderMergeRequestMarkdown,
	models.SchemaPreflight:            mcp.RenderPreflightMarkdown,
}

var schemaVerdicts = map[string]Verdict{
	models.SchemaTestResult: TestVerdict,
	models.SchemaReview:     func(content, _ string) (bool, string) { return ReviewVerdict(content) },
}

// SchemaByName looks a product schema up.
func SchemaByName(name string) (Schema, bool) {
	ps, ok := mcp.ProductSchemaByName(name)
	if !ok {
		return Schema{}, false
	}
	return Schema{ProductSchema: ps, Label: schemaLabels[name], Render: schemaRenderers[name], Verdict: schemaVerdicts[name]}, true
}

// Schemas returns every product schema in declaration order.
func Schemas() []Schema {
	var out []Schema
	for _, ps := range mcp.ProductSchemas() {
		s, _ := SchemaByName(ps.Name)
		out = append(out, s)
	}
	return out
}

// KnownSchema reports whether name is a product schema.
func KnownSchema(name string) bool {
	_, ok := mcp.ProductSchemaByName(name)
	return ok
}

// ValidateCapabilities validates an Agent's capabilities against the schemas.
func ValidateCapabilities(agent string, caps *models.AgentCapabilities) error {
	if err := caps.Validate(KnownSchema); err != nil {
		return fmt.Errorf("Agent %s %w", agent, err)
	}
	return nil
}

// Gated reports whether an Agent with caps writes a verdict product, which
// gives its node pass / fail outlets.
func Gated(caps *models.AgentCapabilities) bool {
	return len(VerdictSchemas(caps)) > 0
}

// VerdictSchemas lists the declared verdict products in declaration order.
func VerdictSchemas(caps *models.AgentCapabilities) []Schema {
	if caps == nil {
		return nil
	}
	var out []Schema
	for _, w := range caps.Writes {
		if s, ok := SchemaByName(w.Schema); ok && s.Verdict != nil {
			out = append(out, s)
		}
	}
	return out
}

// RequiredSchemas lists the products the Agent must write before finishing.
// root_cause is required exactly when the requirement is a bug (workKind).
func RequiredSchemas(caps *models.AgentCapabilities, workKind string) []Schema {
	if caps == nil {
		return nil
	}
	var out []Schema
	for _, w := range caps.Writes {
		required := w.Required
		if w.Schema == models.SchemaRootCause {
			required = workKind == structured.WorkKindBug
		}
		if !required {
			continue
		}
		if s, ok := SchemaByName(w.Schema); ok {
			out = append(out, s)
		}
	}
	return out
}

// DeclaredSchemas lists every product the Agent declares.
func DeclaredSchemas(caps *models.AgentCapabilities) []Schema {
	if caps == nil {
		return nil
	}
	var out []Schema
	for _, w := range caps.Writes {
		if s, ok := SchemaByName(w.Schema); ok {
			out = append(out, s)
		}
	}
	return out
}
