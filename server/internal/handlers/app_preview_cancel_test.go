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

func TestPreviewGuestCancellationIsScopedAndRegisteredBeforeEnqueue(t *testing.T) {
	var turns, otherViewer previewGuestTurns
	first := previewGuestRequest{ID: "first"}
	other := previewGuestRequest{ID: "first"}
	if !turns.prepare(context.Background(), &first) || !otherViewer.prepare(context.Background(), &other) {
		t.Fatal("turn not admitted")
	}
	defer other.finish()
	duplicate := previewGuestRequest{ID: "first"}
	if turns.prepare(context.Background(), &duplicate) {
		t.Fatal("duplicate in-flight ID accepted")
	}
	turns.cancel("unknown")
	if first.ctx.Err() != nil {
		t.Fatal("unknown cancellation affected the current reply")
	}
	// The turn has not reached the worker yet, but its immediate cancel must stick.
	turns.cancel("first")
	if first.ctx.Err() != context.Canceled || other.ctx.Err() != nil {
		t.Fatal("cancel crossed viewer boundary or missed the queued turn")
	}
	first.finish()
	next := previewGuestRequest{ID: "next"}
	if !turns.prepare(context.Background(), &next) {
		t.Fatal("next turn not admitted")
	}
	defer next.finish()
	turns.cancel("first")
	if next.ctx.Err() != nil {
		t.Fatal("late cancellation affected a later turn")
	}
}

func TestPreviewGuestCancelReachesBridgeAndKeepsChatConnected(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "before-output"
		if partial {
			name = "partial-output"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			started, cancelled := make(chan string, 2), make(chan string, 1)
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
				chats := 0
				for {
					var request map[string]any
					if conn.ReadJSON(&request) != nil {
						return
					}
					id, _ := request["opId"].(string)
					send := func(data map[string]any) {
						_ = conn.WriteJSON(map[string]any{"op": "event", "opId": id, "data": data})
					}
					switch request["op"] {
					case "connect":
						_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "guest"})
					case "chat":
						chats++
						started <- id
						if partial || chats > 1 {
							text := "Partial reply"
							if chats > 1 {
								text = "Continued reply"
							}
							send(map[string]any{"type": "session_update", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": text}}})
						}
						if chats > 1 {
							send(map[string]any{"type": "prompt_done", "stopReason": "end_turn"})
						}
					case "cancel":
						cancelled <- id
						send(map[string]any{"type": "prompt_done", "stopReason": "cancelled"})
						_ = conn.WriteJSON(map[string]any{"op": "queue_state", "busy": false, "queue_length": 0})
					}
				}
			}))
			defer bridge.Close()
			address := bridge.Listener.Addr().(*net.TCPAddr)
			client := sandbox.NewACPClient("127.0.0.1", address.Port).WithPassword("fixture")
			if err := client.Connect(ctx); err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			preview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				chatCtx, closeChat := context.WithCancel(ctx)
				defer closeChat()
				requests := make(chan previewGuestRequest, 1)
				go readPreviewGuestRequests(chatCtx, closeChat, conn, requests, &previewGuestTurns{})
				for {
					select {
					case <-chatCtx.Done():
						return
					case request := <-requests:
						_, _, sent := writePreviewGuestTurn(request.ctx, closeChat, conn, client, request.Text, nil)
						request.finish()
						if !sent {
							return
						}
					}
				}
			}))
			defer preview.Close()
			viewer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(preview.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer viewer.Close()
			_ = viewer.SetReadDeadline(time.Now().Add(5 * time.Second))
			read := func() map[string]any {
				t.Helper()
				var frame map[string]any
				if err := viewer.ReadJSON(&frame); err != nil {
					t.Fatal(err)
				}
				return frame
			}
			_ = viewer.WriteJSON(map[string]any{"id": "first", "text": "First request"})
			var opID string
			select {
			case opID = <-started:
			case <-ctx.Done():
				t.Fatal("bridge did not start")
			}
			if partial {
				if frame := read(); frame["type"] != "reply" || frame["text"] != "Partial reply" {
					t.Fatalf("missing partial: %+v", frame)
				}
			}
			_ = viewer.WriteJSON(map[string]any{"type": "cancel", "id": "first"})
			done := read()
			if done["type"] != "done" || done["interrupted"] != true || (partial && done["text"] != "Partial reply") {
				t.Fatalf("interrupted reply: %+v", done)
			}
			if actual := <-cancelled; actual != opID {
				t.Fatalf("cancelled operation %q; want %q", actual, opID)
			}
			_ = viewer.WriteJSON(map[string]any{"id": "next", "text": "Continue"})
			_ = viewer.WriteJSON(map[string]any{"type": "cancel", "id": "first"})
			for done = read(); done["type"] != "done"; done = read() {
			}
			if done["interrupted"] != false || done["text"] != "Continued reply" || !client.IsConnected() || client.BridgeState().Desynced {
				t.Fatalf("chat did not continue after cancel: %+v", done)
			}
		})
	}
}
