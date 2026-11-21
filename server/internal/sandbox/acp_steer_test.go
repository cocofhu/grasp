package sandbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gorilla/websocket"
)

// steerBridge is a fake bridge whose chats stay open until steered (or
// cancelled). ack decides the steer_ack status; "" sends none.
type steerBridge struct {
	capable bool
	ack     SteerStatus
	mu      sync.Mutex
	steers  []map[string]any
}

func (b *steerBridge) handle(conn *websocket.Conn, op string, msg map[string]any) {
	switch op {
	case "connect":
		_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s", "capabilities": map[string]any{"steer": b.capable}})
	case "chat":
		_ = conn.WriteJSON(chunkFrame("a"))
	case "cancel":
		_ = conn.WriteJSON(doneFrame())
	case "steer":
		b.mu.Lock()
		b.steers = append(b.steers, msg)
		b.mu.Unlock()
		opID, _ := msg["opId"].(string)
		steerID, _ := msg["steerId"].(string)
		// An ack for another client's steer must not satisfy this one.
		_ = conn.WriteJSON(map[string]any{"op": "steer_ack", "steerId": "someone-else", "status": "failed"})
		if b.ack == "" {
			return
		}
		frame := map[string]any{"op": "steer_ack", "steerId": steerID, "turnOpId": opID, "status": string(b.ack)}
		if b.ack == SteerFailed {
			frame["message"] = "broken pipe"
		}
		_ = conn.WriteJSON(frame)
		if b.ack == SteerDelivered {
			_ = conn.WriteJSON(tagged(map[string]any{"op": "event", "data": map[string]any{"type": "user_steer", "steerId": steerID, "text": msg["text"]}}, opID))
			_ = conn.WriteJSON(tagged(chunkFrame("b"), opID))
			_ = conn.WriteJSON(tagged(doneFrame(), opID))
		}
	}
}

// startSteerTurn runs a chat in the background and returns once it is in flight.
func startSteerTurn(t *testing.T, c *ACPClient) <-chan *ChatResult {
	t.Helper()
	out := make(chan *ChatResult, 1)
	go func() {
		res, _ := c.ChatStructured(context.Background(), "hi", nil)
		out <- res
	}()
	deadline := time.Now().Add(3 * time.Second)
	for c.InFlightOpID() == "" {
		if time.Now().After(deadline) {
			t.Fatal("turn never started")
		}
		time.Sleep(2 * time.Millisecond)
	}
	return out
}

func TestSteerDeliversIntoInFlightTurn(t *testing.T) {
	b := &steerBridge{capable: true, ack: SteerDelivered}
	h, p := wsServer(t, b.handle)
	c := connectAndClient(t, h, p)
	store := blob.NewMemory()
	ref, err := store.Put(context.Background(), strings.NewReader("png"), blob.Meta{MimeType: "image/png", Name: "a.png"})
	if err != nil {
		t.Fatal(err)
	}
	c.WithBlobs(store)
	if !c.SupportsSteer() {
		t.Fatal("connected frame announced steer")
	}
	if st, err := c.Steer(context.Background(), "early", nil); st != SteerNoTurn || err != nil {
		t.Fatalf("steer without a turn = %q %v", st, err)
	}
	turn := startSteerTurn(t, c)
	opID := c.InFlightOpID()
	images := []models.PromptImage{{Ref: ref.String(), MimeType: "image/png", Name: "a.png"}}
	st, err := c.Steer(context.Background(), "also this", images)
	if st != SteerDelivered || err != nil {
		t.Fatalf("steer = %q %v", st, err)
	}
	res := <-turn
	if res == nil || res.Narration != "ab" {
		t.Fatalf("turn = %+v", res)
	}
	var sawSteer bool
	for _, raw := range res.RawEvents {
		sawSteer = sawSteer || strings.Contains(string(raw), `"user_steer"`)
	}
	if !sawSteer {
		t.Fatal("user_steer must be recorded with the turn it joined")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.steers) != 1 || b.steers[0]["opId"] != opID || b.steers[0]["text"] != "also this" || b.steers[0]["images"] == nil {
		t.Fatalf("steer frame = %v (turn %s)", b.steers, opID)
	}
}

func TestSteerWithoutCapabilityIsUnsupported(t *testing.T) {
	b := &steerBridge{capable: false, ack: SteerDelivered}
	h, p := wsServer(t, b.handle)
	c := connectAndClient(t, h, p)
	turn := startSteerTurn(t, c)
	if st, err := c.Steer(context.Background(), "x", nil); st != SteerUnsupported || err != nil {
		t.Fatalf("steer = %q %v", st, err)
	}
	b.mu.Lock()
	sent := len(b.steers)
	b.mu.Unlock()
	if sent != 0 {
		t.Fatal("an incapable session must not be sent steer frames")
	}
	_ = c.Cancel()
	<-turn
}

func TestSteerAckStatuses(t *testing.T) {
	for _, tc := range []struct {
		ack     SteerStatus
		wantErr string
	}{
		{ack: SteerNoTurn},
		{ack: SteerUnsupported},
		{ack: SteerFailed, wantErr: "broken pipe"},
	} {
		t.Run(string(tc.ack), func(t *testing.T) {
			b := &steerBridge{capable: true, ack: tc.ack}
			h, p := wsServer(t, b.handle)
			c := connectAndClient(t, h, p)
			turn := startSteerTurn(t, c)
			st, err := c.Steer(context.Background(), "x", nil)
			if st != tc.ack || (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("steer = %q %v", st, err)
			}
			_ = c.Cancel()
			<-turn
		})
	}
}

func TestSteerWaitEnds(t *testing.T) {
	old := steerAckWait
	steerAckWait = 50 * time.Millisecond
	t.Cleanup(func() { steerAckWait = old })
	b := &steerBridge{capable: true}
	h, p := wsServer(t, b.handle)
	c := connectAndClient(t, h, p)
	turn := startSteerTurn(t, c)
	if _, err := c.Steer(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("unanswered steer = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Steer(ctx, "x", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled steer = %v", err)
	}
	_ = c.Cancel()
	<-turn
	c.Close()
	if _, err := c.Steer(context.Background(), "x", nil); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("steer on a closed client = %v", err)
	}
}
