package handlers_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gorilla/websocket"
)

// wsURL converts an http(s) test-server URL + path into a ws:// dial URL.
func wsURL(base, path string) string {
	return "ws" + strings.TrimPrefix(base, "http") + path
}

func TestRunEventsWS(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "rw", Status: "running", StartedAt: time.Now(), Graph: models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
	}})
	srv := httptest.NewServer(h.r)
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/runs/rw/events"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	// First frame is the snapshot.
	_, msg, err := c.ReadMessage()
	if err != nil || !strings.Contains(string(msg), "snapshot") {
		t.Fatalf("snapshot frame: %v %s", err, msg)
	}
	// A broker publish is forwarded to the socket as the next frame.
	h.h.Eng.Broker().Publish("rw", []byte(`{"type":"state","nodeId":"in"}`))
	_, msg2, err := c.ReadMessage()
	if err != nil || !strings.Contains(string(msg2), "state") {
		t.Fatalf("broker frame: %v %s", err, msg2)
	}
}

// TestRunEventsWSReviewEnqueueError ensures review_chat failures surface as
// type:"review" event:"error" (not silently dropped).
func TestRunEventsWSReviewEnqueueError(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "rw-rev", Status: "running", StartedAt: time.Now(), Graph: models.Graph{
		Nodes: []models.Node{{ID: "proposal", Type: "agent"}},
	}})
	srv := httptest.NewServer(h.r)
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/runs/rw-rev/events"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = c.ReadMessage() // snapshot
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// Empty content → EnqueueReviewTurn error → review/error frame.
	if err := c.WriteJSON(map[string]any{
		"type": "review_chat", "nodeId": "proposal", "content": "",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	saw := false
	for i := 0; i < 5; i++ {
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}
		s := string(msg)
		if strings.Contains(s, `"type":"review"`) && strings.Contains(s, `"event":"error"`) {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatal("expected review error frame for empty enqueue")
	}
}

func TestSandboxChatWS(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Sandbox{Name: "grasp-sb-cw", Purpose: "test", Status: "stopped"})
	var row models.Sandbox
	h.db.Where("name = ?", "grasp-sb-cw").First(&row)

	srv := httptest.NewServer(h.r)
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/sandboxes/"+uintToStr(row.ID)+"/chat"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Send a chat turn; the container isn't running so the worker reports an
	// error frame after turn_begin.
	if err := c.WriteJSON(map[string]any{"type": "chat", "content": "hi"}); err != nil {
		t.Fatalf("write chat: %v", err)
	}
	_, first, err := c.ReadMessage()
	if err != nil || !strings.Contains(string(first), `"event":"queue_state"`) {
		t.Fatalf("first frame should be the queue snapshot: %v %s", err, first)
	}
	sawError := false
	for i := 0; i < 10; i++ {
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}
		if strings.Contains(string(msg), `"event":"error"`) {
			sawError = true
			break
		}
	}
	if !sawError {
		t.Fatal("expected an error frame for a stopped-container chat")
	}
	// Exercise the cancel branch too.
	_ = c.WriteJSON(map[string]any{"type": "cancel"})
	time.Sleep(50 * time.Millisecond)
}

func TestSandboxTerminalWS(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Sandbox{Name: "grasp-sb-tw", Purpose: "test", Status: "stopped"})
	var row models.Sandbox
	h.db.Where("name = ?", "grasp-sb-tw").First(&row)

	srv := httptest.NewServer(h.r)
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/sandboxes/"+uintToStr(row.ID)+"/terminal"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	// OpenTerminal fails (container not running) -> error frame then close.
	_, msg, err := c.ReadMessage()
	if err != nil || !strings.Contains(string(msg), "error") {
		t.Fatalf("terminal error frame: %v %s", err, msg)
	}
}
