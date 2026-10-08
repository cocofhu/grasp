package streamjson

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"backend/internal/provider/oneshot"
)

// TestParseCursorFixture feeds a real cursor-agent stream-json transcript
// (shell ok, shell failing, read, MCP schema lookup, MCP call) through the
// codec: every tool start pairs with a result by id, failures are flagged and
// durations come from startedAtMs / completedAtMs.
func TestParseCursorFixture(t *testing.T) {
	f, err := os.Open("testdata/cursor-stream-json.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	c := &codec{}
	var uses, results []oneshot.Msg
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		for _, m := range c.ParseLine(sc.Bytes()).Msgs {
			switch m.Kind {
			case oneshot.KindToolUse:
				uses = append(uses, m)
			case oneshot.KindToolResult:
				results = append(results, m)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	titles := make([]string, len(uses))
	for i, u := range uses {
		titles[i] = u.ToolTitle
	}
	want := []string{"Shell", "Shell", "Read", "Get mcp tools", "echo"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Fatalf("tool titles = %v, want %v", titles, want)
	}
	if len(results) != len(uses) {
		t.Fatalf("results = %d, uses = %d", len(results), len(uses))
	}
	byID := map[string]oneshot.Msg{}
	for _, r := range results {
		byID[r.ToolCallID] = r
	}
	for _, u := range uses {
		if _, ok := byID[u.ToolCallID]; !ok {
			t.Errorf("no result for %s (%s)", u.ToolTitle, u.ToolCallID)
		}
	}

	var shell map[string]any
	if err := json.Unmarshal(uses[0].RawInput, &shell); err != nil {
		t.Fatal(err)
	}
	if shell["command"] != "echo hi" || shell["parsingResult"] != nil || shell["toolCallId"] != nil {
		t.Errorf("shell input not trimmed to the command: %s", uses[0].RawInput)
	}
	if r := byID[uses[0].ToolCallID]; r.ToolStatus != "" || !strings.Contains(r.Text, "hi") || r.DurationMs <= 0 {
		t.Errorf("echo result = %+v", r)
	}
	if r := byID[uses[1].ToolCallID]; r.ToolStatus != "failed" || !strings.Contains(r.Text, "No such file") {
		t.Errorf("failing ls result = %+v", r)
	}
	if r := byID[uses[2].ToolCallID]; !strings.Contains(r.Text, "hello fixture") {
		t.Errorf("read result = %+v", r)
	}
	var mcpIn map[string]any
	if json.Unmarshal(uses[4].RawInput, &mcpIn) != nil || len(mcpIn) != 1 || mcpIn["message"] != "ping" {
		t.Errorf("mcp input = %s", uses[4].RawInput)
	}
	if r := byID[uses[4].ToolCallID]; r.Text != "Echo: ping" {
		t.Errorf("mcp result text = %q", r.Text)
	}
}

func TestParseCursorToolCases(t *testing.T) {
	c := &codec{}
	parse := func(line string) []oneshot.Msg { return c.ParseLine([]byte(line)).Msgs }

	t.Run("mcp falls back to name", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"started","call_id":"a","tool_call":{"mcpToolCall":{"args":{"name":"store-set_plan","args":{"goals":[]}}}}}`)
		if len(m) != 1 || m[0].ToolTitle != "store-set_plan" || string(m[0].RawInput) != `{"goals":[]}` {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("mcp without names", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"started","call_id":"a","tool_call":{"mcpToolCall":{"args":{}}}}`)
		if len(m) != 1 || m[0].ToolTitle != "mcp" {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("function with string arguments", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"started","call_id":"f","tool_call":{"function":{"name":"lookup","arguments":"{\"q\":1}"}}}`)
		if len(m) != 1 || m[0].ToolTitle != "lookup" || string(m[0].RawInput) != `{"q":1}` {
			t.Fatalf("got %+v", m)
		}
		m = parse(`{"type":"tool_call","subtype":"started","call_id":"f","tool_call":{"function":{"arguments":"not json"}}}`)
		if len(m) != 1 || m[0].ToolTitle != "function" || string(m[0].RawInput) != `{"arguments":"not json"}` {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("missing call_id pairs by kind and args", func(t *testing.T) {
		s := parse(`{"type":"tool_call","subtype":"started","tool_call":{"grepToolCall":{"args":{"pattern":"x"}}}}`)
		d := parse(`{"type":"tool_call","subtype":"completed","tool_call":{"grepToolCall":{"args":{"pattern":"x"},"result":{"success":{"output":"a.go"}}}}}`)
		if len(s) != 1 || len(d) != 1 || s[0].ToolCallID == "" || s[0].ToolCallID != d[0].ToolCallID {
			t.Fatalf("ids %+v / %+v", s, d)
		}
		if s[0].ToolTitle != "Grep" || d[0].Text != "a.go" {
			t.Fatalf("got %+v / %+v", s, d)
		}
	})
	t.Run("toolCallId inside the call", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"started","tool_call":{"readLintsToolCall":{"args":{},"toolCallId":"in"}}}`)
		if len(m) != 1 || m[0].ToolCallID != "in" || m[0].ToolTitle != "Read lints" {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("rejected and error results fail", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"completed","call_id":"r","tool_call":{"editToolCall":{"args":{},"result":"rejected"}}}`)
		if len(m) != 1 || m[0].ToolStatus != "failed" {
			t.Fatalf("got %+v", m)
		}
		m = parse(`{"type":"tool_call","subtype":"completed","call_id":"e","tool_call":{"editToolCall":{"args":{},"result":{"error":{"message":"denied"}}}}}`)
		if len(m) != 1 || m[0].ToolStatus != "failed" || m[0].Text != "denied" {
			t.Fatalf("got %+v", m)
		}
		m = parse(`{"type":"tool_call","subtype":"completed","call_id":"e","tool_call":{"editToolCall":{"args":{},"result":{"rejected":{}}}}}`)
		if len(m) != 1 || m[0].ToolStatus != "failed" || m[0].Text != "{}" {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("plain and unknown results", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"completed","call_id":"p","tool_call":{"webSearchToolCall":{"args":{},"result":"done"}}}`)
		if len(m) != 1 || m[0].Text != "done" || m[0].ToolStatus != "" {
			t.Fatalf("got %+v", m)
		}
		m = parse(`{"type":"tool_call","subtype":"completed","call_id":"p","tool_call":{"webSearchToolCall":{"args":{},"result":{"links":[1]}}}}`)
		if len(m) != 1 || m[0].Text != `{"links":[1]}` {
			t.Fatalf("got %+v", m)
		}
		m = parse(`{"type":"tool_call","subtype":"completed","call_id":"p","tool_call":{"webSearchToolCall":{"args":{}}}}`)
		if len(m) != 1 || m[0].Text != "" {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("long output is clipped on a rune boundary", func(t *testing.T) {
		long := strings.Repeat("中", maxToolResultText)
		b, _ := json.Marshal(map[string]any{"type": "tool_call", "subtype": "completed", "call_id": "l", "tool_call": map[string]any{"shellToolCall": map[string]any{"result": map[string]any{"success": map[string]any{"stdout": long}}}}})
		m := parse(string(b))
		if len(m) != 1 || len(m[0].Text) > maxToolResultText+len("…") || !strings.HasSuffix(m[0].Text, "…") || !json.Valid([]byte(`"`+m[0].Text+`"`)) {
			t.Fatalf("clip len=%d", len(m[0].Text))
		}
	})
	t.Run("background shell pid", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"completed","call_id":"b","tool_call":{"shellToolCall":{"args":{"command":"python3 -m http.server"},"result":{"isBackground":true,"success":{"pid":4242,"shellId":"s1"}}}}}`)
		if len(m) != 1 || m[0].BackgroundPID != 4242 {
			t.Fatalf("got %+v", m)
		}
		for _, line := range []string{
			`{"type":"tool_call","subtype":"completed","call_id":"b","tool_call":{"shellToolCall":{"args":{},"result":{"success":{"pid":4242,"stdout":"x"}}}}}`,
			`{"type":"tool_call","subtype":"completed","call_id":"b","tool_call":{"shellToolCall":{"args":{},"result":{"isBackground":true,"success":{}}}}}`,
			`{"type":"tool_call","subtype":"completed","call_id":"b","tool_call":{"webSearchToolCall":{"args":{},"result":{"isBackground":true,"success":{"pid":4242}}}}}`,
		} {
			if m := parse(line); len(m) != 1 || m[0].BackgroundPID != 0 {
				t.Fatalf("%s: got %+v", line, m)
			}
		}
	})
	t.Run("duration as numbers or bad values", func(t *testing.T) {
		m := parse(`{"type":"tool_call","subtype":"completed","call_id":"d","tool_call":{"shellToolCall":{"args":{}},"startedAtMs":1000,"completedAtMs":3500}}`)
		if len(m) != 1 || m[0].DurationMs != 2500 {
			t.Fatalf("got %+v", m)
		}
		m = parse(`{"type":"tool_call","subtype":"completed","call_id":"d","tool_call":{"shellToolCall":{"args":{}},"startedAtMs":"9","completedAtMs":"3"}}`)
		if len(m) != 1 || m[0].DurationMs != 0 {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("ignored shapes", func(t *testing.T) {
		for _, line := range []string{
			`{"type":"tool_call","subtype":"started","call_id":"x"}`,
			`{"type":"tool_call","subtype":"started","call_id":"x","tool_call":{"startedAtMs":"1"}}`,
			`{"type":"tool_call","subtype":"started","call_id":"x","tool_call":{"shellToolCall":"bad"}}`,
			`{"type":"tool_call","subtype":"progress","call_id":"x","tool_call":{"shellToolCall":{"args":{}}}}`,
			`{"type":"tool_call","tool_call":[]}`,
		} {
			if m := parse(line); len(m) != 0 {
				t.Errorf("%s -> %+v", line, m)
			}
		}
	})
}

func TestCursorToolHelpers(t *testing.T) {
	if toolDisplayName("") != "Tool" || toolDisplayName("shell") != "Shell" || toolDisplayName("getMcpTools") != "Get mcp tools" {
		t.Error("toolDisplayName")
	}
	if string(trimArgs(json.RawMessage(`[1]`))) != `[1]` {
		t.Error("trimArgs keeps non-objects")
	}
	if string(trimArgs(json.RawMessage(`{"path":"a","toolCallId":"x","workingDirectory":"","n":null}`))) != `{"path":"a"}` {
		t.Error("trimArgs drops noise and empties")
	}
	if flatText(json.RawMessage(`[{"text":"a"},{"text":{"text":"b"}},{"text":{}}]`)) != "a\nb" || flatText(json.RawMessage(`{}`)) != "" {
		t.Error("flatText")
	}
	if msField(nil) != 0 || msField(json.RawMessage(`"12"`)) != 12 || msField(json.RawMessage(`12`)) != 12 {
		t.Error("msField")
	}
	if s, failed := cursorToolResult(json.RawMessage(`[`)); s != "" || failed {
		t.Error("bad result json")
	}
	if s, _ := cursorToolResult(json.RawMessage(`[1]`)); s != "" {
		t.Error("array result")
	}
}
