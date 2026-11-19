package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
	"backend/internal/provider/oneshot"
)

func TestCodexRecognizesOnlyMetadataFallbackDiagnostic(t *testing.T) {
	const warning = "Model metadata for `gpt-5` not found. Defaulting to fallback metadata; this can degrade performance and cause issues."
	for _, test := range []struct {
		message oneshot.Msg
		want    bool
	}{
		{oneshot.Msg{Kind: oneshot.KindError, Text: warning}, true},
		{oneshot.Msg{Kind: oneshot.KindText, Text: warning}, false},
		{oneshot.Msg{Kind: oneshot.KindError, Text: "Model metadata for `gpt-5` not found. Request failed."}, false},
		{oneshot.Msg{Kind: oneshot.KindError, Text: "Authentication failed"}, false},
		{oneshot.Msg{Kind: oneshot.KindError, Text: warning + "\nAPI unavailable"}, false},
	} {
		if got := (codec{}).IsRecoverableDiagnostic(test.message); got != test.want {
			t.Errorf("diagnostic kind=%v recognized=%v, want=%v", test.message.Kind, got, test.want)
		}
	}
}

func TestCodexMetadataWarningIsNotTerminal(t *testing.T) {
	const warning = "Model metadata for `gpt-5` not found. Defaulting to fallback metadata; this can degrade performance and cause issues."
	for _, envelope := range []map[string]any{
		{"type": "error", "message": warning},
		{"msg": map[string]any{"type": "error", "message": warning}},
		{"type": "item.completed", "item": map[string]any{"type": "error", "message": warning}},
	} {
		line, _ := json.Marshal(envelope)
		parsed := (codec{}).ParseLine(line)
		if parsed.StopReason != "" || len(parsed.Msgs) != 1 || parsed.Msgs[0].Kind != oneshot.KindError {
			t.Fatal("nonfatal metadata diagnostic armed a terminal turn boundary")
		}
	}
	line, _ := json.Marshal(map[string]any{"type": "turn.failed", "error": map[string]any{"message": warning}})
	if parsed := (codec{}).ParseLine(line); parsed.StopReason != "failed" {
		t.Fatal("an authoritative turn.failed was downgraded")
	}
}

func TestCodexMetadataWarningDoesNotMisclassifyLingeringSuccessfulProcess(t *testing.T) {
	warning, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"type": "error", "message": "Model metadata for `gpt-5` not found. Defaulting to fallback metadata; this can degrade performance and cause issues."}})
	script := "printf '%s\\n' '" + string(warning) + "' " +
		"'{\"type\":\"item.completed\",\"item\":{\"id\":\"answer\",\"type\":\"agent_message\",\"text\":\"complete answer\"}}' " +
		"'{\"type\":\"turn.completed\"}'\nexec sleep 30\n"
	checkCodexDiagnosticTurn(t, script, "end_turn", "complete answer")
}

func checkCodexDiagnosticTurn(t *testing.T, script, wantStop, wantText string, wantErrors ...string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "codex"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(modelconnection.EnvConfig, "")
	var frames []map[string]any
	session, err := New().Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: root}, func(raw json.RawMessage) {
		var frame map[string]any
		if json.Unmarshal(raw, &frame) == nil {
			frames = append(frames, frame)
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	result, err := session.Prompt(ctx, "continue", nil)
	if result.StopReason != wantStop || (err != nil && wantStop == "end_turn") {
		t.Fatalf("want %s, got result=%+v err=%v", wantStop, result, err)
	}
	var text string
	var diagnostics, done []string
	for _, frame := range frames {
		if frame["type"] == "error_text" {
			diagnostics = append(diagnostics, frame["text"].(string))
		}
		if frame["type"] == "prompt_done" {
			done = append(done, frame["stopReason"].(string))
		}
		if update, ok := frame["update"].(map[string]any); ok && update["sessionUpdate"] == "agent_message_chunk" {
			text += update["content"].(map[string]any)["text"].(string)
		}
	}
	if text != wantText || !slices.Equal(diagnostics, wantErrors) || !slices.Equal(done, []string{wantStop}) {
		t.Fatalf("answer=%q errors=%q prompt_done=%v", text, diagnostics, done)
	}
}
