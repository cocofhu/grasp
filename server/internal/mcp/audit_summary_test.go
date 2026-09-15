package mcp

import (
	"strings"
	"testing"
)

func TestFormatMCPAuditSummary_CommonTools(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		tool    string
		args    map[string]any
		result  string
		isError bool
		want    string
		wantNot string
	}{
		{
			name: "read_artifact success",
			tool: "read_artifact",
			args: map[string]any{"name": "research.json", "content": "SECRET_SHOULD_NOT_LEAK"},
			want: "读取产物 research.json",
		},
		{
			name: "write_artifact success",
			tool: "write_artifact",
			args: map[string]any{"name": "page.html", "content": "<html>huge</html>"},
			want: "写入产物 page.html",
		},
		{
			name:    "write_artifact fail with args.error",
			tool:    "write_artifact",
			args:    map[string]any{"name": "page.html", "error": "content too large"},
			isError: true,
			want:    "写入产物 page.html 失败 · content too large",
		},
		{
			name:    "read_artifact fail from resultText",
			tool:    "read_artifact",
			args:    map[string]any{"name": "missing.json"},
			result:  "read_artifact failed: not found",
			isError: true,
			want:    "读取产物 missing.json 失败 · not found",
		},
		{
			name: "set_research",
			tool: "set_research",
			args: map[string]any{"title": "should not appear", "summary": "long body"},
			want: "写入调研结论 research.json",
		},
		{
			name: "get_research",
			tool: "get_research",
			args: map[string]any{},
			want: "读取调研结论",
		},
		{
			name: "set_proposals",
			tool: "set_proposals",
			args: map[string]any{},
			want: "写入方案结论 proposals.json",
		},
		{
			name: "get_proposals",
			tool: "get_proposals",
			want: "读取方案结论",
		},
		{
			name: "set_clarified_requirement",
			tool: "set_clarified_requirement",
			want: "写入澄清需求结论 clarified_requirement.json",
		},
		{
			name: "get_clarified_requirement",
			tool: "get_clarified_requirement",
			want: "读取澄清需求结论",
		},
		{
			name: "set_plan",
			tool: "set_plan",
			want: "写入计划结论 plan.json",
		},
		{
			name: "get_plan",
			tool: "get_plan",
			want: "读取计划结论",
		},
		{
			name: "set_implementation_result",
			tool: "set_implementation_result",
			want: "写入实现结论 implementation_result.json",
		},
		{
			name: "get_implementation_result",
			tool: "get_implementation_result",
			want: "读取实现结论",
		},
		{
			name: "set_test_result",
			tool: "set_test_result",
			want: "写入测试结论 test_result.json",
		},
		{
			name: "get_test_result",
			tool: "get_test_result",
			want: "读取测试结论",
		},
		{
			name: "set_review",
			tool: "set_review",
			want: "写入评审结论 review.json",
		},
		{
			name: "get_review",
			tool: "get_review",
			want: "读取评审结论",
		},
		{
			name: "node_complete success",
			tool: "node_complete",
			args: map[string]any{"status": "success", "summary": "long agent summary must not leak"},
			want: "节点完成 · success",
		},
		{
			name:    "node_complete failed with short error",
			tool:    "node_complete",
			args:    map[string]any{"status": "failed", "error": "preview check"},
			isError: true,
			want:    "节点完成 · failed · preview check",
		},
		{
			name:    "node_complete failed without reason keeps action",
			tool:    "node_complete",
			args:    map[string]any{"status": "failed"},
			isError: true,
			want:    "节点完成 · failed",
		},
		{
			name: "get_history_detail with node_id",
			tool: "get_history_detail",
			args: map[string]any{"node_id": "research_2wn4"},
			want: "读取历史详情 research_2wn4",
		},
		{
			name: "list_run_history",
			tool: "list_run_history",
			want: "读取运行历史",
		},
		{
			name: "ask_question",
			tool: "ask_question",
			args: map[string]any{"questions": []any{map[string]any{"prompt": "secret prompt"}}},
			want: "提出问题",
		},
		{
			name: "ask_form",
			tool: "ask_form",
			args: map[string]any{"fields": []any{map[string]any{"name": "db_url", "label": "库地址"}}},
			want: "提出表单",
		},
		{
			name: "set_preflight",
			tool: "set_preflight",
			args: map[string]any{"summary": "secret env", "confirmed": true},
			want: "写入环境确认 preflight.json",
		},
		{
			name: "get_preflight",
			tool: "get_preflight",
			want: "读取环境确认",
		},
		{
			name: "set_preview label",
			tool: "set_preview",
			args: map[string]any{"port": float64(8080), "label": "前端"},
			want: "注册预览 · 前端",
		},
		{
			name: "set_preview port only",
			tool: "set_preview",
			args: map[string]any{"port": 9090},
			want: "注册预览 · 端口 9090",
		},
		{
			name: "update_plan_status",
			tool: "update_plan_status",
			args: map[string]any{"id": "g1.1", "status": "done"},
			want: "更新计划状态 · done",
		},
		{
			name: "list_artifacts",
			tool: "list_artifacts",
			want: "列出产物",
		},
		{
			name:    "unknown tool never mcp prefix",
			tool:    "custom_tool",
			args:    map[string]any{"content": "nope"},
			want:    "调用 custom_tool",
			wantNot: "mcp ",
		},
		{
			name:    "unknown tool failure still semantic",
			tool:    "custom_tool",
			args:    map[string]any{"message": "boom"},
			isError: true,
			want:    "调用 custom_tool 失败 · boom",
			wantNot: "mcp custom_tool",
		},
		{
			name: "mcp/ prefix stripped",
			tool: "mcp/read_artifact",
			args: map[string]any{"name": "plan.json"},
			want: "读取产物 plan.json",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FormatMCPAuditSummary(tc.tool, tc.args, tc.result, tc.isError)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if tc.wantNot != "" && strings.Contains(got, tc.wantNot) {
				t.Fatalf("got %q must not contain %q", got, tc.wantNot)
			}
		})
	}
}

