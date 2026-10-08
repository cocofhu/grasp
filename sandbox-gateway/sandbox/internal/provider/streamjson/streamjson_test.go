package streamjson

import (
	"bufio"
	"os"
	"testing"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

// TestParseQwenFixture feeds a real captured Qwen Code 0.20.0 stream-json
// transcript through the codec and asserts every element is extracted. This is
// the content-block dialect (thinking in a `thinking` field, tool_result keyed
// by `tool_use_id`) shared with claude/codebuddy.
func TestParseQwenFixture(t *testing.T) {
	f, err := os.Open("testdata/qwen-stream-json.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	c := &codec{}
	var thinking, text, toolUse, toolResult int
	var sid, stop string
	var lastUsage provider.TokenUsage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		pr := c.ParseLine(sc.Bytes())
		if pr.SessionID != "" {
			sid = pr.SessionID
		}
		if pr.StopReason != "" {
			stop = pr.StopReason
		}
		for _, m := range pr.Msgs {
			switch m.Kind {
			case oneshot.KindThinking:
				thinking++
				if m.Text == "" {
					t.Fatal("thinking chunk has empty text (dialect field missed)")
				}
			case oneshot.KindText:
				text++
			case oneshot.KindToolUse:
				toolUse++
				if m.ToolCallID == "" {
					t.Fatal("tool_use missing id")
				}
			case oneshot.KindToolResult:
				toolResult++
				if m.ToolCallID == "" {
					t.Fatal("tool_result missing tool_use_id correlation")
				}
			}
		}
		for _, u := range pr.Usage {
			lastUsage = u
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sid != "session-redacted" {
		t.Fatalf("session id=%q", sid)
	}
	if thinking != 1 || text != 1 || toolUse != 1 || toolResult != 1 {
		t.Fatalf("counts thinking=%d text=%d toolUse=%d toolResult=%d", thinking, text, toolUse, toolResult)
	}
	if stop != "end_turn" {
		t.Fatalf("stop=%q", stop)
	}
	if lastUsage.InputTokens == 0 || lastUsage.OutputTokens == 0 {
		t.Fatalf("usage not parsed: %+v", lastUsage)
	}
}

func containsStr(list []string, s string) bool {
	for _, it := range list {
		if it == s {
			return true
		}
	}
	return false
}

func TestParseAssistantText(t *testing.T) {
	c := &codec{}
	line := []byte(`{"type":"assistant","session_id":"S1","message":{"model":"m","content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"t1","name":"Bash","input":{"cmd":"ls"}}],"usage":{"input_tokens":3,"output_tokens":5,"cache_read_input_tokens":2}}}`)
	pr := c.ParseLine(line)
	if pr.SessionID != "S1" {
		t.Fatalf("sid=%q", pr.SessionID)
	}
	if len(pr.Msgs) != 2 {
		t.Fatalf("msgs=%d", len(pr.Msgs))
	}
	if pr.Msgs[0].Text != "hi" {
		t.Fatalf("text=%q", pr.Msgs[0].Text)
	}
	if pr.Msgs[1].ToolCallID != "t1" || pr.Msgs[1].ToolTitle != "Bash" {
		t.Fatalf("tool=%+v", pr.Msgs[1])
	}
	u := pr.Usage["m"]
	if u.InputTokens != 3 || u.OutputTokens != 5 || u.CacheReadTokens != 2 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestParseCursorDialect(t *testing.T) {
	c := &codec{}
	// top-level thinking delta (cursor variant)
	th := c.ParseLine([]byte(`{"type":"thinking","subtype":"delta","text":"pondering","session_id":"S1"}`))
	if len(th.Msgs) != 1 || th.Msgs[0].Kind != oneshot.KindThinking || th.Msgs[0].Text != "pondering" {
		t.Fatalf("thinking=%+v", th.Msgs)
	}
	// completed thinking event has no text => no message
	done := c.ParseLine([]byte(`{"type":"thinking","subtype":"completed","session_id":"S1"}`))
	if len(done.Msgs) != 0 {
		t.Fatalf("completed thinking should emit nothing: %+v", done.Msgs)
	}
	// camelCase usage on the result event
	r := c.ParseLine([]byte(`{"type":"result","subtype":"success","session_id":"S1","usage":{"inputTokens":6907,"outputTokens":32,"cacheReadTokens":5984,"cacheWriteTokens":0}}`))
	u := r.Usage["default"]
	if u.InputTokens != 6907 || u.OutputTokens != 32 || u.CacheReadTokens != 5984 {
		t.Fatalf("camelCase usage=%+v", u)
	}
}

func TestParseResult(t *testing.T) {
	c := &codec{}
	ok := c.ParseLine([]byte(`{"type":"result","subtype":"success","session_id":"S1","usage":{"input_tokens":1}}`))
	if ok.StopReason != "end_turn" {
		t.Fatalf("stop=%q", ok.StopReason)
	}
	bad := c.ParseLine([]byte(`{"type":"result","is_error":true,"result":"boom"}`))
	if bad.StopReason != "failed" || len(bad.Msgs) != 1 {
		t.Fatalf("bad=%+v", bad)
	}
}

// TestParseClaudeCodeResultUsageNested is the 2026 Claude Code stream-json
// shape: result.usage is an object with nested server_tool_use / cache_creation
// plus a sibling modelUsage map. A rigid map[string]int64 (or Usage int64)
// used to fail the whole line → prompt_done with empty usage.
func TestParseClaudeCodeResultUsageNested(t *testing.T) {
	c := &codec{}
	line := []byte(`{"type":"result","subtype":"success","is_error":false,"duration_api_ms":156811,"num_turns":11,"stop_reason":"end_turn","session_id":"3ce37a6e-5da6-40c7-9292-abc","usage":{"input_tokens":120,"output_tokens":80,"cache_read_input_tokens":2000,"cache_creation_input_tokens":15,"server_tool_use":{"web_search_requests":1,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":15}},"modelUsage":{"claude-sonnet-4-5":{"inputTokens":120,"outputTokens":80,"cacheReadInputTokens":2000,"cacheCreationInputTokens":15,"costUSD":0.01,"contextWindow":200000}}}`)
	pr := c.ParseLine(line)
	if pr.SessionID != "3ce37a6e-5da6-40c7-9292-abc" {
		t.Fatalf("sid=%q", pr.SessionID)
	}
	if pr.StopReason != "end_turn" {
		t.Fatalf("stop=%q", pr.StopReason)
	}
	u := pr.Usage["claude-sonnet-4-5"]
	if u.InputTokens != 120 || u.OutputTokens != 80 || u.CacheReadTokens != 2000 || u.CacheWriteTokens != 15 {
		t.Fatalf("modelUsage=%+v", u)
	}
	if _, ok := pr.Usage["default"]; ok {
		t.Fatalf("should prefer modelUsage over flat usage, got %+v", pr.Usage)
	}
}

// Nested usage without modelUsage is the live failure mode: the result line
// still has to parse, and tokens fall back onto the flat usage object.
func TestParseClaudeCodeResultUsageNestedNoModelUsage(t *testing.T) {
	c := &codec{}
	line := []byte(`{"type":"result","subtype":"success","is_error":false,"duration_api_ms":156811,"num_turns":11,"stop_reason":"end_turn","session_id":"3ce37a6e-5da6-40c7-9292-abc","usage":{"input_tokens":120,"output_tokens":80,"cache_read_input_tokens":2000,"cache_creation_input_tokens":15,"server_tool_use":{"web_search_requests":1},"service_tier":"standard","cache_creation":{"ephemeral_5m_input_tokens":15}}}`)
	pr := c.ParseLine(line)
	if pr.StopReason != "end_turn" {
		t.Fatalf("stop=%q (nested usage must not drop the result line)", pr.StopReason)
	}
	u := pr.Usage["default"]
	if u.InputTokens != 120 || u.OutputTokens != 80 || u.CacheReadTokens != 2000 || u.CacheWriteTokens != 15 {
		t.Fatalf("flat usage=%+v", u)
	}
}

func TestParseResultUsageIgnoresBareNumber(t *testing.T) {
	c := &codec{}
	pr := c.ParseLine([]byte(`{"type":"result","subtype":"success","session_id":"S1","usage":42}`))
	if pr.StopReason != "end_turn" {
		t.Fatalf("bare usage number must not drop the result line: %+v", pr)
	}
	if len(pr.Usage) != 0 {
		t.Fatalf("bare number usage should be ignored: %+v", pr.Usage)
	}
}

func TestParseToolResult(t *testing.T) {
	c := &codec{}
	pr := c.ParseLine([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","id":"t1","content":[{"type":"text","text":"out"}]}]}}`))
	if len(pr.Msgs) != 1 || pr.Msgs[0].Text != "out" {
		t.Fatalf("pr=%+v", pr)
	}
}

func TestArgs(t *testing.T) {
	c := &codec{c: Config{Bin: "claude", BaseArgs: []string{"--verbose"}, ResumeFlag: "--resume", ModelFlag: "--model", PermissionFlag: "--permission-mode", PermissionValue: "acceptEdits"}}
	args := c.Args(provider.OpenOptions{Model: "sonnet", AutoPermission: true}, "hello", "S1")
	for _, want := range []string{"claude", "-p", "hello", "--output-format", "stream-json", "--verbose", "--model", "sonnet", "--resume", "S1", "--permission-mode", "acceptEdits"} {
		if !containsStr(args, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
}

func TestArgsStdinRawMode(t *testing.T) {
	// cursor: -p is standalone (prompt on stdin), workspace pinned, no --input-format.
	c := &codec{c: Config{Bin: "cursor-agent", PromptMode: PromptStdinRaw, BaseArgs: []string{"--yolo", "--approve-mcps"}, WorkspaceFlag: "--workspace", ResumeFlag: "--resume"}}
	if !c.PromptViaStdin() {
		t.Fatal("stdin-raw mode must feed prompt via stdin")
	}
	args := c.Args(provider.OpenOptions{Cwd: "/w"}, "the prompt", "")
	if containsStr(args, "the prompt") {
		t.Fatalf("prompt must not be in argv: %v", args)
	}
	if containsStr(args, "--input-format") {
		t.Fatalf("stdin-raw must not add --input-format: %v", args)
	}
	for _, want := range []string{"-p", "--output-format", "stream-json", "--yolo", "--approve-mcps", "--workspace", "/w"} {
		if !containsStr(args, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
	if got := string(c.StdinBytes("the prompt", nil)); got != "the prompt" {
		t.Fatalf("stdin bytes=%q", got)
	}
	if c.SupportsImages() {
		t.Fatal("stdin-raw must not claim image support")
	}
}

func TestArgsStdinJSONMode(t *testing.T) {
	// claude/codebuddy: -p standalone + --input-format, prompt as a stream-json envelope.
	c := &codec{c: Config{Bin: "claude", PromptMode: PromptStdinJSON, ResumeFlag: "--resume"}}
	args := c.Args(provider.OpenOptions{}, "hi", "")
	if containsStr(args, "hi") {
		t.Fatalf("prompt must not be in argv: %v", args)
	}
	if !containsStr(args, "--input-format") {
		t.Fatalf("stdin-json must add --input-format: %v", args)
	}
	if !c.SupportsImages() {
		t.Fatal("stdin-json must support images")
	}
	env := string(c.StdinBytes("hi", nil))
	if !containsSub(env, `"type":"user"`) || !containsSub(env, `"text":"hi"`) {
		t.Fatalf("stdin envelope malformed: %s", env)
	}
	imgs := []provider.PromptImage{{Data: "abc123", MimeType: "image/jpeg"}}
	withImg := string(c.StdinBytes("look", imgs))
	if !containsSub(withImg, `"type":"image"`) || !containsSub(withImg, `"media_type":"image/jpeg"`) || !containsSub(withImg, `"data":"abc123"`) {
		t.Fatalf("image envelope malformed: %s", withImg)
	}
}

func TestArgsStrictMcpConfigAddsMcpConfigPath(t *testing.T) {
	// Mirrors Approving's codebuddy BaseArgs: strict without --mcp-config used
	// to load zero MCP servers (official CLI semantics).
	c := &codec{c: Config{
		Bin:        "codebuddy",
		ConfigRoot: "/root/.codebuddy",
		PromptMode: PromptStdinJSON,
		BaseArgs:   []string{"--verbose", "--strict-mcp-config", "--permission-mode", "bypassPermissions"},
	}}
	args := c.Args(provider.OpenOptions{}, "hi", "")
	if !containsStr(args, "--strict-mcp-config") {
		t.Fatalf("missing --strict-mcp-config: %v", args)
	}
	wantPath := "/root/.codebuddy/mcp.json"
	if i := indexOfSlice(args, "--mcp-config"); i < 0 || i+1 >= len(args) || args[i+1] != wantPath {
		t.Fatalf("want --mcp-config %s in %v", wantPath, args)
	}
}

func TestArgsStrictMcpConfigSkipsWhenAlreadySet(t *testing.T) {
	c := &codec{c: Config{
		Bin:        "codebuddy",
		ConfigRoot: "/root/.codebuddy",
		BaseArgs:   []string{"--strict-mcp-config", "--mcp-config", "/custom/mcp.json"},
	}}
	args := c.Args(provider.OpenOptions{}, "hi", "")
	if n := countFlag(args, "--mcp-config"); n != 1 {
		t.Fatalf("want exactly one --mcp-config, got %d in %v", n, args)
	}
	if i := indexOfSlice(args, "--mcp-config"); args[i+1] != "/custom/mcp.json" {
		t.Fatalf("explicit --mcp-config overridden: %v", args)
	}
}

func TestArgsNoStrictSkipsMcpConfigPath(t *testing.T) {
	c := &codec{c: Config{
		Bin:        "claude",
		ConfigRoot: "/root/.claude",
		BaseArgs:   []string{"--verbose"},
	}}
	args := c.Args(provider.OpenOptions{}, "hi", "")
	if containsStr(args, "--mcp-config") {
		t.Fatalf("must not inject --mcp-config without --strict-mcp-config: %v", args)
	}
}

func indexOfSlice(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

func countFlag(args []string, flag string) int {
	n := 0
	for _, a := range args {
		if a == flag {
			n++
		}
	}
	return n
}

func containsSub(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestClaudeAuthEnvCapsBackgroundWait(t *testing.T) {
	get := func(env []string, k string) string {
		for _, kv := range env {
			if len(kv) > len(k) && kv[:len(k)+1] == k+"=" {
				return kv[len(k)+1:]
			}
		}
		return ""
	}
	t.Setenv("SANDBOX_BG_TASK_GRACE", "")
	if v := get(ClaudeAuthEnv(nil), "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"); v != "120000" {
		t.Fatalf("default ceiling = %q", v)
	}
	t.Setenv("SANDBOX_BG_TASK_GRACE", "30s")
	if v := get(ClaudeAuthEnv(nil), "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"); v != "30000" {
		t.Fatalf("ceiling follows the grace: %q", v)
	}
	if v := get(ClaudeAuthEnv([]string{"CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS=5"}), "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"); v != "5" {
		t.Fatalf("explicit ceiling overridden: %q", v)
	}
}
