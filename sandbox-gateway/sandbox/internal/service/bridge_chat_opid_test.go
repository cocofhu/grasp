package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/provider"

	"github.com/gorilla/websocket"
)

// blockingSess blocks every Prompt until its turn ctx is cancelled.
type blockingSess struct {
	stubSess
	prompts atomic.Int32
}

func (s *blockingSess) Prompt(ctx context.Context, _ string, _ []provider.PromptImage) (provider.TurnResult, error) {
	s.prompts.Add(1)
	<-ctx.Done()
	return provider.TurnResult{}, ctx.Err()
}

func newTestBridge(sess provider.Session) *Bridge {
	b := NewBridge()
	b.sess = sess
	b.agentCtx = context.Background()
	return b
}

// dialBridge registers a real websocket client on b and returns the client end.
func dialBridge(t *testing.T, b *Bridge) *websocket.Conn {
	t.Helper()
	up := websocket.Upgrader{}
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		b.RegisterClient(c)
		<-done
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		n := len(b.clients)
		b.mu.Unlock()
		if n > 0 {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatal("client never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type wsFrame struct {
	Op     string          `json:"op"`
	OpID   string          `json:"opId"`
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

// readUntil reads frames until match returns true.
func readUntil(t *testing.T, c *websocket.Conn, match func(wsFrame) bool) wsFrame {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var f wsFrame
		if json.Unmarshal(raw, &f) == nil && match(f) {
			return f
		}
	}
}

func dataType(f wsFrame) string {
	var d struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(f.Data, &d)
	return d.Type
}

func waitIdle(t *testing.T, b *Bridge) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for b.activeOpID() != "" {
		if time.Now().After(deadline) {
			t.Fatalf("turn %q never ended", b.activeOpID())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPromptBeginCarriesOpID(t *testing.T) {
	sess := &blockingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	c := dialBridge(t, b)

	if err := b.ChatWithOpID("hello", "op-A", "chat", nil); err != nil {
		t.Fatalf("chat: %v", err)
	}
	f := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_begin" })
	if f.OpID != "op-A" {
		t.Fatalf("prompt_begin envelope opId=%q want op-A", f.OpID)
	}
	if got := b.activeOpID(); got != "op-A" {
		t.Fatalf("activeOpID=%q want op-A", got)
	}
	b.CancelPromptOp("op-A")
	waitIdle(t, b)
}

func TestCancelPromptOpRemovesOnlyQueuedItem(t *testing.T) {
	sess := &blockingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	c := dialBridge(t, b)

	for _, id := range []string{"op-A", "op-B", "op-C"} {
		if err := b.ChatWithOpID("msg "+id, id, "chat", nil); err != nil {
			t.Fatalf("chat %s: %v", id, err)
		}
	}
	if got := b.CancelPromptOp("op-B"); got != CancelAckRemoved {
		t.Fatalf("cancel queued status=%q want %q", got, CancelAckRemoved)
	}
	ack := readUntil(t, c, func(f wsFrame) bool { return f.Op == "cancel_ack" })
	if ack.OpID != "op-B" || ack.Status != CancelAckRemoved {
		t.Fatalf("ack=%+v", ack)
	}
	if b.activeOpID() != "op-A" {
		t.Fatalf("active turn changed: %q", b.activeOpID())
	}
	if waiting, _, entries := b.PromptQueueInfo(); waiting != 1 || entries[0].OpID != "op-C" {
		t.Fatalf("queue after remove: %d %+v", waiting, entries)
	}
	if got := b.CancelPromptOp("op-missing"); got != CancelAckUnknown {
		t.Fatalf("cancel unknown status=%q", got)
	}
}

func TestCancelPromptOpActiveKeepsQueue(t *testing.T) {
	sess := &blockingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	c := dialBridge(t, b)

	_ = b.ChatWithOpID("first", "op-A", "chat", nil)
	_ = b.ChatWithOpID("second", "op-B", "chat", nil)
	if got := b.CancelPromptOp("op-A"); got != CancelAckCancelling {
		t.Fatalf("cancel active status=%q", got)
	}
	// blockingSess returns no stop reason, so the bridge synthesizes prompt_done for op-A.
	done := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_done" })
	if done.OpID != "op-A" {
		t.Fatalf("prompt_done envelope opId=%q want op-A", done.OpID)
	}
	// The queued op-B must start next instead of being dropped.
	begin := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_begin" && f.OpID == "op-B" })
	if begin.OpID != "op-B" {
		t.Fatalf("next begin=%+v", begin)
	}
	b.CancelPromptOp("op-B")
	waitIdle(t, b)
}
