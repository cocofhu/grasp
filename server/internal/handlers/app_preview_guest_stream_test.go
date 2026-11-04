package handlers

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/gorilla/websocket"
)

func TestPreviewGuestStreamsSanitizedProcessAndFinalTimeline(t *testing.T) {
	advance := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "fixture"})
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request map[string]any
			if conn.ReadJSON(&request) != nil {
				return
			}
			if request["op"] == "connect" {
				_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "guest"})
				continue
			}
			if request["op"] != "chat" {
				continue
			}
			send := func(data map[string]any) {
				_ = conn.WriteJSON(map[string]any{"op": "event", "opId": request["opId"], "data": data})
			}
			update := func(value map[string]any) { send(map[string]any{"type": "session_update", "update": value}) }
			update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"text": "Checking the page"}})
			for _, status := range []string{"in_progress", "completed"} {
				select {
				case <-ctx.Done():
					return
				case <-advance:
				}
				update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "private-call-id",
					"title": "read_file /private/path", "status": status, "rawInput": map[string]any{"path": "private-input"}, "rawOutput": "private-output"})
			}
			update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": "Page updated"}})
			send(map[string]any{"type": "prompt_done", "stopReason": "end_turn"})
		}
	}))
	defer bridge.Close()
	address := bridge.Listener.Addr().(*net.TCPAddr)
	client := sandbox.NewACPClient("127.0.0.1", address.Port).WithPassword("fixture")
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	completed := make(chan bool, 1)
	preview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		text, ok, sent := writePreviewGuestTurn(ctx, cancel, conn, client, "Update the page", nil)
		completed <- ok && sent && text == "Page updated"
	}))
	defer preview.Close()
	viewer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(preview.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	_ = viewer.SetReadDeadline(time.Now().Add(4 * time.Second))
	read := func() map[string]any {
		t.Helper()
		var frame map[string]any
		if err := viewer.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		return frame
	}
	thought := read()
	if thought["type"] != "reply" || thought["text"] != "" || len(thought["parts"].([]any)) != 1 {
		t.Fatalf("thought before narration: %+v", thought)
	}
	// The tool starts in the same burst as the thought. Its running status
	// must arrive even when there will be no more output until it finishes.
	advance <- struct{}{}
	tool := read()
	parts := tool["parts"].([]any)
	if tool["type"] != "reply" || len(parts) != 2 || parts[1].(map[string]any)["title"] != "read_file" || parts[1].(map[string]any)["input"] != nil {
		t.Fatalf("live tool projection: %+v", tool)
	}
	advance <- struct{}{}
	done := read()
	for done["type"] != "done" {
		done = read()
	}
	if done["type"] != "done" || done["text"] != "Page updated" || done["failed"] != false || done["endedAt"] == nil || len(done["parts"].([]any)) != 3 {
		t.Fatalf("final catches throttled chunks: %+v", done)
	}
	if !<-completed {
		t.Fatal("guest turn did not complete")
	}
}
