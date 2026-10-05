package nodereg

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

func TestNodeTypes(t *testing.T) {
	want := []string{"input", "output", "set_var", "branch", "agent", "human_gate", "proposal_select"}
	specs := Specs()
	if len(specs) != len(want) {
		t.Fatalf("specs = %d, want %d", len(specs), len(want))
	}
	for i, s := range specs {
		if s.Type != want[i] {
			t.Fatalf("spec[%d] = %s, want %s", i, s.Type, want[i])
		}
		if got, ok := Get(s.Type); !ok || got.Exec != s.Exec {
			t.Fatalf("Get(%s) = %+v %v", s.Type, got, ok)
		}
	}
	if _, ok := Get("react"); ok {
		t.Fatal("retired node types must not resolve")
	}
}

func TestValidateNodeTypes(t *testing.T) {
	if err := ValidateNodeTypes(nil); err != nil {
		t.Fatalf("nil graph: %v", err)
	}
	ok := &models.Graph{Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "a", Type: "agent"}}}
	if err := ValidateNodeTypes(ok); err != nil {
		t.Fatalf("valid graph: %v", err)
	}
	bad := &models.Graph{Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "r", Type: "research"}}}
	if err := ValidateNodeTypes(bad); err == nil || err.Error() != "未知节点类型 research" {
		t.Fatalf("retired type: %v", err)
	}
}

func TestValidateCapabilities(t *testing.T) {
	good := &models.AgentCapabilities{Interaction: models.InteractionAuto,
		Writes: []models.ProductWrite{{Schema: models.SchemaTestResult, Required: true}}}
	if err := ValidateCapabilities("A", good); err != nil {
		t.Fatalf("valid caps: %v", err)
	}
	if err := ValidateCapabilities("A", nil); err == nil || !strings.Contains(err.Error(), "Agent A 未声明能力") {
		t.Fatalf("nil caps: %v", err)
	}
	unknown := &models.AgentCapabilities{Interaction: models.InteractionAuto,
		Writes: []models.ProductWrite{{Schema: "nope"}}}
	if err := ValidateCapabilities("A", unknown); err == nil || !strings.Contains(err.Error(), "未知产物") {
		t.Fatalf("unknown schema: %v", err)
	}
}

func TestSchemaLookups(t *testing.T) {
	for _, s := range Schemas() {
		if !KnownSchema(s.Name) {
			t.Fatalf("schema %s not known", s.Name)
		}
		if s.Label == "" {
			t.Fatalf("schema %s has no label", s.Name)
		}
		if s.OutputKey() != s.Name {
			t.Fatalf("output key of %s = %s", s.Name, s.OutputKey())
		}
	}
	if KnownSchema("nope") {
		t.Fatal("unknown schema reported known")
	}
	if _, ok := SchemaByName("nope"); ok {
		t.Fatal("SchemaByName(nope)")
	}
	page, _ := SchemaByName(models.SchemaPage)
	if page.Render != nil || page.ArtifactName != mcp.PageArtifactName {
		t.Fatalf("page schema = %+v", page)
	}
}

func TestCapabilitySchemas(t *testing.T) {
	caps := &models.AgentCapabilities{Interaction: models.InteractionAuto, Writes: []models.ProductWrite{
		{Schema: models.SchemaTestResult, Required: true},
		{Schema: models.SchemaReview, Required: true},
		{Schema: models.SchemaRootCause},
		{Schema: models.SchemaResearch},
	}}
	if !Gated(caps) || Gated(nil) {
		t.Fatal("Gated")
	}
	if Gated(&models.AgentCapabilities{Writes: []models.ProductWrite{{Schema: models.SchemaPlan}}}) {
		t.Fatal("plan carries no verdict")
	}
	if v := VerdictSchemas(caps); len(v) != 2 || v[0].Name != models.SchemaTestResult || v[1].Name != models.SchemaReview {
		t.Fatalf("verdicts = %+v", v)
	}
	if len(DeclaredSchemas(caps)) != 4 || DeclaredSchemas(nil) != nil || VerdictSchemas(nil) != nil {
		t.Fatal("DeclaredSchemas")
	}
	names := func(ss []Schema) string {
		var out []string
		for _, s := range ss {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(RequiredSchemas(caps, "feature")); got != "test_result,review" {
		t.Fatalf("feature required = %s", got)
	}
	if got := names(RequiredSchemas(caps, "bug")); got != "test_result,review,root_cause" {
		t.Fatalf("bug required = %s", got)
	}
	if RequiredSchemas(nil, "bug") != nil {
		t.Fatal("nil caps required")
	}
}

func TestTestVerdict(t *testing.T) {
	if pass, reason := TestVerdict(`{"summary":"s","skipped":1,"cases":[{"name":"x","status":"skipped"}]}`, ""); !pass || reason != "" {
		t.Errorf("skipped-only passes: %v %q", pass, reason)
	}
	if pass, reason := TestVerdict(`{"summary":"s","failed":1,"cases":[{"name":"x","status":"failed"}]}`, ""); pass || !strings.Contains(reason, "失败") {
		t.Errorf("failed blocks: %v %q", pass, reason)
	}
	if pass, _ := TestVerdict(`{bad`, ""); pass {
		t.Error("malformed must fail")
	}
	plan := `{"goals":[{"id":"g1","title":"A","subgoals":[{"id":"g1.1","title":"x"},{"id":"g1.2","title":"y"}]}]}`
	if pass, reason := TestVerdict(`{"summary":"s","cases":[{"name":"a","status":"passed"}]}`, plan); pass || !strings.Contains(reason, "plan_coverage") {
		t.Errorf("missing coverage: %v %q", pass, reason)
	}
	full := `{"summary":"s","plan_coverage":[{"plan_id":"g1.1","passed":true,"evidence":"ok"},{"plan_id":"g1.2","passed":true,"evidence":"ok"}]}`
	if pass, reason := TestVerdict(full, plan); !pass || reason != "" {
		t.Errorf("full coverage: %v %q", pass, reason)
	}
}

func TestReviewVerdict(t *testing.T) {
	cases := map[string]bool{
		`{"summary":"s","verdict":"approve"}`:         true,
		`{"summary":"s","verdict":"request_changes"}`: false,
		`{"summary":"s","verdict":"reject"}`:          false,
		`{"verdict":"bogus"}`:                         false,
	}
	for body, want := range cases {
		pass, reason := ReviewVerdict(body)
		if pass != want || (!pass && reason == "") {
			t.Errorf("%s: pass=%v reason=%q", body, pass, reason)
		}
	}
}

func TestBuildManifest(t *testing.T) {
	m := BuildManifest()
	if len(m.NodeTypes) != len(Specs()) || len(m.Schemas) != len(Schemas()) || len(m.Tools) == 0 {
		t.Fatalf("manifest = %+v", m)
	}
	for _, s := range m.Schemas {
		if m.OutputKeyToArtifact[s.OutputKey] != s.ArtifactName {
			t.Fatalf("output key map for %s", s.Name)
		}
		if s.Name == models.SchemaPage && s.OutputJSONKey != "" {
			t.Fatal("page has no rendered JSON output")
		}
		if (s.Name == models.SchemaTestResult) != s.Verdict && s.Name != models.SchemaReview {
			t.Fatalf("verdict flag for %s", s.Name)
		}
	}
	if m.ArtifactToOutputJSON[mcp.ResearchArtifactName] != "research_json" {
		t.Fatalf("artifact json map = %v", m.ArtifactToOutputJSON)
	}
}
