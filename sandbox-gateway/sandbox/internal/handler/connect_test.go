package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/service"

	"github.com/gorilla/websocket"
)

func TestConnectedFramesNameTheConnectTheyAnswer(t *testing.T) {
	installFakeCodex(t)
	t.Setenv("CONFIG_ROOT", t.TempDir())
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv("GRASP_MODEL_CONNECTION", "")
	chats := service.NewChatManager()
	t.Cleanup(chats.Close)
	srv := httptest.NewServer(newChatsEngine(chats))
	t.Cleanup(srv.Close)
	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
		return c
	}
	connected := func(c *websocket.Conn) map[string]any {
		t.Helper()
		for {
			var f map[string]any
			if err := c.ReadJSON(&f); err != nil {
				t.Fatal(err)
			}
			if f["op"] == "connected" {
				return f
			}
		}
	}

	first := dial()
	if err := first.WriteJSON(map[string]any{"op": "connect", "connectId": "c-1", "cwd": t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if f := connected(first); f["connectId"] != "c-1" {
		t.Fatalf("reply to the connect: %v", f["connectId"])
	}
	if f := connected(dial()); f["connectId"] != "" {
		t.Fatalf("state shown to a new socket: %v", f["connectId"])
	}
}
