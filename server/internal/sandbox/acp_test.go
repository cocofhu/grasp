package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/textutil"

	"github.com/gorilla/websocket"
)

var testUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// wsServer starts an httptest server whose /ws endpoint upgrades to a WebSocket
// and calls handle for each received frame; handle drives the scripted replies.
func wsServer(t *testing.T, handle func(conn *websocket.Conn, op string, msg map[string]any)) (string, int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			handle(conn, fmt.Sprint(m["op"]), m)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func chunkFrame(text string) map[string]any {
	return map[string]any{"op": "event", "data": map[string]any{
		"type":   "session_update",
		"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": text}},
	}}
}

func doneFrame() map[string]any {
	return map[string]any{"op": "event", "data": map[string]any{"type": "prompt_done"}}
}

func connectAndClient(t *testing.T, h string, p int) *ACPClient {
	t.Helper()
	c := NewACPClient(h, p).WithSession("/root/workspace", nil)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestACPConnectAndChatStructured(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "sess-1"})
		case "chat":
			_ = conn.WriteJSON(chunkFrame("hello world"))
			_ = conn.WriteJSON(doneFrame())
		}
	})
	c := connectAndClient(t, h, p)
	if !c.IsConnected() || c.SessionID() != "sess-1" {
		t.Fatalf("connected=%v session=%q", c.IsConnected(), c.SessionID())
	}
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("ChatStructured: %v", err)
	}
	if res.Narration != "hello world" {
		t.Errorf("narration = %q", res.Narration)
	}
}

func TestACPConnectError(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "error", "message": "handshake refused"})
		}
	})
	c := NewACPClient(h, p)
	err := c.Connect(context.Background())
	if err == nil || !contains(err.Error(), "handshake refused") {
		t.Fatalf("Connect error = %v", err)
	}
}

func TestACPConnectDialFailure(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1) // nothing listening
	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("expected dial failure")
	}
}

// TestACPConnectAuthWarmupRetry covers the cold-start race: the bridge's WS
// port is up but the in-container cursor-agent hasn't finished authenticating,
// so the first session/new is rejected with "Authentication required". Connect
// must re-dial and succeed once the agent warms up, instead of failing the node.
func TestACPConnectAuthWarmupRetry(t *testing.T) {
	prev := authWarmupBackoff
	authWarmupBackoff = 5 * time.Millisecond
	t.Cleanup(func() { authWarmupBackoff = prev })

	var connects int
	var mu sync.Mutex
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		if op != "connect" {
			return
		}
		mu.Lock()
		connects++
		n := connects
		mu.Unlock()
		if n < 3 {
			_ = conn.WriteJSON(map[string]any{"op": "error",
				"message": "session/new: rpc error -32000: Authentication required"})
			return
		}
		_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "sess-warm"})
	})

	c := NewACPClient(h, p)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect should retry through auth warmup: %v", err)
	}
	t.Cleanup(c.Close)
	if !c.IsConnected() || c.SessionID() != "sess-warm" {
		t.Fatalf("connected=%v session=%q", c.IsConnected(), c.SessionID())
	}
	mu.Lock()
	got := connects
	mu.Unlock()
	if got != 3 {
		t.Errorf("connect attempts = %d, want 3 (2 auth rejections + 1 success)", got)
	}
}

// TestACPConnectAuthBudgetExhausted verifies a permanently-rejecting auth
// (e.g. a genuinely bad key) fails for real once the warmup budget elapses,
// rather than looping forever.
func TestACPConnectAuthBudgetExhausted(t *testing.T) {
	prevBackoff, prevBudget := authWarmupBackoff, authWarmupBudget
	authWarmupBackoff = 2 * time.Millisecond
	authWarmupBudget = 20 * time.Millisecond
	t.Cleanup(func() { authWarmupBackoff, authWarmupBudget = prevBackoff, prevBudget })

	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "error", "message": "Authentication required"})
		}
	})
	c := NewACPClient(h, p)
	err := c.Connect(context.Background())
	if err == nil || !contains(err.Error(), "Authentication required") {
		t.Fatalf("expected auth failure after budget, got %v", err)
	}
}

func TestACPChatErrorNoContent(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			_ = conn.WriteJSON(map[string]any{"op": "error", "message": "model failed"})
		}
	})
	c := connectAndClient(t, h, p)
	_, err := c.ChatStructured(context.Background(), "x", nil)
	if err == nil || !contains(err.Error(), "model failed") {
		t.Fatalf("expected agent error, got %v", err)
	}
	if errors.Is(err, ErrConnClosed) || errors.Is(err, ErrChatIdle) {
		t.Error("agent error must not be an infra sentinel")
	}
}

func TestACPChatErrorWithContentReturns(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			_ = conn.WriteJSON(chunkFrame("partial"))
			_ = conn.WriteJSON(map[string]any{"op": "error", "message": "late error"})
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "x", nil)
	if err != nil {
		t.Fatalf("error after content should be swallowed: %v", err)
	}
	if res.Narration != "partial" {
		t.Errorf("narration = %q", res.Narration)
	}
}

func TestACPChatIdleTimeout(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
		// chat: intentionally send nothing -> idle watchdog trips
	})
	c := NewACPClient(h, p).WithIdleTimeout(60 * time.Millisecond)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err := c.ChatStructured(context.Background(), "x", nil)
	if !errors.Is(err, ErrChatIdle) {
		t.Fatalf("expected ErrChatIdle, got %v", err)
	}
}

func TestACPChatConnClosedMidTurn(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			_ = conn.Close() // drop mid-turn
		}
	})
	c := connectAndClient(t, h, p)
	_, err := c.ChatStructured(context.Background(), "x", nil)
	if !errors.Is(err, ErrConnClosed) {
		t.Fatalf("expected ErrConnClosed, got %v", err)
	}
}

