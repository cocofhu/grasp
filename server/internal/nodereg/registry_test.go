package nodereg

import (
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
)

func TestRegistryStructuredProducts(t *testing.T) {
	cases := map[string]struct{ artifact, tool string }{
		"research": {mcp.ResearchArtifactName, "set_research"},
		"test":     {mcp.TestResultArtifactName, "set_test_result"},
		"review":   {mcp.ReviewArtifactName, "set_review"},
		"plan":     {mcp.PlanArtifactName, "set_plan"},
	}
	for typ, want := range cases {
		a, tool := StructuredProduct(typ)
		if a != want.artifact || tool != want.tool {
			t.Fatalf("%s: got (%q,%q) want (%q,%q)", typ, a, tool, want.artifact, want.tool)
		}
	}
	if _, tool := StructuredProduct("agent"); tool != "" {
		t.Fatalf("agent should have no structured tool, got %q", tool)
	}
}

func TestBuildManifestMatchesRegistry(t *testing.T) {
	m := BuildManifest()
	if m.OutputKeyToArtifact["research"] != mcp.ResearchArtifactName {
		t.Fatal("research mapping")
	}
	if m.OutputKeyToArtifact["proposal"] != mcp.ProposalArtifactName {
		t.Fatal("selected proposal mapping")
	}
	if m.ArtifactToOutputJSON[mcp.ProposalArtifactName] != "proposal_json" {
		t.Fatal("selected proposal json key")
	}
	if m.ArtifactToOutputJSON[mcp.TestResultArtifactName] != "test_result_json" {
		t.Fatal("test json key")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown type")
	}
	for i := 1; i < len(m.Products); i++ {
		if m.Products[i-1].Type > m.Products[i].Type {
			t.Fatalf("products not sorted: %q after %q", m.Products[i].Type, m.Products[i-1].Type)
		}
	}
	// Rebuilding must keep the same stable product order (map iteration noise).
	again := BuildManifest()
	if len(again.Products) != len(m.Products) {
		t.Fatalf("rebuild len=%d want %d", len(again.Products), len(m.Products))
	}
	for i := range m.Products {
		if again.Products[i].Type != m.Products[i].Type {
			t.Fatalf("rebuild order drift at %d: %q vs %q", i, again.Products[i].Type, m.Products[i].Type)
		}
	}
}

func TestEmbeddedRules(t *testing.T) {
	rules := EmbeddedRuleFiles("test")
	if len(rules) != 1 || rules[0] != "rules/test.md" {
		t.Fatalf("test rules: %v", rules)
	}
	if len(EmbeddedRuleFiles("branch")) != 0 {
		t.Fatal("branch should have no embedded rules")
	}
	grasp := EmbeddedRuleFiles("grasp")
	if len(grasp) != 1 || grasp[0] != "rules/grasp.md" {
		t.Fatalf("grasp rules: %v", grasp)
	}
	alias := EmbeddedRuleFiles("approve")
	if len(alias) != 1 || alias[0] != "rules/grasp.md" {
		t.Fatalf("approve alias rules: %v", alias)
	}
}

func TestIsGrasp(t *testing.T) {
	if !IsGrasp("grasp") || !IsGrasp("approve") {
		t.Fatal("grasp and approve should be IsGrasp")
	}
	if IsGrasp("react") || IsGrasp("human_gate") {
		t.Fatal("react/human_gate must not be IsGrasp")
	}
}

func TestClarifyInteractive(t *testing.T) {
	if !ClarifyInteractive("react") || !ClarifyInteractive("approve") || !ClarifyInteractive("grasp") || !ClarifyInteractive("preflight") {
		t.Fatal("react, grasp/approve and preflight should be clarify-interactive")
	}
	if ClarifyInteractive("agent") || ClarifyInteractive("plan") || ClarifyInteractive("research") {
		t.Fatal("agent/plan/research must not be clarify-interactive")
	}
}

