package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/gorilla/websocket"
)

func TestPublicGateEventsWSStreamsSanitizedAcp(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-pub-ws", "research-ws", true)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-pub-ws/reviews/research-ws/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	if !gateshare.ValidTokenShape(token) {
		t.Fatalf("token: %s", token)
	}

	srv := httptest.NewServer(h.r)
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/public/gate-approvals/events"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.WriteJSON(map[string]any{"token": token}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	_, ready, err := c.ReadMessage()
	if err != nil || !strings.Contains(string(ready), `"type":"ready"`) {
		t.Fatalf("ready: %v %s", err, ready)
	}

	h.h.Eng.Broker().Publish("run-pub-ws", mustJSON(t, map[string]any{
		"type":   "acp",
		"runId":  "run-pub-ws",
		"nodeId": "research-ws",
		"busy":   true,
		"events": []any{
			map[string]any{"kind": "thought", "text": "思考 http://127.0.0.1/api/runs/x"},
			map[string]any{"kind": "message", "text": "标题已改为绿色"},
			map[string]any{"kind": "tool_call", "title": "write", "text": "secret"},
		},
	}))
	h.h.Eng.Broker().Publish("run-pub-ws", mustJSON(t, map[string]any{
		"type":   "acp",
		"runId":  "run-pub-ws",
		"nodeId": "other-node",
		"events": []any{map[string]any{"kind": "message", "text": "should-not-leak"}},
	}))
	h.h.Eng.Broker().Publish("run-pub-ws", mustJSON(t, map[string]any{
		"type":   "review",
		"runId":  "run-pub-ws",
		"nodeId": "research-ws",
		"event":  "turn_begin",
		"item":   map[string]any{"text": "改成绿的"},
	}))

	deadline := time.Now().Add(3 * time.Second)
	sawAcp, sawReview := false, false
	for time.Now().Before(deadline) && (!sawAcp || !sawReview) {
		_ = c.SetReadDeadline(time.Now().Add(time.Until(deadline) + 50*time.Millisecond))
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}
		s := string(msg)
		if strings.Contains(s, "run-pub-ws") || strings.Contains(s, "research-ws") || strings.Contains(s, "should-not-leak") || strings.Contains(s, "tool_call") {
			t.Fatalf("leaked: %s", s)
		}
		if strings.Contains(s, `"type":"acp"`) && strings.Contains(s, "标题已改为绿色") {
			sawAcp = true
			if strings.Contains(s, "127.0.0.1") {
				t.Fatalf("url leak: %s", s)
			}
		}
		if strings.Contains(s, `"event":"turn_begin"`) && strings.Contains(s, "改成绿的") {
			sawReview = true
		}
	}
	if !sawAcp || !sawReview {
		t.Fatalf("sawAcp=%v sawReview=%v", sawAcp, sawReview)
	}
}

// lastTurnProvider keeps serving the previous turn's snapshot, as the sandbox
// event log does once a turn has finished.
type lastTurnProvider struct{ fakeProvider }

func (lastTurnProvider) LiveNodeEvents(ctx context.Context, runID, nodeID string) ([]models.AcpEvent, bool, error) {
	return []models.AcpEvent{{Kind: "message", Text: "上一轮的回复"}}, true, nil
}

func TestPublicGateEventsWSSkipsStaleSeedWhenIdle(t *testing.T) {
	h := newHarness(t)
	old := h.h.Eng
	eng := engine.New(h.db, lastTurnProvider{}, h.host, h.h.Arts, 5)
	h.h.Eng = eng
	t.Cleanup(func() {
		eng.Close()
		h.h.Eng = old
	})
	seedInboxReview(t, h, "run-pub-idle", "research-idle", true)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-pub-idle/reviews/research-idle/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	srv := httptest.NewServer(h.r)
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/public/gate-approvals/events"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.WriteJSON(map[string]any{"token": token}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if _, ready, err := c.ReadMessage(); err != nil || !strings.Contains(string(ready), `"type":"ready"`) {
		t.Fatalf("ready: %v %s", err, ready)
	}
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}
		if strings.Contains(string(msg), "上一轮的回复") {
			t.Fatalf("idle session seeded the previous turn: %s", msg)
		}
	}
}

func TestPublicGateEventsWSRejectsBadToken(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(h.r)
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/public/gate-approvals/events"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.WriteJSON(map[string]any{"token": "ab"}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(msg), `"status":"invalid"`) {
		t.Fatalf("want invalid, got %s", msg)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