func TestFormatMCPAuditSummary_DoesNotLeakContentOrResult(t *testing.T) {
	t.Parallel()
	secret := "TOP_SECRET_PAYLOAD_" + strings.Repeat("x", 40)
	got := FormatMCPAuditSummary("write_artifact", map[string]any{
		"name":      "notes.md",
		"content":   secret,
		"arguments": map[string]any{"raw": secret},
	}, secret, false)
	if got != "写入产物 notes.md" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "TOP_SECRET") || strings.Contains(got, secret) {
		t.Fatalf("leaked secret into summary: %q", got)
	}

	hugeJSON := `{"summary":"` + strings.Repeat("leak", 80) + `"}`
	got = FormatMCPAuditSummary("read_artifact", map[string]any{"name": "research.json"}, hugeJSON, false)
	if got != "读取产物 research.json" {
		t.Fatalf("success must not mine resultText: %q", got)
	}
	if strings.Contains(got, "leak") {
		t.Fatalf("leaked result body: %q", got)
	}
}

func TestFormatMCPAuditSummary_FailWithoutReasonKeepsAction(t *testing.T) {
	t.Parallel()
	got := FormatMCPAuditSummary("write_artifact", map[string]any{"name": "a.json"}, "", true)
	if got != "写入产物 a.json" {
		t.Fatalf("got %q, want action without empty fail suffix", got)
	}
}


func TestFormatMCPAuditSummary_EdgeBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		tool    string
		args    map[string]any
		result  string
		isError bool
		want    string
	}{
		{name: "empty tool becomes unknown", tool: "   ", want: "调用 unknown"},
		{name: "node_complete without status", tool: "node_complete", want: "节点完成"},
		{name: "set_preview bare", tool: "set_preview", want: "注册预览"},
		{name: "update_plan_status bare", tool: "update_plan_status", want: "更新计划状态"},
		{name: "get_proposal", tool: "get_proposal", want: "读取方案结论"},
		{name: "set_proposal", tool: "set_proposal", want: "写入方案结论 proposal.json"},
		{name: "unknown get stem", tool: "get_mystery", want: "调用 get_mystery"},
		{name: "unknown set stem", tool: "set_mystery", want: "调用 set_mystery"},
		{name: "join without name", tool: "read_artifact", args: map[string]any{}, want: "读取产物"},
		{
			name:    "error prefers message when error empty",
			tool:    "list_artifacts",
			args:    map[string]any{"message": "disk full"},
			isError: true,
			want:    "列出产物 失败 · disk full",
		},
		{
			name:    "blob result ignored",
			tool:    "list_artifacts",
			result:  "{" + strings.Repeat("x", 200) + "}",
			isError: true,
			want:    "列出产物",
		},
		{
			name:    "html blob ignored",
			tool:    "list_artifacts",
			result:  "<html>" + strings.Repeat("y", 200),
			isError: true,
			want:    "列出产物",
		},
		{
			name:    "long plain text blob ignored",
			tool:    "list_artifacts",
			result:  strings.Repeat("z", 450),
			isError: true,
			want:    "列出产物",
		},
		{
			name:    "clips long error and takes first sentence",
			tool:    "ask_question",
			args:    map[string]any{"error": strings.Repeat("错", 100) + "。后面不该出现"},
			isError: true,
			want:    "提出问题 失败 · " + strings.Repeat("错", 80) + "…",
		},
		{
			name:    "english sentence split",
			tool:    "ask_question",
			args:    map[string]any{"error": "timeout waiting. more detail"},
			isError: true,
			want:    "提出问题 失败 · timeout waiting",
		},
		{
			name:    "newline truncates reason",
			tool:    "ask_question",
			args:    map[string]any{"error": "line1\nline2"},
			isError: true,
			want:    "提出问题 失败 · line1",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FormatMCPAuditSummary(tc.tool, tc.args, tc.result, tc.isError)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestWhitelistArgAndClipHelpers(t *testing.T) {
	t.Parallel()
	if got := whitelistArg(nil, "name"); got != "" {
		t.Fatalf("nil args: %q", got)
	}
	if got := whitelistArg(map[string]any{"content": "x"}, "content"); got != "" {
		t.Fatalf("non-whitelist key: %q", got)
	}
	if got := clipAuditRunes("abc", 0); got != "abc" {
		t.Fatalf("max<=0 should keep: %q", got)
	}
	if got := clipAuditRunes("  ", 10); got != "" {
		t.Fatalf("blank: %q", got)
	}
}
