package streamjson

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

// Gemini's native stream-json schema uses top-level message/content and
// result/status fields, unlike the Claude-family content-block dialect.
// https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/output/types.ts
func TestGeminiStreamTranscript(t *testing.T) {
	c := &codec{c: Config{AgentName: provider.Gemini}}
	frames := []string{
		`{"type":"init","session_id":"gemini-session","model":"gemini-test"}`,
		`{"type":"message","role":"user","content":"inspect the project"}`,
		`{"type":"message","role":"assistant","content":"Checking ","delta":true}`,
		`{"type":"tool_use","tool_name":"read_file","tool_id":"read-1","parameters":{"file_path":"README.md"}}`,
		`{"type":"tool_result","tool_id":"read-1","status":"success","output":"project description"}`,
		`{"type":"message","role":"assistant","content":"done."}`,
		`{"type":"result","status":"success","stats":{"input_tokens":20,"output_tokens":8,"cached":5,"input":15,"models":{"gemini-test":{"input_tokens":20,"output_tokens":8,"cached":5,"input":15}}}}`,
	}
	var sid, stop string
	var messages []oneshot.Msg
	var usage provider.TokenUsage
	for _, frame := range frames {
		res := c.ParseLine([]byte(frame))
		if res.SessionID != "" {
			sid = res.SessionID
		}
		if res.StopReason != "" {
			stop = res.StopReason
		}
		messages = append(messages, res.Msgs...)
		if u, ok := res.Usage["gemini-test"]; ok {
			usage = u
		}
	}
	if sid != "gemini-session" || stop != "end_turn" {
		t.Fatalf("session=%q stop=%q", sid, stop)
	}
	if len(messages) != 4 || messages[0].Kind != oneshot.KindText || messages[0].Text != "Checking " || messages[3].Text != "done." {
		t.Fatalf("assistant output: %+v", messages)
	}
	if call := messages[1]; call.Kind != oneshot.KindToolUse || call.ToolCallID != "read-1" || call.ToolTitle != "read_file" || string(call.RawInput) != `{"file_path":"README.md"}` {
		t.Fatalf("tool call: %+v", call)
	}
	if result := messages[2]; result.Kind != oneshot.KindToolResult || result.ToolCallID != "read-1" || result.ToolStatus != "completed" || result.Text != "project description" {
		t.Fatalf("tool result: %+v", result)
	}
	if usage.InputTokens != 15 || usage.OutputTokens != 8 || usage.CacheReadTokens != 5 {
		t.Fatalf("usage: %+v", usage)
	}
}

func TestGeminiDiagnostics(t *testing.T) {
	c := &codec{c: Config{AgentName: provider.Gemini}}
	for _, tt := range []struct {
		name, frame, stop, text string
		kind                    oneshot.MsgKind
	}{
		{"fatal", `{"type":"error","severity":"error","message":"invalid API key"}`, "failed", "invalid API key", oneshot.KindError},
		{"warning", `{"type":"error","severity":"warning","message":"retrying"}`, "", "retrying", oneshot.KindLog},
		{"result failure", `{"type":"result","status":"error","error":{"type":"API_ERROR","message":"quota exceeded"}}`, "failed", "quota exceeded", oneshot.KindError},
		{"result without detail", `{"type":"result","status":"error"}`, "failed", "Gemini request failed", oneshot.KindError},
		{"tool failure", `{"type":"tool_result","tool_id":"read-2","status":"error","error":{"type":"FILE_ERROR","message":"file missing"}}`, "", "file missing", oneshot.KindToolResult},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := c.ParseLine([]byte(tt.frame))
			if res.StopReason != tt.stop || len(res.Msgs) != 1 || res.Msgs[0].Kind != tt.kind || res.Msgs[0].Text != tt.text {
				t.Fatalf("result: %+v", res)
			}
			if tt.kind == oneshot.KindToolResult && (res.Msgs[0].ToolStatus != "failed" || res.Msgs[0].ToolCallID != "read-2") {
				t.Fatalf("tool error lost: %+v", res.Msgs[0])
			}
		})
	}
	res := c.ParseLine([]byte(`{"type":"result","status":"success","stats":{"input_tokens":6,"output_tokens":4,"cached":2}}`))
	if res.Usage["default"].InputTokens != 4 || res.Usage["default"].OutputTokens != 4 || res.Usage["default"].CacheReadTokens != 2 {
		t.Fatalf("aggregate-only usage: %+v", res.Usage)
	}
}

