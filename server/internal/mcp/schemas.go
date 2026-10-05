package mcp

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// PageArtifactName is the reserved single-file HTML page product.
const PageArtifactName = "page.html"

// ProductSchema is the MCP protocol of one structured product: the reserved
// artifact it lives in and the tool that writes it (empty for page.html, which
// is written through write_artifact).
type ProductSchema struct {
	Name         string
	ArtifactName string
	SetTool      string
}

var productSchemas = []ProductSchema{
	{models.SchemaClarifiedRequirement, ClarifiedRequirementArtifactName, "set_clarified_requirement"},
	{models.SchemaPlan, PlanArtifactName, "set_plan"},
	{models.SchemaResearch, ResearchArtifactName, "set_research"},
	{models.SchemaRootCause, RootCauseArtifactName, "set_root_cause"},
	{models.SchemaProposals, ProposalsArtifactName, "set_proposals"},
	{models.SchemaImplementationResult, ImplementationResultArtifactName, "set_implementation_result"},
	{models.SchemaTestResult, TestResultArtifactName, "set_test_result"},
	{models.SchemaReview, ReviewArtifactName, "set_review"},
	{models.SchemaPreflight, PreflightArtifactName, "set_preflight"},
	{models.SchemaPage, PageArtifactName, ""},
}

// ProductSchemas returns every product schema in declaration order.
func ProductSchemas() []ProductSchema {
	return append([]ProductSchema(nil), productSchemas...)
}

// ProductSchemaByName looks a schema up by its name.
func ProductSchemaByName(name string) (ProductSchema, bool) {
	for _, s := range productSchemas {
		if s.Name == name {
			return s, true
		}
	}
	return ProductSchema{}, false
}

func schemaForSetTool(tool string) (ProductSchema, bool) {
	for _, s := range productSchemas {
		if s.SetTool != "" && s.SetTool == tool {
			return s, true
		}
	}
	return ProductSchema{}, false
}

// toolAllowed is the sole capability gate for grantable and set_* tools.
func toolAllowed(caps *models.AgentCapabilities, tool string) bool {
	for _, t := range models.GrantableTools {
		if t == tool {
			return caps.HasTool(tool)
		}
	}
	if s, ok := schemaForSetTool(tool); ok {
		return caps.WritesSchema(s.Name)
	}
	return false
}

// toolListed reports whether tools/list carries a tool for caps: grantable and
// set_* tools only when allowed, every other tool always.
func toolListed(caps *models.AgentCapabilities, tool string) bool {
	if tool == models.ToolAskQuestion && caps.ReviewEnabled() {
		return true
	}
	for _, t := range models.GrantableTools {
		if t == tool {
			return toolAllowed(caps, tool)
		}
	}
	if _, ok := schemaForSetTool(tool); ok {
		return toolAllowed(caps, tool)
	}
	return true
}

// readAllowed reports whether the active Agent may read an artifact: declared
// reads, its own writes, and the platform feedback ledger. Outside an Agent
// node (no active node) reads are unrestricted.
func (h *Host) readAllowed(runID, token, name string) bool {
	name = strings.TrimSpace(name)
	active := h.ActiveNode(runID)
	if active == "" {
		return true
	}
	caps := h.ActiveCaps(runID)
	if caps.CanRead(name) || IsFeedbackArtifactName(name) || name == NodeOutcomeArtifactName {
		return true
	}
	return h.artifactWriterNode(runID, token, name) == active
}

func readDeniedMsg(tool, name string) string {
	return tool + " failed: 当前 Agent 未被授权读取产物 " + name + "(在 Agent 能力的「可读产物」中添加)"
}

func toolDeniedMsg(tool string) string {
	if s, ok := schemaForSetTool(tool); ok {
		return tool + " 不可用:当前 Agent 未声明产物 " + s.Name + "(在 Agent 能力的「写产物」中添加)。"
	}
	return tool + " 不可用:当前 Agent 未被授予该工具(在 Agent 能力的「工具」中添加)。"
}
