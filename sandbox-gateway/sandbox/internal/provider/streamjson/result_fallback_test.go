package streamjson

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

func TestResultOnlyAnswerFallbackIsPerTurnAndDoesNotDuplicateStreams(t *testing.T) {
	shared := &codec{c: Config{AgentName: provider.CodeBuddy}}
	for _, test := range []struct {
		name  string
		lines []string
		want  string
		stop  string
	}{
		{"result only", []string{`{"type":"result","subtype":"success","result":"complete answer"}`}, "complete answer", "end_turn"},
		{"already streamed", []string{`{"type":"assistant","message":{"content":[{"type":"text","text":"streamed answer"}]}}`, `{"type":"result","subtype":"success","result":"streamed answer"}`}, "streamed answer", "end_turn"},
		{"repeated terminal", []string{`{"type":"result","subtype":"success","result":"one answer"}`, `{"type":"result","subtype":"success","result":"one answer"}`}, "one answer", "end_turn"},
		{"empty result", []string{`{"type":"result","subtype":"success","result":""}`}, "", "end_turn"},
		{"failed result", []string{`{"type":"result","subtype":"success","is_error":true,"result":"failure detail"}`}, "", "failed"},
		{"failed subtype", []string{`{"type":"result","subtype":"error_during_execution","result":"failure detail"}`}, "", "failed"},
		{"refusal", []string{`{"type":"result","subtype":"success","stop_reason":"refusal","result":"refused"}`}, "", "refusal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parser := shared.NewTurnParser(provider.OpenOptions{})
			var text, stop string
			for _, line := range test.lines {
				parsed := parser.ParseLine([]byte(line))
				stop = parsed.StopReason
				for _, message := range parsed.Msgs {
					if message.Kind == oneshot.KindText {
						text += message.Text
					}
				}
			}
			if text != test.want || stop != test.stop {
				t.Fatalf("text=%q stop=%q", text, stop)
			}
		})
	}
	// Simultaneous sessions share the registry codec, but each turn gets a
	// fresh parser. A streamed answer in one cannot suppress another's result.
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Go(func() {
			parser := shared.NewTurnParser(provider.OpenOptions{})
			parsed := parser.ParseLine([]byte(`{"type":"result","subtype":"success","result":"isolated"}`))
			if len(parsed.Msgs) != 1 || parsed.Msgs[0].Text != "isolated" {
				t.Error("another turn/session suppressed this result")
			}
		})
	}
	workers.Wait()
}

func TestResultFallbackKeepsProcessAndStreamFailures(t *testing.T) {
	for _, test := range []struct {
		name, trailer string
		wantError     bool
	}{
		{"success", "", false},
		{"process failure", "exit 7", true},
		{"stream failure", "head -c 11000000 /dev/zero | tr '\\000' x", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "result-cli")
			script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"complete answer\"}'\n" + test.trailer + "\n"
			if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			var frames []map[string]any
			session, err := New(Config{AgentName: provider.CodeBuddy, Bin: binary}).Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: root}, func(raw json.RawMessage) {
				var frame map[string]any
				if json.Unmarshal(raw, &frame) == nil {
					frames = append(frames, frame)
				}
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			_, err = session.Prompt(context.Background(), "continue", nil)
			if (err != nil) != test.wantError {
				t.Fatalf("process/stream error=%v", err)
			}
			var text strings.Builder
			var errors int
			for _, frame := range frames {
				if frame["type"] == "error_text" {
					errors++
				}
				if update, ok := frame["update"].(map[string]any); ok && update["sessionUpdate"] == "agent_message_chunk" {
					text.WriteString(update["content"].(map[string]any)["text"].(string))
				}
			}
			if text.String() != "complete answer" || (errors > 0) != test.wantError {
				t.Fatalf("fallback text=%q errorframes=%d", text.String(), errors)
			}
		})
	}
}
