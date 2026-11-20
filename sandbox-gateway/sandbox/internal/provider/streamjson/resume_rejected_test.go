package streamjson

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"backend/internal/provider"
)

// fakeClaude rejects --resume gone the way Claude Code 2.1 does (an error
// result echoing the requested id, no init), fails a resumed turn on "live"
// after init, and otherwise starts session "fresh".
const fakeClaude = `#!/bin/sh
resume=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--resume" ]; then resume="$2"; fi
  shift
done
case "$resume" in
gone)
  echo "No conversation found with session ID: gone" >&2
  printf '%s\n' '{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":0,"session_id":"gone"}'
  exit 1 ;;
live)
  printf '%s\n' '{"type":"system","subtype":"init","session_id":"live"}'
  printf '%s\n' '{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":0,"session_id":"live","result":"API Error: 400"}'
  exit 1 ;;
*)
  printf '%s\n' '{"type":"system","subtype":"init","session_id":"fresh"}'
  printf '%s\n' '{"type":"assistant","session_id":"fresh","message":{"content":[{"type":"text","text":"hello"}]}}'
  printf '%s\n' '{"type":"result","subtype":"success","num_turns":1,"session_id":"fresh","result":"hello"}' ;;
esac
`

func openFakeClaude(t *testing.T, resume string) (provider.Session, func() []map[string]any) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "claude")
	if err := os.WriteFile(binary, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var frames []map[string]any
	onEvent := func(raw json.RawMessage) {
		var frame map[string]any
		if json.Unmarshal(raw, &frame) == nil {
			mu.Lock()
			frames = append(frames, frame)
			mu.Unlock()
		}
	}
	session, err := New(Config{AgentName: provider.ClaudeCode, Bin: binary, PromptMode: PromptStdinJSON, ResumeFlag: "--resume"}).
		Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: root, ResumeSessionID: resume}, onEvent, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), frames...)
	}
}

func promptDone(frames []map[string]any) map[string]any {
	for _, f := range frames {
		if f["type"] == "prompt_done" {
			return f
		}
	}
	return nil
}

func TestRejectedResumeStartsAFreshSessionAndReportsTheReset(t *testing.T) {
	session, frames := openFakeClaude(t, "gone")
	if _, err := session.Prompt(context.Background(), "hi", nil); err != nil {
		t.Fatal(err)
	}
	if session.SessionID() != "fresh" {
		t.Fatalf("session = %q, want the fresh one", session.SessionID())
	}
	done := promptDone(frames())
	if done["sessionReset"] != true || done["previousSessionId"] != "gone" {
		t.Fatalf("prompt_done = %v", done)
	}
}

func TestAFailedTurnOnAResumedSessionKeepsTheSession(t *testing.T) {
	session, frames := openFakeClaude(t, "live")
	if _, err := session.Prompt(context.Background(), "hi", nil); err == nil {
		t.Fatal("the failed turn reported success")
	}
	if session.SessionID() != "live" {
		t.Fatalf("session = %q, want the resumed one kept", session.SessionID())
	}
	if done := promptDone(frames()); done == nil || done["sessionReset"] != nil {
		t.Fatalf("prompt_done = %v", done)
	}
}
