package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// idleOrDropServer replies to connect but does nothing / drops on chat, per mode.
func silentConnectServer(t *testing.T, onChat func(conn *websocket.Conn)) (string, int) {
	return wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			if onChat != nil {
				onChat(conn)
			}
		}
	})
}

func TestChatStreamIdleAndConnClosed(t *testing.T) {
	// Idle: server sends nothing on chat.
	h, p := silentConnectServer(t, nil)
	c := NewACPClient(h, p).WithPassword(testBridgePassword).WithIdleTimeout(50 * time.Millisecond)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.ChatStream(context.Background(), "x", nil, nil); !errors.Is(err, ErrChatIdle) {
		t.Errorf("ChatStream idle = %v", err)
	}

	// Connection dropped mid-turn.
	h2, p2 := silentConnectServer(t, func(conn *websocket.Conn) { _ = conn.Close() })
	c2 := connectAndClient(t, h2, p2)
	if _, err := c2.ChatStream(context.Background(), "x", nil, nil); !errors.Is(err, ErrConnClosed) {
		t.Errorf("ChatStream conn-closed = %v", err)
	}
}

func TestChatStreamResultIdleAndCtx(t *testing.T) {
	h, p := silentConnectServer(t, nil)
	c := NewACPClient(h, p).WithPassword(testBridgePassword).WithIdleTimeout(50 * time.Millisecond)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.ChatStreamResult(context.Background(), "x", nil, nil); !errors.Is(err, ErrChatIdle) {
		t.Errorf("ChatStreamResult idle = %v", err)
	}

	h2, p2 := silentConnectServer(t, nil)
	c2 := connectAndClient(t, h2, p2)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := c2.ChatStreamResult(ctx, "x", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("ChatStreamResult ctx = %v", err)
	}
}

func TestChatStreamErrorWithoutContent(t *testing.T) {
	h, p := silentConnectServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteJSON(map[string]any{"op": "error", "message": "boom"})
	})
	c := connectAndClient(t, h, p)
	var seen int
	if _, err := c.ChatStream(context.Background(), "x", nil, func(json.RawMessage) { seen++ }); err == nil {
		t.Error("expected error frame to fail the turn")
	}
	if seen == 0 {
		t.Error("onEvent should have received the error frame")
	}
}
