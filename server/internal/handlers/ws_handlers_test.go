package handlers_test

import (
	"encoding/json"
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

// Snapshot uses the same runDetailDTO as GetRun, so it must carry the run title
// and must not blank it after connect.
func TestRunEventsWSSnapshotIncludesTitle(t *testing.T) {
	h := newHarness(t)
	if err := h.db.Create(&models.Run{
		ID: "rw-title", WorkflowID: "wf-self", WorkflowName: "自我迭代",
		Title: "这个行高有点高了", Status: "running", StartedAt: time.Now(),
		Graph: models.Graph{Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}}},
	}).Error; err != nil {
		t.Fatalf("seed run: %v", err)
	}
	srv := httptest.NewServer(h.r)
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/runs/rw-title/events"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("snapshot frame: %v", err)
	}
	var frame struct {
		Type string         `json:"type"`
		Run  map[string]any `json:"run"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil {
		t.Fatalf("decode snapshot: %v %s", err, msg)
	}
	if frame.Type != "snapshot" {
		t.Fatalf("type=%q want snapshot", frame.Type)
	}
	if frame.Run["title"] != "这个行高有点高了" {
		t.Fatalf("snapshot run.title=%v", frame.Run["title"])
	}
	if frame.Run["workflowName"] != "自我迭代" {
		t.Fatalf("snapshot workflowName=%v", frame.Run["workflowName"])
	}

	detail := h.do("GET", "/api/runs/rw-title", nil)
	if detail.Code != 200 {
		t.Fatalf("get run: %d %s", detail.Code, detail.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["title"] != frame.Run["title"] {
		t.Fatalf("GetRun title=%v snapshot title=%v", got["title"], frame.Run["title"])
	}
}

// TestRunEventsWSReviewEnqueueError ensures review_chat failures surface as
// type:"review" event:"error" (not silently dropped).
func TestRunEventsWSReviewEnqueueError(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "rw-rev", Status: "running", StartedAt: time.Now(), Graph: models.Graph{
		Nodes: []models.Node{{ID: "research", Type: "agent"}},
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
		"type": "review_chat", "nodeId": "research", "content": "",
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

// TestRunEventsWSClarifyVisitEnded: a WS chat to an ended clarify visit gets
// the same contract as HTTP 409 (dropped + clientMessageId), on the sending
// connection only; another subscriber of the run sees nothing.
func TestRunEventsWSClarifyVisitEnded(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	runID := "rw-clar-ended"
	h.db.Create(&models.Run{ID: runID, Status: "cancelled", StartedAt: now, Graph: models.Graph{
		Nodes: []models.Node{{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "澄清"}},
	}})
	h.db.Create(&models.ReactConversation{RunID: runID, NodeID: "react", Iteration: 2, Messages: []models.ReactMessage{{Role: "agent", Text: "q?"}}})
	h.db.Create(&models.StateRun{RunID: runID, NodeID: "react", Iteration: 2, Status: "cancelled"})
	srv := httptest.NewServer(h.r)
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/runs/"+runID+"/events"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	other, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/runs/"+runID+"/events"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial other: %v", err)
	}
	defer other.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	other.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := other.ReadMessage(); err != nil {
		t.Fatalf("other snapshot: %v", err)
	}
	const cid = "0f8e2a6c-3b1d-4c7e-9a5f-6d2b8c4e1a70"
	if err := c.WriteJSON(map[string]any{
		"type": "chat", "nodeId": "react", "content": "我的回答", "clientMessageId": cid,
		"images":      []map[string]any{{"data": "QUJD", "mimeType": "image/png", "name": "a.png"}},
		"annotations": []map[string]any{{"selector": "#x", "note": "改这里"}},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var f map[string]any
	for i := 0; i < 5; i++ {
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}
		var m map[string]any
		if json.Unmarshal(msg, &m) == nil && m["event"] == "error" {
			f = m
			break
		}
	}
	if f == nil {
		t.Fatal("no error frame")
	}
	if f["type"] != "review" || f["code"] != "clarify_visit_ended" || f["message"] != "运行已结束，这条消息没有发送" ||
		f["autoRetryOnResume"] != false || f["nodeId"] != "react" || f["iteration"] != float64(2) {
		t.Fatalf("frame=%v", f)
	}
	d, _ := f["dropped"].(map[string]any)
	if d == nil || d["text"] != "我的回答" {
		t.Fatalf("dropped=%v", f["dropped"])
	}
	if a, _ := d["attachments"].([]any); len(a) != 1 || a[0].(map[string]any)["data"] != "QUJD" {
		t.Fatalf("attachments must echo as sent: %v", d["attachments"])
	}
	if a, _ := d["annotations"].([]any); len(a) != 1 || a[0].(map[string]any)["note"] != "改这里" {
		t.Fatalf("annotations=%v", d["annotations"])
	}
	if f["clientMessageId"] != cid {
		t.Fatalf("clientMessageId=%v want %s", f["clientMessageId"], cid)
	}
	// The other subscriber must not receive the reject (it carries the
	// sender's content). Drain until a short deadline; any error frame fails.
	other.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for {
		_, msg, err := other.ReadMessage()
		if err != nil {
			break // deadline: nothing more arrived
		}
		if strings.Contains(string(msg), "clarify_visit_ended") || strings.Contains(string(msg), "我的回答") {
			t.Fatalf("enqueue reject leaked to another subscriber: %s", msg)
		}
	}
}
