package structured

import (
	"encoding/json"
	"strings"
	"testing"
)

func validMergeRequestArgs() map[string]any {
	return map[string]any{
		"summary": "两个仓库均已合入 main 并创建 MR/PR。",
		"items": []any{
			map[string]any{
				"repo":         "web",
				"sourceBranch": "grasp/feat-login",
				"targetBranch": "main",
				"url":          "https://github.com/acme/web/pull/12",
				"provider":     "GitHub",
				"state":        "created",
			},
			map[string]any{
				"repo":         "server",
				"sourceBranch": "grasp/feat-login",
				"targetBranch": "main",
				"provider":     "other",
				"state":        "unsupported",
				"note":         "自建 Git 服务无 API,请手动创建",
			},
		},
	}
}

func TestParseAndRenderMergeRequest(t *testing.T) {
	doc, err := ParseMergeRequest(validMergeRequestArgs())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 2 || doc.Items[0].Provider != MRProviderGitHub {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	raw, _ := json.Marshal(doc)
	md := RenderMergeRequestMarkdown(string(raw))
	for _, want := range []string{"两个仓库", "**web**", "`grasp/feat-login` → `main`", "已创建", "[PR](https://github.com/acme/web/pull/12)", "不支持", "自建 Git 服务"} {
		if !strings.Contains(md, want) {
			t.Fatalf("render missing %q in %s", want, md)
		}
	}
	if RenderMergeRequestMarkdown(`{bad`) != `{bad` {
		t.Fatal("bad json should passthrough")
	}
}

func TestParseMergeRequestDefaultsProvider(t *testing.T) {
	args := validMergeRequestArgs()
	delete(args["items"].([]any)[0].(map[string]any), "provider")
	doc, err := ParseMergeRequest(args)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Items[0].Provider != MRProviderOther {
		t.Fatalf("provider = %q", doc.Items[0].Provider)
	}
}

func TestParseMergeRequestRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m map[string]any)
		want   string
	}{
		{"empty summary", func(m map[string]any) { m["summary"] = " " }, "summary"},
		{"no items", func(m map[string]any) { m["items"] = []any{} }, "items"},
		{"missing repo", func(m map[string]any) { item0(m)["repo"] = "" }, "items[0].repo"},
		{"missing source", func(m map[string]any) { delete(item0(m), "sourceBranch") }, "items[0].sourceBranch"},
		{"missing target", func(m map[string]any) { delete(item0(m), "targetBranch") }, "items[0].targetBranch"},
		{"bad provider", func(m map[string]any) { item0(m)["provider"] = "bitbucket" }, "provider"},
		{"bad state", func(m map[string]any) { item0(m)["state"] = "open" }, "state"},
		{"missing url", func(m map[string]any) { delete(item0(m), "url") }, "items[0].url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := validMergeRequestArgs()
			tc.mutate(args)
			_, err := ParseMergeRequest(args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func item0(m map[string]any) map[string]any {
	return m["items"].([]any)[0].(map[string]any)
}
