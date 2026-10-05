package models

import (
	"strings"
	"testing"
)

func knownTestSchema(s string) bool {
	return s == SchemaPlan || s == SchemaTestResult || s == SchemaImplementationResult
}

func TestAgentCapabilitiesValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps *AgentCapabilities
		want string
	}{
		{"nil", nil, "未声明能力"},
		{"bad interaction", &AgentCapabilities{Interaction: "chat"}, "交互方式"},
		{"bad tool", &AgentCapabilities{Interaction: InteractionAuto, Tools: []string{"submit_mr"}}, "不可授权"},
		{"clarify without ask", &AgentCapabilities{Interaction: InteractionClarify}, "必须授予"},
		{"clarify with review", &AgentCapabilities{Interaction: InteractionClarify, Review: true, Tools: []string{ToolAskQuestion}}, "不能再开启复审"},
		{"negative rounds", &AgentCapabilities{Interaction: InteractionAuto, MaxRounds: -1}, "maxRounds"},
		{"unknown schema", &AgentCapabilities{Interaction: InteractionAuto, Writes: []ProductWrite{{Schema: "nope"}}}, "未知产物"},
		{"duplicate schema", &AgentCapabilities{Interaction: InteractionAuto, Writes: []ProductWrite{{Schema: SchemaPlan}, {Schema: SchemaPlan}}}, "重复声明"},
		{"ok", &AgentCapabilities{Interaction: InteractionAuto, Review: true, Tools: []string{ToolSetPreview}, Writes: []ProductWrite{{Schema: SchemaPlan, Required: true}}}, ""},
	} {
		err := tc.caps.Validate(knownTestSchema)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected %v", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: err=%v want %q", tc.name, err, tc.want)
		}
	}
	if err := (&AgentCapabilities{Interaction: InteractionAuto, Writes: []ProductWrite{{Schema: "any"}}}).Validate(nil); err != nil {
		t.Errorf("nil knownSchema accepts any schema: %v", err)
	}
}

func TestAgentCapabilitiesPredicates(t *testing.T) {
	var none *AgentCapabilities
	if none.Clarify() || none.ReviewEnabled() || none.Interactive() || none.HasTool(ToolSetPreview) ||
		none.CanPreview() || none.WritesSchema(SchemaPlan) || none.CommitsCode() || none.TracksPlanProgress() ||
		none.CanRead("plan.json") || none.Clone() != nil {
		t.Fatal("nil caps must deny everything")
	}
	impl := &AgentCapabilities{
		Interaction: InteractionAuto, Review: true,
		Tools:  []string{ToolSetPreview, ToolUpdatePlanStatus},
		Reads:  []string{" plan.json "},
		Writes: []ProductWrite{{Schema: SchemaImplementationResult, Required: true}},
	}
	if impl.Clarify() || !impl.ReviewEnabled() || !impl.Interactive() || !impl.CanPreview() ||
		!impl.CommitsCode() || !impl.TracksPlanProgress() || !impl.CanRead("plan.json") || impl.CanRead("review.json") {
		t.Fatalf("impl predicates wrong: %+v", impl)
	}
	clarify := &AgentCapabilities{Interaction: InteractionClarify, Review: true, Tools: []string{ToolAskQuestion}, Reads: []string{"*"}}
	if !clarify.Clarify() || clarify.ReviewEnabled() || !clarify.Interactive() || !clarify.CanRead("anything") {
		t.Fatal("clarify predicates wrong")
	}
	cp := impl.Clone()
	cp.Tools[0] = "x"
	cp.Reads[0] = "y"
	cp.Writes[0].Schema = "z"
	if impl.Tools[0] != ToolSetPreview || impl.Reads[0] != " plan.json " || impl.Writes[0].Schema != SchemaImplementationResult {
		t.Fatal("Clone must deep-copy slices")
	}
}
