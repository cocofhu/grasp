package handlers

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func eventHeartbeatWSPair(t *testing.T, pongWait, pingPeriod time.Duration) (*websocket.Conn, <-chan struct{}) {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		configureEventHeartbeat(conn, pongWait)
		writer := &wsWriter{conn: conn}
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		ping := time.NewTicker(pingPeriod)
		defer ping.Stop()
		for {
			select {
			case <-readDone:
				return
			case <-ping.C:
				if err := writeEventHeartbeat(conn, writer.write); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, done
}

func readEventHeartbeat(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, frame, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if string(frame) != `{"type":"heartbeat"}` {
		t.Fatalf("heartbeat frame = %s", frame)
	}
}

func requireEventHeartbeatClosed(t *testing.T, conn *websocket.Conn, done <-chan struct{}) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				t.Fatal("unresponsive peer stayed connected until the client read deadline")
			}
			break
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat handler did not release the disconnected subscriber")
	}
}

func TestEventHeartbeatClosesPeerWithoutPong(t *testing.T) {
	conn, done := eventHeartbeatWSPair(t, 150*time.Millisecond, 20*time.Millisecond)
	conn.SetPingHandler(func(string) error { return nil })
	readEventHeartbeat(t, conn)
	requireEventHeartbeatClosed(t, conn, done)
}

func TestEventHeartbeatPongKeepsIdlePeerConnected(t *testing.T) {
	const pongWait = 150 * time.Millisecond
	conn, done := eventHeartbeatWSPair(t, pongWait, 20*time.Millisecond)
	started := time.Now()
	// No chat/control frames are sent. Protocol pong replies alone must keep
	// the subscription alive while the model is silent for multiple deadlines.
	for time.Since(started) < 3*pongWait {
		readEventHeartbeat(t, conn)
	}
	conn.SetPingHandler(func(string) error { return nil })
	requireEventHeartbeatClosed(t, conn, done)
}

func TestEventHeartbeatReturnsDataWriteFailure(t *testing.T) {
	conn, _ := eventHeartbeatWSPair(t, time.Second, time.Hour)
	want := errors.New("data heartbeat write failed")
	if err := writeEventHeartbeat(conn, func([]byte) error { return want }); !errors.Is(err, want) {
		t.Fatalf("write heartbeat error = %v, want %v", err, want)
	}
}
