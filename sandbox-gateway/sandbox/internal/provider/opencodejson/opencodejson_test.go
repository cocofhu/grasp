package opencodejson

import (
	"testing"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

func has(list []string, s string) bool {
	for _, it := range list {
		if it == s {
			return true
		}
	}
	return false
}

func TestArgsPositionalPrompt(t *testing.T) {
	c := &codec{c: Config{
		Bin:           "opencode",
		BaseArgs:      []string{"run", "--format", "json", "--dangerously-skip-permissions"},
		WorkspaceFlag: "--dir",
		ResumeFlag:    "--session",
		ModelFlag:     "--model",
	}}
	args := c.Args(provider.OpenOptions{Cwd: "/w", Model: "m"}, "do it", "S9")
	// prompt must be last (positional).
	if args[len(args)-1] != "do it" {
		t.Fatalf("prompt not last: %v", args)
	}
	for _, want := range []string{"opencode", "run", "--format", "json", "--dir", "/w", "--model", "m", "--session", "S9"} {
		if !has(args, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
}

func TestParseTextAndSession(t *testing.T) {
	c := &codec{}
	pr := c.ParseLine([]byte(`{"type":"text","sessionID":"S1","part":{"type":"text","text":"hello"}}`))
	if pr.SessionID != "S1" {
		t.Fatalf("sid=%q", pr.SessionID)
	}
	if len(pr.Msgs) != 1 || pr.Msgs[0].Kind != oneshot.KindText || pr.Msgs[0].Text != "hello" {
		t.Fatalf("msgs=%+v", pr.Msgs)
	}
}

func TestParseToolUse(t *testing.T) {
	c := &codec{}
	pr := c.ParseLine([]byte(`{"type":"tool_use","part":{"tool":"bash","callID":"c1","state":{"input":{"cmd":"ls"}}}}`))
	if len(pr.Msgs) != 1 || pr.Msgs[0].Kind != oneshot.KindToolUse {
		t.Fatalf("msgs=%+v", pr.Msgs)
	}
	if pr.Msgs[0].ToolCallID != "c1" || pr.Msgs[0].ToolTitle != "bash" {
		t.Fatalf("tool=%+v", pr.Msgs[0])
	}
	if string(pr.Msgs[0].RawInput) != `{"cmd":"ls"}` {
		t.Fatalf("input=%s", pr.Msgs[0].RawInput)
	}
}

func TestParseToolUseFinished(t *testing.T) {
	c := &codec{}
	t.Run("completed", func(t *testing.T) {
		pr := c.ParseLine([]byte(`{"type":"tool_use","part":{"tool":"bash","callID":"c1","state":{"status":"completed","output":"docs\n"}}}`))
		if len(pr.Msgs) != 2 || pr.Msgs[1].Kind != oneshot.KindToolResult || pr.Msgs[1].ToolStatus != "" || pr.Msgs[1].Text != "docs\n" {
			t.Fatalf("msgs=%+v", pr.Msgs)
		}
	})
	t.Run("error", func(t *testing.T) {
		pr := c.ParseLine([]byte(`{"type":"tool_use","part":{"tool":"artifact-store_get_plan","callID":"c2","state":{"status":"error","input":{},"error":"get_plan failed: denied"}}}`))
		if len(pr.Msgs) != 2 {
			t.Fatalf("msgs=%+v", pr.Msgs)
		}
		if pr.Msgs[0].Kind != oneshot.KindToolUse || pr.Msgs[0].ToolCallID != "c2" || pr.Msgs[0].ToolTitle != "artifact-store_get_plan" {
			t.Fatalf("use=%+v", pr.Msgs[0])
		}
		got := pr.Msgs[1]
		if got.Kind != oneshot.KindToolResult || got.ToolCallID != "c2" || got.ToolStatus != "failed" || got.Text != "get_plan failed: denied" {
			t.Fatalf("result=%+v", got)
		}
	})
}

func TestParseStepFinishUsage(t *testing.T) {
	c := &codec{}
	pr := c.ParseLine([]byte(`{"type":"step_finish","part":{"reason":"stop","tokens":{"input":10,"output":20,"cache":{"read":3,"write":4}}}}`))
	if pr.StopReason != "end_turn" {
		t.Fatalf("stop=%q", pr.StopReason)
	}
	u := pr.Usage["default"]
	if u.InputTokens != 10 || u.OutputTokens != 20 || u.CacheReadTokens != 3 || u.CacheWriteTokens != 4 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestParseStepFinishToolCallsNotTerminal(t *testing.T) {
	c := &codec{}
	pr := c.ParseLine([]byte(`{"type":"step_finish","part":{"reason":"tool-calls","tokens":{"input":1,"output":2}}}`))
	if pr.StopReason != "" {
		t.Fatalf("tool-calls step must not be terminal: %q", pr.StopReason)
	}
}

func TestParseError(t *testing.T) {
	c := &codec{}
	pr := c.ParseLine([]byte(`{"type":"error","error":{"name":"X","data":{"message":"boom"}}}`))
	if pr.StopReason != "failed" || len(pr.Msgs) != 1 || pr.Msgs[0].Text != "boom" {
		t.Fatalf("pr=%+v", pr)
	}
}
