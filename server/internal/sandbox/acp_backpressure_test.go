package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Hold the aggregation callback while more frames than the bounded channel
// can hold arrive. The final failure and prompt_done must follow every chunk;
// dropping either can leave the platform FIFO waiting on an already idle CLI.
func TestRunTurnSlowConsumerPreservesBurstAndTerminal(t *testing.T) {
	const chunks = 128
	burstWritten := make(chan struct{})
	var chatCalls atomic.Int32
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "slow"})
		case "chat":
			id := fmt.Sprint(msg["opId"])
			if chatCalls.Add(1) > 1 {
				_ = conn.WriteJSON(tagged(chunkFrame("next message"), id))
				_ = conn.WriteJSON(tagged(doneFrame(), id))
				return
			}
			for range chunks {
				_ = conn.WriteJSON(tagged(chunkFrame("x"), id))
			}
			_ = conn.WriteJSON(map[string]any{"op": "event", "opId": id,
				"data": map[string]any{"type": "error_text", "text": "quota exhausted"}})
			done := tagged(doneFrame(), id)
			done["data"].(map[string]any)["stopReason"] = "failed"
			_ = conn.WriteJSON(done)
			_ = conn.WriteJSON(queueStateFrame(false, "", 7))
			close(burstWritten)
		}
	})
	c := NewACPClient(h, p).WithPassword(testBridgePassword)
	c.eventCh = make(chan json.RawMessage, 4)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var once sync.Once
	res, err := c.ChatStreamResult(ctx, "once only", nil, func(*ChatResult) {
		// Pause only the first update. A passive mirror barrier is reached by
		// the old dropping reader; a lossless reader remains behind the full
		// buffer until this callback returns.
		once.Do(func() {
			<-burstWritten
			deadline := time.NewTimer(50 * time.Millisecond)
			defer deadline.Stop()
			poll := time.NewTicker(time.Millisecond)
			defer poll.Stop()
			for c.BridgeState().Waiting != 7 {
				select {
				case <-poll.C:
				case <-deadline.C:
					return
				}
			}
		})
	})
	if err != nil || res == nil {
		t.Fatalf("burst turn: result=%+v err=%v", res, err)
	}
	if res.Narration != strings.Repeat("x", chunks) || !res.Failed || res.Interrupted || res.StopReason != "failed" || res.ErrorText != "quota exhausted" {
		t.Fatalf("burst lost content or terminal: text=%d failed=%v interrupted=%v stop=%q error=%q", len(res.Narration), res.Failed, res.Interrupted, res.StopReason, res.ErrorText)
	}
	next, err := c.ChatStructured(ctx, "next", nil)
	if err != nil || next == nil || next.Narration != "next message" || chatCalls.Load() != 2 {
		t.Fatalf("next turn did not drain once: result=%+v err=%v chats=%d", next, err, chatCalls.Load())
	}
}

func TestRunTurnDrainsTerminalBeforeSocketClose(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "closing"})
		case "chat":
			id := fmt.Sprint(msg["opId"])
			_ = conn.WriteJSON(tagged(chunkFrame("complete"), id))
			_ = conn.WriteJSON(tagged(doneFrame(), id))
			_ = conn.Close()
		}
	})
	c := connectAndClient(t, h, p)
	var once sync.Once
	res, err := c.ChatStreamResult(context.Background(), "hi", nil, func(*ChatResult) {
		once.Do(func() {
			select {
			case <-c.done:
			case <-time.After(time.Second):
				t.Error("bridge did not close")
			}
		})
	})
	if err != nil || res == nil || res.Interrupted || res.Narration != "complete" || c.BridgeState().LastDoneOpID != res.OpID {
		t.Fatalf("buffered terminal lost at EOF: result=%+v err=%v", res, err)
	}
}

func TestRunTurnDisconnectKeepsIncompleteReplyExplicit(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "partial"})
		case "chat":
			_ = conn.WriteJSON(tagged(chunkFrame("partial"), fmt.Sprint(msg["opId"])))
			_ = conn.Close()
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil || res == nil || !res.Interrupted || res.Narration != "partial" || res.ErrorText == "" || res.StopReason != "" {
		t.Fatalf("incomplete reply must stay interrupted: result=%+v err=%v", res, err)
	}
}

func TestACPBackpressureStopsOnCloseRedialAndReaderRelease(t *testing.T) {
	for _, action := range []string{"close", "redial", "release"} {
		t.Run(action, func(t *testing.T) {
			h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
				switch op {
				case "connect":
					_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "lease"})
				case "flood":
					for range 10 {
						_ = conn.WriteJSON(queueStateFrame(true, "other-op", 1))
					}
					_ = conn.WriteJSON(queueStateFrame(false, "", 9))
				case "chat":
					id := fmt.Sprint(msg["opId"])
					_ = conn.WriteJSON(tagged(chunkFrame("after backpressure"), id))
					_ = conn.WriteJSON(tagged(doneFrame(), id))
				}
			})
			c := NewACPClient(h, p).WithPassword(testBridgePassword)
			c.eventCh = make(chan json.RawMessage, 2)
			if err := c.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(c.Close)
			release := c.acquireReader()
			t.Cleanup(release)
			if err := c.send(map[string]any{"op": "flood"}); err != nil {
				t.Fatal(err)
			}
			waitACPCondition(t, func() bool { return len(c.eventCh) == cap(c.eventCh) })
			switch action {
			case "close":
				c.Close()
				select {
				case <-c.done:
				case <-time.After(time.Second):
					t.Fatal("Close left readLoop blocked on its buffer")
				}
				return
			case "redial":
				old := c.done
				c.redial()
				select {
				case <-old:
				default:
					t.Fatal("redial left the previous readLoop alive")
				}
				if err := c.Connect(context.Background()); err != nil {
					t.Fatal(err)
				}
			case "release":
				release()
				waitACPCondition(t, func() bool { return c.BridgeState().Waiting == 9 })
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			res, err := c.ChatStructured(ctx, "hi", nil)
			if err != nil || res == nil || res.Narration != "after backpressure" {
				t.Fatalf("chat after %s: result=%+v err=%v", action, res, err)
			}
		})
	}
}

func waitACPCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for ACP condition")
		}
	}
}
