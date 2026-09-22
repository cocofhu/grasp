package structured

import (
	"encoding/json"
	"strings"
	"testing"
)

func validRootCauseArgs() map[string]any {
	return map[string]any{
		"title":        "登录按钮无响应",
		"summary":      "点击登录后无请求发出,用户无法进入系统。",
		"symptom":      "点击「登录」无反应",
		"expected":     "应发起登录请求并跳转首页",
		"actual":       "点击后无网络请求,页面停留",
		"reproduction": []any{"打开登录页", "输入账号密码", "点击登录"},
		"impact":       "所有用户无法登录",
		"root_cause":   "提交处理器未绑定到按钮的 click 事件,导致点击被忽略",
		"evidence": []any{
			map[string]any{"title": "事件监听缺失", "detail": "LoginForm.vue 中 button 没有 @click"},
		},
		"diagrams": []any{
			map[string]any{
				"kind":    "flowchart",
				"title":   "失败路径",
				"source":  "flowchart TD\n  A[点击登录] --> B[无处理器]\n  B --> C[无请求]",
				"caption": "点击后中断",
			},
		},
	}
}

func TestParseAndRenderRootCause(t *testing.T) {
	doc, err := ParseRootCause(validRootCauseArgs())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title == "" || len(doc.Evidence) != 1 || len(doc.Diagrams) != 1 {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	raw, _ := json.Marshal(doc)
	md := RenderRootCauseMarkdown(string(raw))
	for _, want := range []string{"概述", "期望", "实际", "根因", "失败路径"} {
		if !strings.Contains(md, want) {
			t.Fatalf("render missing %q in %s", want, md)
		}
	}
	if RenderRootCauseMarkdown(`{bad`) != `{bad` {
		t.Fatal("bad json should passthrough")
	}
}

func TestParseRootCauseRejects(t *testing.T) {
	base := validRootCauseArgs()
	cases := []struct {
		name string
		mut  func(map[string]any)
		sub  string
	}{
		{"empty title", func(m map[string]any) { m["title"] = "" }, "title"},
		{"empty evidence", func(m map[string]any) { m["evidence"] = []any{} }, "evidence"},
		{"no diagrams", func(m map[string]any) { m["diagrams"] = []any{} }, "diagrams"},
		{"symbol only root cause", func(m map[string]any) { m["root_cause"] = "handleLogin" }, "符号名"},
		{"forbidden patch", func(m map[string]any) { m["patch"] = "diff" }, "patch"},
		{"empty diagram source", func(m map[string]any) {
			m["diagrams"] = []any{map[string]any{"title": "x", "source": "  "}}
		}, "source"},
		{"bad kind", func(m map[string]any) {
			m["diagrams"] = []any{map[string]any{
				"kind": "er", "title": "x",
				"source": "flowchart TD\n  A-->B",
			}}
		}, "kind"},
		{"no title or caption", func(m map[string]any) {
			m["diagrams"] = []any{map[string]any{
				"source": "flowchart TD\n  A-->B",
			}}
		}, "title 或 caption"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := validRootCauseArgs()
			tc.mut(args)
			_, err := ParseRootCause(args)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.sub) {
				t.Fatalf("err %q want substring %q", err.Error(), tc.sub)
			}
		})
	}
	_ = base
}

func TestParseRootCauseInvalidMermaid(t *testing.T) {
	prev := mermaidSyntaxCheck
	t.Cleanup(func() { mermaidSyntaxCheck = prev })
	mermaidSyntaxCheck = func(string) error { return errorsNew("boom") }
	args := validRootCauseArgs()
	_, err := ParseRootCause(args)
	if err == nil || !strings.Contains(err.Error(), "mermaid") {
		t.Fatalf("want mermaid error, got %v", err)
	}
}

func errorsNew(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

func TestClarifiedWorkKind(t *testing.T) {
	if ClarifiedWorkKind(`{"work_kind":"BUG"}`) != WorkKindBug {
		t.Fatal("normalize")
	}
	if ClarifiedWorkKind(`{}`) != "" {
		t.Fatal("empty")
	}
	doc, err := ParseClarifiedRequirement(jsonToMap(MinimalValidClarifiedRequirementJSON))
	if err != nil {
		t.Fatal(err)
	}
	if doc.WorkKind != WorkKindFeature {
		t.Fatalf("fixture work_kind=%q", doc.WorkKind)
	}
	args := jsonToMap(MinimalValidClarifiedRequirementJSON)
	delete(args, "work_kind")
	doc, err = ParseClarifiedRequirement(args)
	if err != nil || doc.WorkKind != "" {
		t.Fatalf("omit work_kind for react: %+v err=%v", doc, err)
	}
	args["work_kind"] = "nope"
	if _, err := ParseClarifiedRequirement(args); err == nil {
		t.Fatal("invalid work_kind")
	}
}

func jsonToMap(s string) map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	return m
}
