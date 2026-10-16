package models

import (
	"strings"
	"testing"
)

func TestSchemaContracts(t *testing.T) {
	for _, s := range []string{SchemaClarifiedRequirement, SchemaPlan, SchemaResearch, SchemaRootCause,
		SchemaImplementationResult, SchemaTestResult, SchemaReview, SchemaPreflight, SchemaPage} {
		if strings.TrimSpace(SchemaContract(s)) == "" {
			t.Errorf("schema %s has no contract", s)
		}
	}
	if SchemaContract("nope") != "" {
		t.Error("unknown schema must have no contract")
	}
}

// A failing verdict marked failed fails the run instead of taking the fail outlet.
func TestOutcomeContractSeparatesVerdictFromFailure(t *testing.T) {
	for _, want := range []string{"判定结论", "仍以 `success` 标记", "fail 出口"} {
		if !strings.Contains(OutcomeContract, want) {
			t.Errorf("OutcomeContract missing %q", want)
		}
	}
}

// The injector serves one app at a time and only HTML gets the pick script;
// PREVIEW_PORT from inside the sandbox bypasses the injector.
func TestPreviewContractSteersPortUse(t *testing.T) {
	for _, want := range []string{
		"set_preview(port=实际端口)",
		"同一时刻只有一个端口带取点",
		"只登记用户当前要看的那一个前端页面",
		"非页面服务不要登记",
		"会被拒绝",
		"http://127.0.0.1:<应用实际端口>/",
		"不要访问 `$PREVIEW_PORT`",
	} {
		if !strings.Contains(PreviewContract, want) {
			t.Errorf("PreviewContract missing %q", want)
		}
	}
}

func TestPromptTemplates(t *testing.T) {
	if got := FeedbackHeaderFor(3); !strings.Contains(got, "3") || strings.Contains(got, "{n}") {
		t.Errorf("FeedbackHeaderFor=%q", got)
	}
	if got := PlanIncompleteRetryFor([]string{"a", "b"}); !strings.Contains(got, "- a\n- b") {
		t.Errorf("PlanIncompleteRetryFor=%q", got)
	}
	if got := ClarifiedOpenQuestionsRetryFor([]string{"q"}); !strings.Contains(got, "- q") || strings.Contains(got, "{items}") {
		t.Errorf("ClarifiedOpenQuestionsRetryFor=%q", got)
	}
	if got := PreflightRetryFor(""); !strings.Contains(got, "preflight.json 未就绪") {
		t.Errorf("PreflightRetryFor default=%q", got)
	}
	if got := PreflightRetryFor("缺 token"); !strings.Contains(got, "缺 token") {
		t.Errorf("PreflightRetryFor=%q", got)
	}
	if got := StructuredRetryFor("r.json", "set_research"); !strings.Contains(got, "r.json") || !strings.Contains(got, "set_research") {
		t.Errorf("StructuredRetryFor=%q", got)
	}
	if got := ReviewCommitWrapUpFor(""); !strings.Contains(got, "未能列出文件") {
		t.Errorf("ReviewCommitWrapUpFor default=%q", got)
	}
	if got := ReviewCommitWrapUpFor("a.go"); !strings.Contains(got, "a.go") || strings.Contains(got, "{files}") {
		t.Errorf("ReviewCommitWrapUpFor=%q", got)
	}
}