func TestGeminiUsageDoesNotCountCachedInputTwice(t *testing.T) {
	c := &codec{c: Config{AgentName: provider.Gemini}}
	for _, tt := range []struct {
		name, stats   string
		input, cached int64
	}{
		{"explicit zero uncached", `{"input_tokens":6,"output_tokens":4,"cached":6,"input":0}`, 0, 6},
		{"aggregate native fields", `{"input_tokens":6,"output_tokens":4,"cached":2,"input":4}`, 4, 2},
		{"aggregate older fields", `{"input_tokens":6,"output_tokens":4,"cached":2}`, 4, 2},
		{"aggregate without cache", `{"input_tokens":6,"output_tokens":4}`, 6, 0},
		{"per model native fields", `{"input_tokens":6,"output_tokens":4,"cached":2,"models":{"model-a":{"input_tokens":4,"output_tokens":3,"cached":2,"input":2},"model-b":{"input_tokens":2,"output_tokens":1,"cached":0,"input":2}}}`, 4, 2},
		{"per model older fields", `{"models":{"model-a":{"input_tokens":4,"output_tokens":3,"cached":2},"model-b":{"input_tokens":2,"output_tokens":1,"cached":0}}}`, 4, 2},
		{"missing total input", `{"output_tokens":4,"cached":2}`, 0, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := c.ParseLine([]byte(`{"type":"result","status":"success","stats":` + tt.stats + `}`))
			var total provider.TokenUsage
			for _, usage := range res.Usage {
				total.InputTokens += usage.InputTokens
				total.OutputTokens += usage.OutputTokens
				total.CacheReadTokens += usage.CacheReadTokens
			}
			if total.InputTokens != tt.input || total.OutputTokens != 4 || total.CacheReadTokens != tt.cached {
				t.Fatalf("disjoint token buckets: %+v, model usage=%+v", total, res.Usage)
			}
		})
	}
}

func TestGeminiProcessFailureBeforeDone(t *testing.T) {
	// A real process returning zero can still report a failed Gemini result.
	// Ensure the transport sends the explanation before prompt_done, so clients
	// that detach at the boundary retain the reason for the empty response.
	bin := filepath.Join(t.TempDir(), "gemini")
	script := "#!/bin/sh\ncat <<'JSON'\n" + `{"type":"init","session_id":"gemini-failed","model":"gemini-test"}
{"type":"result","status":"error","error":{"type":"API_ERROR","message":"quota exceeded"}}
` + "JSON\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRASP_MODEL_CONNECTION", "")
	var frames []json.RawMessage
	p := New(Config{AgentName: provider.Gemini, Bin: bin, PromptMode: PromptArg})
	s, err := p.Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir()}, func(raw json.RawMessage) {
		frames = append(frames, append(json.RawMessage(nil), raw...))
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	res, err := s.Prompt(context.Background(), "inspect", nil)
	if err != nil || res.StopReason != "failed" {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	var explanation string
	done := false
	for _, raw := range frames {
		var frame struct {
			Type, Text, StopReason string
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatal(err)
		}
		switch frame.Type {
		case "error_text":
			if done {
				t.Fatal("diagnostic arrived after prompt_done")
			}
			explanation += frame.Text
		case "prompt_done":
			done = true
			if frame.StopReason != "failed" {
				t.Fatalf("stop=%q", frame.StopReason)
			}
		}
	}
	if !done || !strings.Contains(explanation, "quota exceeded") {
		t.Fatalf("missing failure explanation: done=%v text=%q frames=%s", done, explanation, frames)
	}
}
