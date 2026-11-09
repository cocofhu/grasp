package handler

import (
	"backend/internal/service"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This is the native bridge + real one-shot subprocess transport, with a local
// deterministic CLI wire peer. It makes no vendor/model inference claim.
func TestNativeAttachOnlyAcrossClientRestartPreservesOriginalSubprocessAndResult(t *testing.T) {
	bin, work := t.TempDir(), t.TempDir()
	calls := filepath.Join(bin, "invocations")
	script := `#!/bin/sh
printf x >> "$ATTACH_OPERATION_CALLS"
while [ ! -e "$ATTACH_OPERATION_RELEASE" ]; do /bin/sleep 0.01; done
printf '%s\n' '{"type":"thread.started","thread_id":"actual-vendor-thread"}' '{"type":"item.completed","item":{"id":"answer","type":"agent_message","text":"original native result"}}' '{"type":"turn.completed","usage":{"input_tokens":2,"output_tokens":3}}'
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ATTACH_OPERATION_CALLS", calls)
	t.Setenv("ATTACH_OPERATION_RELEASE", filepath.Join(bin, "release"))
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv("CONFIG_ROOT", t.TempDir())
	t.Setenv("GRASP_MODEL_CONNECTION", "")
	t.Setenv("GRASP_CONFIGURED_MODELS", "")
	chats := service.NewChatManager()
	bridge := chats.Default()
	session, err := bridge.EnsureAgent(work, work, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	nativeSession := bridge.NativeSessionID()
	server := httptest.NewServer(newChatsEngine(chats))
	defer server.Close()
	dial := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?attachOnly=1", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	read := func(conn *websocket.Conn, op string) map[string]json.RawMessage {
		t.Helper()
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]json.RawMessage
			if json.Unmarshal(raw, &value) != nil {
				t.Fatal(string(raw))
			}
			var kind string
			_ = json.Unmarshal(value["op"], &kind)
			if kind == op {
				return value
			}
			if kind == "error" {
				t.Fatalf("native error: %s", raw)
			}
		}
	}
	// The control plane first sends one prompt, then loses its client socket.
	if err := bridge.ChatWithOpID("run exactly once", "g-original", "chat", nil); err != nil {
		t.Fatal(err)
	}
	first := dial()
	request := map[string]any{"op": "attach_operation", "opId": "g-original", "sessionId": nativeSession, "cwd": work, "workspaceFingerprint": ""}
	if err := first.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	initial := read(first, "operation_attached")
	if string(initial["complete"]) != "false" {
		t.Fatal("fixture did not attach to in-flight subprocess")
	}
	_ = first.Close()
	// A second client only observes the original operation and its completion.
	second := dial()
	if err := second.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	read(second, "operation_attached")
	if err := os.WriteFile(filepath.Join(bin, "release"), []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	read(second, "event") // prompt_done or the original streaming update
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := second.WriteJSON(request); err != nil {
			t.Fatal(err)
		}
		reply := read(second, "operation_attached")
		if string(reply["complete"]) == "true" {
			if !strings.Contains(string(reply["events"]), "original native result") || !strings.Contains(string(reply["events"]), `"opId":"g-original"`) {
				t.Fatal(string(reply["events"]))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("original operation never completed")
		}
	}
	if bridge.NativeSessionID() != nativeSession {
		t.Fatal("vendor thread discovery changed native session provenance")
	}
	if session.SessionID() != "actual-vendor-thread" {
		t.Fatal("fixture did not exercise mutable vendor session", session.SessionID())
	}
	actual, err := os.ReadFile(calls)
	if err != nil || string(actual) != "x" {
		t.Fatal("native prompt was replayed", string(actual), err)
	}
	// Missing receipt must return an error; it cannot create a new Agent/turn.
	request["opId"] = "g-missing"
	if err := second.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	for {
		_, raw, err := second.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Op string `json:"op"`
		}
		_ = json.Unmarshal(raw, &value)
		if value.Op == "error" {
			break
		}
	}
	actual, _ = os.ReadFile(calls)
	if string(actual) != "x" {
		t.Fatal("missing operation dispatched", string(actual))
	}
}