func TestACPChatCtxCancel(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
	})
	c := connectAndClient(t, h, p)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := c.ChatStructured(ctx, "x", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestACPChatStreamAndStreamResult(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			_ = conn.WriteJSON(chunkFrame("streamed"))
			_ = conn.WriteJSON(doneFrame())
		}
	})
	c := connectAndClient(t, h, p)

	var events int
	res, err := c.ChatStream(context.Background(), "x", nil, func(json.RawMessage) { events++ })
	if err != nil || res.Narration != "streamed" {
		t.Fatalf("ChatStream res=%+v err=%v", res, err)
	}
	if events == 0 {
		t.Error("onEvent not called")
	}

	var progress int
	res2, err := c.ChatStreamResult(context.Background(), "x", nil, func(*ChatResult) { progress++ })
	if err != nil || res2.Narration != "streamed" {
		t.Fatalf("ChatStreamResult res=%+v err=%v", res2, err)
	}
	if progress == 0 {
		t.Error("onProgress not called")
	}
}

func TestACPNotConnectedGuards(t *testing.T) {
	c := NewACPClient("127.0.0.1", 9)
	if _, err := c.ChatStructured(context.Background(), "x", nil); !errors.Is(err, ErrConnClosed) {
		t.Errorf("ChatStructured guard = %v", err)
	}
	if _, err := c.ChatStream(context.Background(), "x", nil, nil); !errors.Is(err, ErrConnClosed) {
		t.Errorf("ChatStream guard = %v", err)
	}
	if _, err := c.ChatStreamResult(context.Background(), "x", nil, nil); !errors.Is(err, ErrConnClosed) {
		t.Errorf("ChatStreamResult guard = %v", err)
	}
	if err := c.Cancel(); err == nil {
		t.Error("Cancel guard should error when not connected")
	}
}

func TestACPCancel(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
	})
	c := connectAndClient(t, h, p)
	if err := c.Cancel(); err != nil {
		t.Errorf("Cancel: %v", err)
	}
}

func TestChatMessageWithImages(t *testing.T) {
	msg := chatMessage("hi", []models.PromptImage{
		{Data: "abc", MimeType: "image/png", Name: "photo.png"},
		{Data: ""},
	}, "")
	imgs, ok := msg["images"].([]map[string]string)
	if !ok || len(imgs) != 1 || imgs[0]["data"] != "abc" {
		t.Fatalf("images = %v", msg["images"])
	}
	if imgs[0]["name"] != "photo.png" {
		t.Fatalf("name = %q, want photo.png", imgs[0]["name"])
	}
	if plain := chatMessage("hi", nil, ""); plain["images"] != nil {
		t.Error("no images key expected for text-only chat")
	}
	// Missing name stays compatible with old clients (no name key).
	anon := chatMessage("hi", []models.PromptImage{{Data: "x", MimeType: "image/png"}}, "")
	anonImgs := anon["images"].([]map[string]string)
	if _, has := anonImgs[0]["name"]; has {
		t.Error("empty Name should omit name key")
	}
}

func TestNewIdleWatch(t *testing.T) {
	c, reset, stop := newIdleWatch(0)
	if c != nil {
		t.Error("disabled idle watch should have nil channel")
	}
	reset()
	stop()

	ch, reset2, stop2 := newIdleWatch(30 * time.Millisecond)
	defer stop2()
	reset2() // should not fire immediately
	select {
	case <-ch:
		t.Fatal("fired too early after reset")
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-ch:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("idle watch never fired")
	}
}

func TestWaitForACPReady(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {})
	if err := WaitForACPReady(context.Background(), h, p, "", time.Second); err != nil {
		t.Fatalf("ready server: %v", err)
	}
	// Cancelled context returns promptly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitForACPReady(ctx, "127.0.0.1", 1, "", time.Second); err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestParseHelpers(t *testing.T) {
	op, sess := parseOpAndSession(json.RawMessage(`{"op":"connected","sessionId":"z"}`))
	if op != "connected" || sess != "z" {
		t.Errorf("parseOpAndSession = %q,%q", op, sess)
	}
	if m := parseErrorMessage(json.RawMessage(`{"message":"boom"}`)); m != "boom" {
		t.Errorf("parseErrorMessage = %q", m)
	}
	if textutil.TruncateBytes("abcdef", 3, "...") != "abc..." {
		t.Errorf("truncate = %q", textutil.TruncateBytes("abcdef", 3, "..."))
	}
	if textutil.TruncateBytes("ab", 3, "...") != "ab" {
		t.Error("truncate should not modify short strings")
	}
}

// TestParseQueueBusy pins the queue_state busy extraction: a present flag is
// returned verbatim (ok=true), while a missing field or malformed frame yields
// ok=false so callers ignore it instead of wrongly reading the session as idle.
func TestParseQueueBusy(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantBusy bool
		wantOK   bool
	}{
		{"busy true", `{"op":"queue_state","busy":true}`, true, true},
		{"busy false", `{"op":"queue_state","busy":false}`, false, true},
		{"missing field", `{"op":"queue_state","queue_length":0}`, false, false},
		{"malformed", `{"op":"queue_state","busy":`, false, false},
		{"wrong type", `{"op":"queue_state","busy":"yes"}`, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			busy, ok := parseQueueBusy(json.RawMessage(c.raw))
			if busy != c.wantBusy || ok != c.wantOK {
				t.Fatalf("parseQueueBusy(%s) = (%v,%v), want (%v,%v)", c.raw, busy, ok, c.wantBusy, c.wantOK)
			}
		})
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
