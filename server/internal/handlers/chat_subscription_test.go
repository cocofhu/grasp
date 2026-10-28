package handlers

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"

	"github.com/gorilla/websocket"
)

func chatSubscriptionWSPair(t *testing.T, ch <-chan chatsession.Event, write func(any) error) (*websocket.Conn, <-chan struct{}) {
	t.Helper()
	pumpDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(pumpDone)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if write == nil {
			write = conn.WriteJSON
		}
		pumpChatSubscription(conn, ch, write, nil)
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, pumpDone
}

func requirePumpedSocketClosed(t *testing.T, conn *websocket.Conn, pumpDone <-chan struct{}) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("websocket read succeeded after the subscription writer stopped")
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatal("websocket stayed open until the read deadline")
		}
	}
	select {
	case <-pumpDone:
	case <-time.After(time.Second):
		t.Fatal("subscription writer did not return")
	}
}

func TestPumpChatSubscriptionClosesWebSocket(t *testing.T) {
	ch := make(chan chatsession.Event, 1)
	conn, pumpDone := chatSubscriptionWSPair(t, ch, nil)
	ch <- chatsession.Event{Type: chatsession.FrameSession, Event: chatsession.EventTurnDone, Payload: map[string]any{"interrupted": false}}

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read forwarded frame: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("decode forwarded frame: %v", err)
	}
	if frame["event"] != chatsession.EventTurnDone || frame["interrupted"] != false {
		t.Fatalf("forwarded frame = %#v", frame)
	}

	close(ch)
	requirePumpedSocketClosed(t, conn, pumpDone)
}

func TestPumpSubscriptionCloseWhileWriteBlocked(t *testing.T) {
	ch := make(chan chatsession.Event, 1)
	ch <- chatsession.Event{Type: chatsession.FrameSession, Event: chatsession.EventTurnDone, Payload: map[string]any{}}
	writeEntered := make(chan struct{})
	unblockWrite := make(chan struct{})
	conn, pumpDone := chatSubscriptionWSPair(t, ch, func(any) error {
		close(writeEntered)
		<-unblockWrite
		return nil
	})

	select {
	case <-writeEntered:
	case <-time.After(time.Second):
		close(unblockWrite)
		t.Fatal("blocking writer was not entered")
	}
	close(ch)
	select {
	case <-pumpDone:
	case <-time.After(250 * time.Millisecond):
		t.Error("pump remained blocked after the subscription channel closed")
	}
	_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Error("websocket stayed open while the data write was still blocked")
	}
	close(unblockWrite)
	select {
	case <-pumpDone:
	case <-time.After(time.Second):
		t.Fatal("pump did not exit after the blocked writer was released")
	}
}

func TestPumpChatSubscriptionWriteFailureClosesWebSocket(t *testing.T) {
	ch := make(chan chatsession.Event, 1)
	ch <- chatsession.Event{Type: chatsession.FrameSession, Event: chatsession.EventError, Payload: map[string]any{}}
	writeCalled := make(chan any, 1)
	writeErr := errors.New("injected write failure")
	conn, pumpDone := chatSubscriptionWSPair(t, ch, func(v any) error {
		writeCalled <- v
		return writeErr
	})

	requirePumpedSocketClosed(t, conn, pumpDone)
	select {
	case v := <-writeCalled:
		frame, ok := v.(map[string]any)
		if !ok || frame["event"] != chatsession.EventError {
			t.Fatalf("written frame = %#v", v)
		}
	default:
		t.Fatal("injected writer was not called")
	}
}