func TestRequiredProductsGrasp(t *testing.T) {
	req := RequiredProducts("grasp")
	if len(req) != 2 {
		t.Fatalf("RequiredProducts(grasp) len=%d want 2", len(req))
	}
	if req[0].ArtifactName != mcp.ClarifiedRequirementArtifactName || req[0].SetTool != "set_clarified_requirement" {
		t.Fatalf("first required = %+v", req[0])
	}
	if req[1].ArtifactName != mcp.PlanArtifactName || req[1].SetTool != "set_plan" {
		t.Fatalf("second required = %+v", req[1])
	}
	opt := OptionalProducts("grasp")
	if len(opt) != 4 {
		t.Fatalf("OptionalProducts(grasp) len=%d want 4", len(opt))
	}
}

// frontendNodeTypes mirrors web/src/lib/types.ts NodeType union.
var frontendNodeTypes = []string{
	"input", "output", "react", "preflight", "agent", "grasp", "plan", "implement",
	"research", "test", "review", "proposal", "proposal_select",
	"submit_mr", "visual", "human_gate", "app_preview", "branch", "set_var",
}

func TestRegistryCoversFrontendNodeTypes(t *testing.T) {
	known := map[string]bool{}
	for _, t := range knownTypes() {
		known[t] = true
	}
	for _, typ := range frontendNodeTypes {
		if !known[typ] {
			t.Fatalf("backend registry missing frontend node type %q", typ)
		}
	}
	if len(knownTypes()) != len(frontendNodeTypes) {
		t.Fatalf("registry has %d types, frontend expects %d", len(knownTypes()), len(frontendNodeTypes))
	}
}

func TestStructuredNodesHaveRendererAndTool(t *testing.T) {
	for _, typ := range []string{"react", "preflight", "research", "test", "review", "proposal", "implement"} {
		s, ok := Get(typ)
		if !ok {
			t.Fatalf("missing %s", typ)
		}
		if s.SetTool == "" || s.ArtifactName == "" || s.OutputKey == "" {
			t.Fatalf("%s missing structured fields: %+v", typ, s)
		}
		if Renderer(s.Render) == nil {
			t.Fatalf("%s missing renderer", typ)
		}
	}
}

func TestGatedNodes(t *testing.T) {
	if s, _ := Get("test"); s.Gate != GateTest {
		t.Fatal("test gate kind")
	}
	if s, _ := Get("review"); s.Gate != GateReview {
		t.Fatal("review gate kind")
	}
}

func TestPromptContractText(t *testing.T) {
	if PromptContractText(nil, "research", "", "") == "" {
		t.Fatal("research contract")
	}
	if PromptContractText(nil, "approve", "", "") == "" {
		t.Fatal("approve contract")
	}
	if PromptContractText(nil, "preflight", "", "") == "" {
		t.Fatal("preflight contract")
	}
	if PromptContractText(nil, "agent", "", "") != "" {
		t.Fatal("agent should have no fixed contract")
	}
}

func TestReviewCapableDefaults(t *testing.T) {
	// test is intentionally excluded: it already has a structured gate verdict
	// path and is not part of the post-run ReAct review surface.
	for _, typ := range []string{"plan", "implement", "research", "review", "proposal", "visual", "app_preview"} {
		if !ReviewCapable(typ) {
			t.Fatalf("%s should be review-capable", typ)
		}
		if DefaultReviewVar(typ) != "review" {
			t.Fatalf("%s DefaultReviewVar = %q, want review", typ, DefaultReviewVar(typ))
		}
	}
	for _, typ := range []string{"test", "react", "preflight", "agent", "approve", "human_gate", "proposal_select", "input", "nope"} {
		if ReviewCapable(typ) {
			t.Fatalf("%s must not be review-capable", typ)
		}
		if DefaultReviewVar(typ) != "" {
			t.Fatalf("%s DefaultReviewVar should be empty", typ)
		}
	}
}
