package nodereg

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

func TestNodeTypes(t *testing.T) {
	want := []string{"input", "output", "set_var", "branch", "agent", "human_gate"}
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
	full := `{"summary":"s","cases":[{"name":"a","status":"passed"}],"plan_coverage":[{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]},{"plan_id":"g1.2","passed":true,"evidence":"ok","cases":["a"]}]}`
	if pass, reason := TestVerdict(full, plan); !pass || reason != "" {
		t.Errorf("full coverage: %v %q", pass, reason)
	}
}

// run-224eb8c7: the main turn hit the per-turn limit mid-test and the nudge got
// an interim "still running" result with every leaf claimed passed while the
// full suite, lint/build and E2E were skipped. That must not pass the gate.
func TestTestVerdictRejectsInterimResult(t *testing.T) {
	plan := `{"goals":[{"id":"g1","title":"A","subgoals":[{"id":"g1.1","title":"a"},{"id":"g1.2","title":"b"}]},{"id":"g2","title":"B","subgoals":[{"id":"g2.1","title":"c"},{"id":"g2.2","title":"d"},{"id":"g2.3","title":"e"}]},{"id":"g3","title":"C","subgoals":[{"id":"g3.1","title":"f"},{"id":"g3.2","title":"g"}]}]}`
	cases := `"cases":[{"detail":"npm test -- --run 三个目标测试文件：3 files / 19 tests passed。","name":"[web] 定向回归（19 tests）","status":"passed"},{"detail":"命令已启动但尚未完成，不能据此判定通过。","name":"[web] 完整单元测试与覆盖率","status":"skipped"},{"detail":"尚未执行或完成，不能据此判定通过。","name":"[web] lint、vue-tsc、build 与 Playwright E2E","status":"skipped"}]`
	leaves := []string{"g1.1", "g1.2", "g2.1", "g2.2", "g2.3", "g3.1", "g3.2"}
	cover := func(refs string) string {
		var items []string
		for _, id := range leaves {
			items = append(items, `{"evidence":"目标测试已通过；浏览器验收仍待执行。","passed":true,"plan_id":"`+id+`"`+refs+`}`)
		}
		return `{"summary":"测试执行中",` + cases + `,"skipped":2,"passed":1,"plan_coverage":[` + strings.Join(items, ",") + `]}`
	}
	if pass, reason := TestVerdict(cover(""), plan); pass || !strings.Contains(reason, "未关联任何用例") {
		t.Errorf("as recorded (no case refs): %v %q", pass, reason)
	}
	if pass, reason := TestVerdict(cover(`,"cases":["[web] 定向回归（19 tests）","[web] lint、vue-tsc、build 与 Playwright E2E"]`), plan); pass || !strings.Contains(reason, "未通过(skipped)") {
		t.Errorf("leaf backed by skipped E2E: %v %q", pass, reason)
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
