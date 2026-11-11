package sandbox

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func httpHostPort(t *testing.T, srv *httptest.Server) (string, int) {
	t.Helper()
	addr := srv.Listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

// eventLogServer serves the ws connect handshake (with an embedded eventLog and
// paging metadata) plus GET /api/events for older turns.
func eventLogServer(t *testing.T, eventLog []map[string]any, totalTurns int, hasMore bool, pageStatus int) (string, int) {
	t.Helper()
	mux := http.NewServeMux()
	handleTestLogin(mux)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.WriteJSON(map[string]any{
				"op": "connected", "sessionId": "log-sess",
				"eventLog": eventLog, "totalTurns": totalTurns, "hasMoreTurns": hasMore,
			})
		}
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		if pageStatus != http.StatusOK {
			w.WriteHeader(pageStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events":  []map[string]any{{"type": "session_update", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": "older "}}}},
			"hasMore": false,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func chunkEvent(text string) map[string]any {
	return map[string]any{"type": "session_update",
		"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": text}}}
}

func TestFetchEventLogWithPaging(t *testing.T) {
	log := []map[string]any{chunkEvent("recent")}
	h, p := eventLogServer(t, log, 60, true, http.StatusOK)
	res, sess, err := FetchEventLogWithPassword(context.Background(), h, p, testBridgePassword)
	if err != nil {
		t.Fatalf("FetchEventLog: %v", err)
	}
	if sess != "log-sess" {
		t.Errorf("session = %q", sess)
	}
	// Older page ("older ") is prepended to the recent turn.
	if res.Narration != "older recent" {
		t.Errorf("aggregated narration = %q", res.Narration)
	}
}

func TestFetchEventLogPagingErrorIsPartial(t *testing.T) {
	log := []map[string]any{chunkEvent("recent")}
	h, p := eventLogServer(t, log, 60, true, http.StatusInternalServerError)
	res, _, err := FetchEventLogWithPassword(context.Background(), h, p, testBridgePassword)
	if err != nil {
		t.Fatalf("FetchEventLog: %v", err)
	}
	if res.Narration != "recent" {
		t.Errorf("partial narration = %q", res.Narration)
	}
}

func TestFetchEventLogDialError(t *testing.T) {
	if _, _, err := FetchEventLogWithPassword(context.Background(), "127.0.0.1", 1, testBridgePassword); err == nil {
		t.Error("expected dial error")
	}
	if _, _, err := FetchEventLogRawWithPassword(context.Background(), "127.0.0.1", 1, testBridgePassword); err == nil {
		t.Error("expected raw dial error")
	}
}
