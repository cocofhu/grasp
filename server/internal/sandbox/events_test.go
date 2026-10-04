package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/textutil"
)

func TestNormalizeKindString(t *testing.T) {
	cases := map[string]string{
		"agentMessageChunk": "agent_message_chunk",
		"tool-call":         "tool_call",
		"plan":              "plan",
		"":                  "",
	}
	for in, want := range cases {
		if got := normalizeKindString(in); got != want {
			t.Errorf("normalizeKindString(%q) = %q want %q", in, got, want)
		}
	}
}

func TestIsToolKind(t *testing.T) {
	for _, k := range []string{"tool_call", "toolcall_update", "mcp_tool_call", "foo_tool_call_bar"} {
		if !isToolKind(k) {
			t.Errorf("isToolKind(%q) should be true", k)
		}
	}
	if isToolKind("plan") {
		t.Error("plan is not a tool kind")
	}
}

func TestDispatchSessionUpdateKinds(t *testing.T) {
	r := &ChatResult{}
	dispatchSessionUpdate("agent_message_chunk", map[string]any{"content": map[string]any{"text": "msg"}}, r)
	dispatchSessionUpdate("agent_thought_chunk", map[string]any{"content": "thinking"}, r)
	dispatchSessionUpdate("available_commands_update", map[string]any{"availableCommands": []any{map[string]any{"name": "build"}}}, r)
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "t1", "title": "run", "status": "in_progress"}, r)
	dispatchSessionUpdate("tool_call_update", map[string]any{"toolCallId": "t1", "status": "completed"}, r)
	dispatchSessionUpdate("current_mode_update", map[string]any{}, r) // silent meta

	if r.Narration != "msg" {
		t.Errorf("narration = %q", r.Narration)
	}
	if r.Thought != "thinking" {
		t.Errorf("thought = %q", r.Thought)
	}
	if len(r.Commands) != 1 || r.Commands[0].Name != "build" {
		t.Errorf("commands = %+v", r.Commands)
	}
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].Status != "completed" {
		t.Errorf("toolcalls = %+v", r.ToolCalls)
	}
}

func TestDispatchPlanToolAndUpdate(t *testing.T) {
	r := &ChatResult{}
	// A native plan session_update.
	dispatchSessionUpdate("plan", map[string]any{"entries": []any{
		map[string]any{"content": "step 1", "status": "pending"},
	}}, r)
	if r.Plan == nil || len(r.Plan.Entries) != 1 || r.Plan.Entries[0].Content != "step 1" {
		t.Fatalf("plan = %+v", r.Plan)
	}
	// A plan-named tool call carrying entries in rawInput.
	r2 := &ChatResult{}
	dispatchSessionUpdate("tool_call", map[string]any{
		"title":    "update_todos",
		"rawInput": json.RawMessage(`{"entries":[{"content":"a","status":"done"}]}`),
	}, r2)
	if r2.Plan == nil || len(r2.Plan.Entries) != 1 {
		t.Errorf("plan-from-tool = %+v", r2.Plan)
	}
}

func TestAcpEventsOrdering(t *testing.T) {
	r := &ChatResult{
		Thought:   "th",
		Plan:      &ACPPlan{Entries: []ACPPlanEntry{{Content: "c", Status: "done"}}},
		ToolCalls: []ACPToolCall{{ID: "t1", Title: "run", Status: "completed"}},
		Narration: "final",
	}
	ev := r.AcpEvents()
	if len(ev) != 4 {
		t.Fatalf("event count = %d", len(ev))
	}
	if ev[0].Kind != "thought" || ev[1].Kind != "plan" || ev[2].Kind != "tool_call" || ev[3].Kind != "message" {
		t.Errorf("event ordering = %+v", ev)
	}
	var nilRes *ChatResult
	if nilRes.AcpEvents() != nil {
		t.Error("nil result should yield nil events")
	}
}

