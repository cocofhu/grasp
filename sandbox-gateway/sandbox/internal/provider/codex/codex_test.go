package codex

import (
	"strings"
	"testing"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

func TestParseAgentMessage(t *testing.T) {
	var c codec
	pr := c.ParseLine([]byte(`{"msg":{"type":"agent_message","message":"hello"}}`))
	if len(pr.Msgs) != 1 || pr.Msgs[0].Kind != oneshot.KindText || pr.Msgs[0].Text != "hello" {
		t.Fatalf("pr=%+v", pr)
	}
}

func TestParseSessionAndUsageAndComplete(t *testing.T) {
	var c codec
	if sid := c.ParseLine([]byte(`{"msg":{"type":"session_configured","session_id":"S9"}}`)).SessionID; sid != "S9" {
		t.Fatalf("sid=%q", sid)
	}
	u := c.ParseLine([]byte(`{"msg":{"type":"token_count","info":{"input_tokens":10,"output_tokens":4,"cached_input_tokens":2}}}`)).Usage
	if u["default"].InputTokens != 10 || u["default"].OutputTokens != 4 || u["default"].CacheReadTokens != 2 {
		t.Fatalf("usage=%+v", u)
	}
	if stop := c.ParseLine([]byte(`{"msg":{"type":"task_complete"}}`)).StopReason; stop != "end_turn" {
		t.Fatalf("stop=%q", stop)
	}
}

func TestAuthEnvDropsAPIKey(t *testing.T) {
	var c codec
	got := c.AuthEnv([]string{"OPENAI_API_KEY=sk-test", "CODEX_API_KEY=sk-2", "FOO=bar", "ACP_CODEX_API_KEY=sk-3"})
	if len(got) != 1 || got[0] != "FOO=bar" {
		t.Fatalf("env=%v", got)
	}
}

func TestArgsReasoningEffort(t *testing.T) {
	var c codec
	opts := provider.OpenOptions{Model: "gpt-5"}

	t.Setenv(envReasoningEffort, "")
	if got := strings.Join(c.Args(opts, "hi", ""), " "); strings.Contains(got, "model_reasoning_effort") {
		t.Fatalf("unset effort leaked into argv: %s", got)
	}

	t.Setenv(envReasoningEffort, " high ")
	got := strings.Join(c.Args(opts, "hi", "S1"), " ")
	want := "codex exec resume S1 --json --skip-git-repo-check --model gpt-5 -c model_reasoning_effort=high hi"
	if got != want {
		t.Fatalf("argv=%q\nwant %q", got, want)
	}

	t.Setenv(envReasoningEffort, "auto")
	if got := strings.Join(c.Args(opts, "hi", ""), " "); strings.Contains(got, "model_reasoning_effort") {
		t.Fatalf("auto effort leaked into argv: %s", got)
	}
}

func TestParseThreadEvents(t *testing.T) {
	var c codec
	sid := c.ParseLine([]byte(`{"type":"thread.started","thread_id":"0199a213-81c0-7800-8aa1-bbab2a035a53"}`))
	if sid.SessionID != "0199a213-81c0-7800-8aa1-bbab2a035a53" {
		t.Fatalf("sid=%q", sid.SessionID)
	}
	started := c.ParseLine([]byte(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","aggregated_output":"","status":"in_progress"}}`))
	if len(started.Msgs) != 1 || started.Msgs[0].Kind != oneshot.KindToolUse || started.Msgs[0].ToolCallID != "item_1" || started.Msgs[0].ToolTitle != "bash -lc ls" {
		t.Fatalf("start=%+v", started.Msgs)
	}
	done := c.ParseLine([]byte(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","aggregated_output":"docs\n","exit_code":0,"status":"completed"}}`))
	if len(done.Msgs) != 1 || done.Msgs[0].Kind != oneshot.KindToolResult || done.Msgs[0].Text != "docs\n" {
		t.Fatalf("done=%+v", done.Msgs)
	}
	msg := c.ParseLine([]byte(`{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Repo contains docs."}}`))
	if len(msg.Msgs) != 1 || msg.Msgs[0].Kind != oneshot.KindText || msg.Msgs[0].Text != "Repo contains docs." {
		t.Fatalf("msg=%+v", msg.Msgs)
	}
	think := c.ParseLine([]byte(`{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"planning"}}`))
	if len(think.Msgs) != 1 || think.Msgs[0].Kind != oneshot.KindThinking {
		t.Fatalf("think=%+v", think.Msgs)
	}
	mcp := c.ParseLine([]byte(`{"type":"item.completed","item":{"id":"item_9","type":"mcp_tool_call","server":"github","tool":"search_issues","status":"completed"}}`))
	if len(mcp.Msgs) != 1 || mcp.Msgs[0].ToolTitle != "github/search_issues" {
		t.Fatalf("mcp=%+v", mcp.Msgs)
	}
	patch := c.ParseLine([]byte(`{"type":"item.completed","item":{"id":"item_4","type":"file_change","changes":[{"path":"a.go","kind":"update"}],"status":"completed"}}`))
	if len(patch.Msgs) != 2 || patch.Msgs[0].Kind != oneshot.KindToolUse || patch.Msgs[1].Text != "a.go" {
		t.Fatalf("patch=%+v", patch.Msgs)
	}
	usage := c.ParseLine([]byte(`{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122,"reasoning_output_tokens":0}}`))
	if usage.StopReason != "end_turn" || usage.Usage["default"].InputTokens != 24763 || usage.Usage["default"].CacheReadTokens != 24448 || usage.Usage["default"].OutputTokens != 122 {
		t.Fatalf("usage=%+v stop=%s", usage.Usage, usage.StopReason)
	}
	fail := c.ParseLine([]byte(`{"type":"turn.failed","error":{"message":"failed to refresh token"}}`))
	if fail.StopReason != "failed" || len(fail.Msgs) != 1 || fail.Msgs[0].Text != codexLoginRepaste {
		t.Fatalf("fail=%+v", fail)
	}
}

func TestParseCapturedUnauthenticatedExec(t *testing.T) {
	// Captured from codex-cli 0.160.1: `codex exec --json --skip-git-repo-check`
	// with no ChatGPT login. The CLI emits thread.started, then 401s against
	// api.openai.com, and ends with turn.failed. Request ids are omitted.
	var c codec
	lines := []string{
		`{"type":"thread.started","thread_id":"01a111df-5174-7100-b248-f5a9d4076a15"}`,
		`{"type":"turn.started"}`,
		`{"type":"error","message":"Reconnecting... 1/5 (unexpected status 401 Unauthorized: Missing bearer or basic authentication in header, url: https://api.openai.com/v1/responses)"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Falling back from WebSockets to HTTPS transport. unexpected status 401 Unauthorized: Missing bearer or basic authentication in header, url: wss://api.openai.com/v1/responses"}}`,
		`{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized: Missing bearer or basic authentication in header, url: https://api.openai.com/v1/responses"}}`,
	}
	started := c.ParseLine([]byte(lines[0]))
	if started.SessionID != "01a111df-5174-7100-b248-f5a9d4076a15" {
		t.Fatalf("sid=%q", started.SessionID)
	}
	if turn := c.ParseLine([]byte(lines[1])); turn.StopReason != "" || len(turn.Msgs) != 0 {
		t.Fatalf("turn.started=%+v", turn)
	}
	for _, line := range lines[2:] {
		got := c.ParseLine([]byte(line))
		if got.StopReason != "failed" || len(got.Msgs) != 1 || got.Msgs[0].Kind != oneshot.KindError || got.Msgs[0].Text != codexLoginRepaste {
			t.Fatalf("line %s => %+v", line, got)
		}
	}
}

func TestRefreshMentionIsNotLoginFailure(t *testing.T) {
	var c codec
	msg := c.ParseLine([]byte(`{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Rotated the refresh token and kept the session"}}`))
	if len(msg.Msgs) != 1 || msg.Msgs[0].Kind != oneshot.KindText || msg.Msgs[0].Text != "Rotated the refresh token and kept the session" {
		t.Fatalf("msg=%+v", msg.Msgs)
	}
	tool := c.ParseLine([]byte(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"echo","aggregated_output":"codex login refreshed the refresh token","status":"completed"}}`))
	if len(tool.Msgs) != 1 || strings.Contains(tool.Msgs[0].Text, "请重新粘贴登录文件") {
		t.Fatalf("tool=%+v", tool.Msgs)
	}
	fail := c.ParseLine([]byte(`{"type":"turn.failed","error":{"message":"Rotated the refresh token and kept the session"}}`))
	if len(fail.Msgs) != 1 || fail.Msgs[0].Text == codexLoginRepaste || strings.Contains(fail.Msgs[0].Text, "请重新粘贴登录文件") {
		t.Fatalf("success wording must not be rewritten: %+v", fail.Msgs)
	}
}

func TestArgsResume(t *testing.T) {
	var c codec
	fresh := c.Args(provider.OpenOptions{Model: "gpt-5"}, "hi", "")
	if fresh[0] != "codex" || fresh[1] != "exec" || fresh[len(fresh)-1] != "hi" {
		t.Fatalf("fresh=%v", fresh)
	}
	res := c.Args(provider.OpenOptions{}, "hi", "S1")
	if !(res[2] == "resume" && res[3] == "S1") {
		t.Fatalf("resume args=%v", res)
	}
}
