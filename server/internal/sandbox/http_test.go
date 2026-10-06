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

func TestFetchCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"protocol":"1.0","changes":{"endpoint":"/api/changes","vcs":"git"},"ide":{"codeServer":true,"port":8080}}`))
	}))
	defer srv.Close()
	h, p := httpHostPort(t, srv)
	caps, err := fetchCapabilities(context.Background(), h, p)
	if err != nil {
		t.Fatalf("FetchCapabilities: %v", err)
	}
	if !caps.supportsChanges() {
		t.Error("expected SupportsChanges true")
	}
	var nilCaps *Capabilities
	if nilCaps.supportsChanges() {
		t.Error("nil caps must not support changes")
	}
}

func TestFetchCapabilitiesErrors(t *testing.T) {
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()
	h, p := httpHostPort(t, notFound)
	if _, err := fetchCapabilities(context.Background(), h, p); err == nil {
		t.Error("expected 404 error")
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer badJSON.Close()
	h2, p2 := httpHostPort(t, badJSON)
	if _, err := fetchCapabilities(context.Background(), h2, p2); err == nil {
		t.Error("expected decode error")
	}
}

func TestFetchChanges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"vcs":"git","branch":"feat","pushed":true,"changedFiles":[{"path":"a.go","status":"M","added":3}]}`))
	}))
	defer srv.Close()
	h, p := httpHostPort(t, srv)
	ch, err := fetchChanges(context.Background(), h, p)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}
	if ch.Branch != "feat" || !ch.Pushed || len(ch.ChangedFiles) != 1 {
		t.Errorf("changes = %+v", ch)
	}

	nf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer nf.Close()
	h2, p2 := httpHostPort(t, nf)
	if _, err := fetchChanges(context.Background(), h2, p2); err == nil {
		t.Error("expected non-200 error")
	}
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