func TestExtractContentText(t *testing.T) {
	if got := extractContentText("plain"); got != "plain" {
		t.Errorf("string = %q", got)
	}
	if got := extractContentText(map[string]any{"text": "t"}); got != "t" {
		t.Errorf("map.text = %q", got)
	}
	if got := extractContentText(map[string]any{"parts": []any{map[string]any{"text": "a"}, map[string]any{"text": "b"}}}); got != "ab" {
		t.Errorf("parts = %q", got)
	}
	if got := extractContentText([]any{"x", "y"}); got != "xy" {
		t.Errorf("array = %q", got)
	}
	if got := extractContentText(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
}

func TestTruncateTextEvents(t *testing.T) {
	if textutil.TruncateBytes("abc", 5, "…(truncated)") != "abc" {
		t.Error("short unchanged")
	}
	if got := textutil.TruncateBytes("abcdef", 3, "…(truncated)"); got != "abc…(truncated)" {
		t.Errorf("TruncateBytes = %q", got)
	}
}

func TestAcpEventsThoughtFullTextNoTruncate(t *testing.T) {
	// plan coverage: g1.1 / g2.2 — Thought >4000 bytes must equal full text,
	// with no …(truncated) / ...(truncated) suffix (Chinese UTF-8 sample).
	sample := eventsChineseBoundarySample()
	if len(sample) <= 4000 {
		t.Fatalf("sample too short for boundary test: %d bytes", len(sample))
	}
	ev := (&ChatResult{Thought: sample}).AcpEvents()
	if len(ev) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ev))
	}
	if ev[0].Kind != "thought" {
		t.Fatalf("kind = %q want thought", ev[0].Kind)
	}
	if ev[0].Text != sample {
		t.Fatalf("thought text must equal full Thought (%d bytes), got %d bytes", len(sample), len(ev[0].Text))
	}
	if !utf8.ValidString(ev[0].Text) {
		t.Fatalf("AcpEvents invalid UTF-8: %q", ev[0].Text)
	}
	if strings.Contains(ev[0].Text, "…(truncated)") || strings.Contains(ev[0].Text, "...(truncated)") {
		t.Fatalf("thought must not contain truncated suffix: %q", ev[0].Text[len(ev[0].Text)-32:])
	}
	// Exactly 4000 bytes: still full, no suffix.
	exact := strings.Repeat("a", 4000)
	evExact := (&ChatResult{Thought: exact}).AcpEvents()
	if len(evExact) != 1 || evExact[0].Text != exact {
		t.Fatalf("exactly-4000 thought must be unchanged")
	}
	// Short thought unchanged.
	evShort := (&ChatResult{Thought: "short"}).AcpEvents()
	if len(evShort) != 1 || evShort[0].Text != "short" {
		t.Fatalf("short thought = %q", evShort[0].Text)
	}
	// Empty thought: no thought event.
	if len((&ChatResult{}).AcpEvents()) != 0 {
		t.Fatalf("empty ChatResult should yield no events")
	}
	// Thought >8000 bytes stays full (plan g1.2 / g2.1). Chinese sample stays valid UTF-8.
	longThought := strings.Repeat("思考需要完整保留到结尾。", 400) + "思考结尾END"
	if len(longThought) <= 8000 {
		t.Fatalf("thought sample too short: %d bytes", len(longThought))
	}
	evLongThought := (&ChatResult{Thought: longThought}).AcpEvents()
	if len(evLongThought) != 1 || evLongThought[0].Kind != "thought" || evLongThought[0].Text != longThought {
		t.Fatalf("thought >8000 must equal full text, got %d bytes", len(evLongThought[0].Text))
	}
	if !utf8.ValidString(evLongThought[0].Text) {
		t.Fatal("long thought invalid UTF-8")
	}
	if strings.Contains(evLongThought[0].Text, "…(truncated)") || strings.Contains(evLongThought[0].Text, "...(truncated)") {
		t.Fatal("long thought must not contain truncated suffix")
	}
	// Message >8000 is full text, not the old 8000-byte truncation (plan g1.2 / g2.1).
	longMsg := strings.Repeat("回复需要完整保留到结尾。", 400) + "结尾标记END"
	if len(longMsg) <= 8000 {
		t.Fatalf("message sample too short: %d bytes", len(longMsg))
	}
	evMsg := (&ChatResult{Narration: longMsg}).AcpEvents()
	if len(evMsg) != 1 || evMsg[0].Kind != "message" {
		t.Fatalf("expected 1 message event, got %+v", evMsg)
	}
	if evMsg[0].Text != longMsg {
		t.Fatalf("message text must equal full Narration (%d bytes), got %d", len(longMsg), len(evMsg[0].Text))
	}
	if !utf8.ValidString(evMsg[0].Text) {
		t.Fatal("message invalid UTF-8")
	}
	if strings.Contains(evMsg[0].Text, "…(truncated)") || strings.Contains(evMsg[0].Text, "...(truncated)") {
		t.Fatal("message must not contain truncated suffix")
	}
	exactMsg := strings.Repeat("m", 8000)
	evExactMsg := (&ChatResult{Narration: exactMsg}).AcpEvents()
	if len(evExactMsg) != 1 || evExactMsg[0].Text != exactMsg {
		t.Fatal("exactly-8000 message must be unchanged")
	}
	evShortMsg := (&ChatResult{Narration: "short"}).AcpEvents()
	if len(evShortMsg) != 1 || evShortMsg[0].Kind != "message" || evShortMsg[0].Text != "short" {
		t.Fatalf("short message = %+v", evShortMsg)
	}
	// Sealed segment narration is also full (plan g1.2).
	sealed := &ChatResult{Thought: longThought, Narration: longMsg}
	sealed.sealSegment()
	var sealedMsg, sealedThought string
	for _, e := range sealed.AcpEvents() {
		switch e.Kind {
		case "message":
			sealedMsg = e.Text
		case "thought":
			sealedThought = e.Text
		}
	}
	if sealedMsg != longMsg || sealedThought != longThought {
		t.Fatalf("sealed segment must keep full narration and thought, msg=%d thought=%d", len(sealedMsg), len(sealedThought))
	}
}

