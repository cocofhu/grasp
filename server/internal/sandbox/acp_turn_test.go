package sandbox

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) {
	cancelAckWait = 300 * time.Millisecond
	os.Exit(m.Run())
}

func tagged(frame map[string]any, opID string) map[string]any {
	frame["opId"] = opID
	return frame
}

func queueStateFrame(busy bool, runningOpID string, waiting int) map[string]any {
	f := map[string]any{"op": "queue_state", "busy": busy, "queue_length": waiting}
	if runningOpID != "" {
		f["running"] = map[string]any{"opId": runningOpID}
	}
	return f
}

// A tagging bridge still streaming a stale turn: frames of the other opId must
// neither leak into this turn nor end it.
func TestRunTurnIgnoresOtherOpFrames(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("stale "), "old-op"))
			_ = conn.WriteJSON(tagged(doneFrame(), "old-op"))
			_ = conn.WriteJSON(tagged(chunkFrame("fresh"), mine))
			_ = conn.WriteJSON(tagged(doneFrame(), mine))
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("ChatStructured: %v", err)
	}
	if res.Narration != "fresh" || res.Interrupted {
		t.Fatalf("narration=%q interrupted=%v", res.Narration, res.Interrupted)
	}
	if !strings.HasPrefix(res.OpID, "g-") {
		t.Fatalf("opId = %q", res.OpID)
	}
}

// Once the bridge is known to tag frames, untagged events are not ours.
func TestRunTurnStrictAfterTaggedFrame(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("a"), mine))
			_ = conn.WriteJSON(doneFrame()) // untagged: ignored
			_ = conn.WriteJSON(tagged(chunkFrame("b"), mine))
			_ = conn.WriteJSON(tagged(doneFrame(), mine))
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil || res.Narration != "ab" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// Legacy bridge (no opId anywhere) keeps working.
func TestRunTurnLegacyBridge(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			_ = conn.WriteJSON(chunkFrame("legacy"))
			_ = conn.WriteJSON(doneFrame())
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil || res.Narration != "legacy" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// Idle abort sends cancel(opId); the bridge acks with the turn's prompt_done
// and the partial reply comes back marked Interrupted, bridge not desynced.
func TestRunTurnIdleCancelAck(t *testing.T) {
	var mu sync.Mutex
	var cancelled string
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("partial"), mine))
		case "cancel":
			id, _ := msg["opId"].(string)
			mu.Lock()
			cancelled = id
			mu.Unlock()
			_ = conn.WriteJSON(map[string]any{"op": "cancel_ack", "opId": id, "status": "cancelling"})
			done := doneFrame()
			done["data"].(map[string]any)["stopReason"] = "cancelled"
			_ = conn.WriteJSON(tagged(done, id))
			_ = conn.WriteJSON(queueStateFrame(false, "", 0))
		}
	})
	c := connectAndClient(t, h, p).WithIdleTimeout(150 * time.Millisecond)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !res.Interrupted || res.Narration != "partial" || res.ErrorText == "" {
		t.Fatalf("res = %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if cancelled != res.OpID {
		t.Fatalf("cancel opId = %q, want %q", cancelled, res.OpID)
	}
	if c.BridgeState().Desynced {
		t.Fatal("acked cancel must not mark desynced")
	}
}

// No ack at all: the client gives up, reports ErrChatIdle and flags desync
// until the bridge reports idle again.
func TestRunTurnCancelNoAckDesync(t *testing.T) {
	var connMu sync.Mutex
	var server *websocket.Conn
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		connMu.Lock()
		server = conn
		connMu.Unlock()
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
	})
	c := connectAndClient(t, h, p).WithIdleTimeout(100 * time.Millisecond)
	_, err := c.ChatStructured(context.Background(), "hi", nil)
	if !errors.Is(err, ErrChatIdle) {
		t.Fatalf("err = %v, want ErrChatIdle", err)
	}
	if !c.BridgeState().Desynced {
		t.Fatal("unacknowledged cancel should mark desynced")
	}
	connMu.Lock()
	_ = server.WriteJSON(queueStateFrame(false, "", 0))
	connMu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for c.BridgeState().Desynced {
		if time.Now().After(deadline) {
			t.Fatal("idle queue_state should clear desync")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Bridge watchdog timeout with no content surfaces ErrSandboxTurnTimeout with
// the bridge's explanation.
func TestRunTurnBridgeTimeout(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(map[string]any{"op": "event", "opId": mine,
				"data": map[string]any{"type": "error_text", "text": "连续 10m 没有任何输出"}})
			done := doneFrame()
			done["data"].(map[string]any)["stopReason"] = "timeout"
			_ = conn.WriteJSON(tagged(done, mine))
		}
	})
	c := connectAndClient(t, h, p)
	_, err := c.ChatStructured(context.Background(), "hi", nil)
	if !errors.Is(err, ErrSandboxTurnTimeout) || !errors.Is(err, ErrChatIdle) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "没有任何输出") {
		t.Fatalf("err should carry bridge reason: %v", err)
	}
}

func TestBridgeStateMirror(t *testing.T) {
	var connMu sync.Mutex
	var server *websocket.Conn
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		connMu.Lock()
		server = conn
		connMu.Unlock()
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
	})
	c := connectAndClient(t, h, p)
	if c.BridgeState().Known {
		t.Fatal("state should be unknown before any queue_state")
	}
	connMu.Lock()
	_ = server.WriteJSON(queueStateFrame(true, "op-x", 2))
	connMu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := c.BridgeState()
		if st.Known {
			if !st.Busy || st.RunningOpID != "op-x" || st.Waiting != 2 {
				t.Fatalf("state = %+v", st)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue_state not mirrored")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