func eventsChineseBoundarySample() string {
	prefix := strings.Repeat("我需要深入分析这个问题，首先阅读上游产物，然后检查 server/internal/sandbox/events.go 中的 truncateText 实现。关键发现：当 Agent 输出较长中文思考文本时，", 20)
	suffix := "保所有中文字符在截断边界处保持完整，避免出现 U+FFFD 替换字符。接下来将统一替换 4 处风险点并补充单测。"
	return prefix + "确" + suffix
}

func TestDispatchFrameBothShapes(t *testing.T) {
	r := &ChatResult{}
	dispatchFrame(json.RawMessage(`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"A"}}}}`), r)
	dispatchFrame(json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"B"}}}`), r)
	dispatchFrame(json.RawMessage(`not json`), r) // ignored
	if r.Narration != "AB" {
		t.Errorf("narration = %q", r.Narration)
	}
}

func TestACPClientHelpers(t *testing.T) {
	// Empty host defaults to loopback.
	c := NewACPClient("", 8765)
	if c.host != "127.0.0.1" || c.port != 8765 {
		t.Fatalf("client = %+v", c)
	}
	c.WithSession("/root/workspace", nil).WithIdleTimeout(time.Second)
	if c.cwd != "/root/workspace" || c.idleTimeout != time.Second {
		t.Errorf("session/idle not set: %+v", c)
	}
	if c.IsConnected() || c.SessionID() != "" {
		t.Error("fresh client should be disconnected")
	}
	// Cancel while disconnected errors.
	if err := c.Cancel(); err == nil {
		t.Error("cancel while disconnected should error")
	}
}

func TestHasContent(t *testing.T) {
	if hasContent(nil) {
		t.Error("nil -> false")
	}
	if hasContent(&ChatResult{}) {
		t.Error("empty -> false")
	}
	if !hasContent(&ChatResult{Narration: "x"}) {
		t.Error("narration -> true")
	}
	if !hasContent(&ChatResult{Plan: &ACPPlan{}}) {
		t.Error("plan -> true")
	}
	if !hasContent(&ChatResult{ToolCalls: []ACPToolCall{{ID: "t"}}}) {
		t.Error("toolcalls -> true")
	}
}

func TestExtractPlanEntriesAndCommands(t *testing.T) {
	// entries key.
	if e := extractPlanEntries(map[string]any{"entries": []any{map[string]any{"content": "a"}}}); len(e) != 1 {
		t.Errorf("entries = %+v", e)
	}
	// steps fallback.
	if e := extractPlanEntries(map[string]any{"steps": []any{map[string]any{"title": "b"}}}); len(e) != 1 {
		t.Errorf("steps = %+v", e)
	}
	// commands via availableCommands + fallback + non-list.
	if cs := extractCommands(map[string]any{"availableCommands": []any{map[string]any{"name": "build"}}}); len(cs) != 1 || cs[0].Name != "build" {
		t.Errorf("availableCommands = %+v", cs)
	}
	if cs := extractCommands(map[string]any{"commands": []any{map[string]any{"command": "run", "summary": "s"}}}); len(cs) != 1 {
		t.Errorf("commands = %+v", cs)
	}
	if extractCommands(map[string]any{"commands": "not-a-list"}) != nil {
		t.Error("non-list commands -> nil")
	}
	if extractCommands(map[string]any{"commands": []any{map[string]any{"name": ""}}}) != nil {
		t.Error("nameless command dropped -> nil")
	}
}

func TestMergeNestedVariants(t *testing.T) {
	if mergeNested(nil) != nil {
		t.Error("nil in -> nil out")
	}
	// snake_case nested object lifted up; no clobber of existing keys.
	out := mergeNested(map[string]any{
		"session_update": map[string]any{"a": 1, "b": 2},
		"b":              99,
	})
	if out["a"] != 1 || out["b"] != 99 {
		t.Errorf("mergeNested lift/clobber = %+v", out)
	}
	if _, ok := out["session_update"]; ok {
		t.Error("session_update should be deleted after lift")
	}
	// No nested object -> passthrough copy.
	out2 := mergeNested(map[string]any{"x": 1})
	if out2["x"] != 1 {
		t.Errorf("passthrough = %+v", out2)
	}
}

func TestNormalizeStatusAll(t *testing.T) {
	cases := map[string]string{
		"":            "pending",
		"in-progress": "in_progress",
		"running":     "in_progress",
		"executing":   "in_progress",
		"complete":    "completed",
		"success":     "completed",
		"done":        "completed",
		"canceled":    "cancelled",
		"weird":       "weird",
	}
	for in, want := range cases {
		if got := normalizeStatus(in); got != want {
			t.Errorf("normalizeStatus(%q) = %q want %q", in, got, want)
		}
	}
}

func TestExtractPlanEntriesFromRaw(t *testing.T) {
	if extractPlanEntriesFromRaw(nil) != nil {
		t.Error("empty -> nil")
	}
	if extractPlanEntriesFromRaw(json.RawMessage(`bad`)) != nil {
		t.Error("bad json -> nil")
	}
	if e := extractPlanEntriesFromRaw(json.RawMessage(`{"todos":[{"content":"c","status":"running"}]}`)); len(e) != 1 || e[0].Status != "in_progress" {
		t.Errorf("todos = %+v", e)
	}
	if e := extractPlanEntriesFromRaw(json.RawMessage(`{"steps":[{"title":"s"}]}`)); len(e) != 1 || e[0].Content != "s" {
		t.Errorf("steps = %+v", e)
	}
	if extractPlanEntriesFromRaw(json.RawMessage(`{"other":1}`)) != nil {
		t.Error("no plan keys -> nil")
	}
}

func TestNormalizeSessionUpdate(t *testing.T) {
	if k, f := normalizeSessionUpdate(nil); k != "" || f != nil {
		t.Error("empty update")
	}
	if k, _ := normalizeSessionUpdate(json.RawMessage(`bad`)); k != "" {
		t.Error("bad json")
	}
	k, flat := normalizeSessionUpdate(json.RawMessage(`{"sessionUpdate":"agentMessageChunk","content":{"text":"x"}}`))
	if k != "agent_message_chunk" || flat["content"] == nil {
		t.Errorf("normalize = %q %+v", k, flat)
	}
}

func TestDispatchEventDataMalformed(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1)
	if c.dispatchEventData(json.RawMessage(`not json`), &ChatResult{}) {
		t.Error("malformed envelope should not signal done")
	}
	if c.dispatchEventData(json.RawMessage(`{"op":"event","data":{"type":"prompt_done"}}`), &ChatResult{}) != true {
		t.Error("prompt_done should signal done")
	}
	if c.dispatchEventData(json.RawMessage(`{"op":"other"}`), &ChatResult{}) {
		t.Error("non-event op should not signal done")
	}
}
