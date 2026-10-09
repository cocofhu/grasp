package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestToolSessionInvalidationsCoalesceAndCleanUp(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	token := h.RegisterRun("a")
	otherToken := h.RegisterRun("b")
	sid := h.newToolSession("a", token)
	otherSID := h.newToolSession("b", otherToken)
	stream := &toolStream{wake: make(chan struct{}, 1), done: make(chan struct{})}
	other := &toolStream{wake: make(chan struct{}, 1), done: make(chan struct{})}
	h.toolSessions["a"][sid].stream = stream
	h.toolSessions["b"][otherSID].stream = other
	h.SetOutcomeAllowed("a", true)
	<-stream.wake
	h.SetOutcomeAllowed("a", true)
	select {
	case <-stream.wake:
		t.Fatal("setting the same flag must not send another notification")
	default:
	}
	for i := 0; i < 100; i++ {
		h.SetOutcomeAllowed("a", i%2 != 0)
	}
	if len(stream.wake) != 1 {
		t.Fatal("slow subscriber must retain exactly one invalidation")
	}
	if len(other.wake) != 0 {
		t.Fatal("notification leaked to another run")
	}
	h.UnregisterRun("a")
	select {
	case <-stream.done:
	default:
		t.Fatal("unregister must close the stream")
	}
	if h.hasToolSession("a", sid) || h.OutcomeAllowed("a") || h.ToolsListGeneration("a") != 0 {
		t.Fatal("unregister must clear session and tool-surface state")
	}
	if !h.hasToolSession("b", otherSID) {
		t.Fatal("unregister affected another run")
	}
	h.deleteToolSession("b", otherSID)
	h.deleteToolSession("b", otherSID) // idempotent cleanup must not close twice
	if len(h.toolSessions) != 0 {
		t.Fatal("empty session maps must be removed")
	}
	if h.newToolSession("a", token) != "" {
		t.Fatal("revoked token must not create an orphan session")
	}
}

func TestToolSessionConcurrentChangeAndCleanup(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	token := h.RegisterRun("r")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				sid := h.newToolSession("r", token)
				h.SetOutcomeAllowed("r", j%2 == 0)
				h.deleteToolSession("r", sid)
			}
		}()
	}
	wg.Wait()
	h.UnregisterRun("r")
}

func TestAcceptsEventStream(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false}, {"application/json", false}, {"*/*", false},
		{"text/event-stream", true}, {"application/json, text/event-stream; q=0.5", true},
		{"text/event-stream; q=0", false}, {"text/event-stream; q=bogus", false},
		{"text/event-stream; q=2", false}, {"text/event-stream; broken", false},
	} {
		if got := acceptsEventStream(tc.value); got != tc.want {
			t.Errorf("Accept %q: got %v want %v", tc.value, got, tc.want)
		}
	}
}

type brokenMCPBody struct{}

func (brokenMCPBody) Read([]byte) (int, error) { return 0, errors.New("broken request body") }
func (brokenMCPBody) Close() error             { return nil }

func TestRunHTTPTransportErrorsAndLegacyPOST(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	token := h.RegisterRun("r")
	sid := h.newToolSession("r", token)
	for _, tc := range []struct {
		name, method, session, accept, body string
		status                              int
	}{
		{"legacy ping", "POST", "", "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, 200},
		{"notification", "POST", sid, "", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, 202},
		{"parse error", "POST", "", "", `{`, 400},
		{"missing GET session", "GET", "", "text/event-stream", "", 400},
		{"missing DELETE session", "DELETE", "", "", "", 400},
		{"unknown session", "POST", "missing", "", `{}`, 404},
		{"wrong Accept", "GET", sid, "application/json", "", 406},
		{"unsupported method", "PUT", "", "", "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/mcp", strings.NewReader(tc.body))
			r.Header.Set(mcpSessionHeader, tc.session)
			r.Header.Set("Accept", tc.accept)
			w := httptest.NewRecorder()
			h.ServeRunHTTP(w, r, "r", token)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body)
			}
			if tc.status == 202 && w.Body.Len() != 0 {
				t.Fatal("notification must not return a body")
			}
		})
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.Body = brokenMCPBody{}
	h.ServeRunHTTP(w, r, "r", token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("broken body: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeRunHTTP(w, r, "r", "wrong")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", w.Code)
	}
	h.deleteToolSession("r", sid)
	w = httptest.NewRecorder()
	h.serveToolStream(w, httptest.NewRequest(http.MethodGet, "/mcp", nil), "r", sid)
	if w.Code != http.StatusNotFound {
		t.Fatal("session removed during GET must fail closed")
	}
	for _, origin := range []string{"https://mcp.example", "https://evil.example", "null", "http://mcp.example/path", "http://%"} {
		r := httptest.NewRequest(http.MethodPost, "https://mcp.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeRunHTTP(w, r, "r", token)
		want := http.StatusForbidden
		if origin == "https://mcp.example" {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Errorf("Origin %q: status=%d want=%d", origin, w.Code, want)
		}
	}
}

// A transport that cannot flush must release its subscription immediately.
type unflushableMCPWriter struct{ header http.Header }

func (w *unflushableMCPWriter) Header() http.Header       { return w.header }
func (*unflushableMCPWriter) Write(p []byte) (int, error) { return len(p), nil }
func (*unflushableMCPWriter) WriteHeader(int)             {}

func TestToolStreamCannotFlushReleasesSubscription(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	token := h.RegisterRun("r")
	sid := h.newToolSession("r", token)
	w := &unflushableMCPWriter{header: make(http.Header)}
	h.serveToolStream(w, httptest.NewRequest(http.MethodGet, "/mcp", nil), "r", sid)
	if h.toolSessions["r"][sid].stream != nil {
		t.Fatal("unflushable stream leaked a subscription")
	}
	h.deleteToolSession("r", sid)
}

func TestToolStreamCanceledRequestReleasesSubscription(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	token := h.RegisterRun("r")
	sid := h.newToolSession("r", token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serveToolStream(httptest.NewRecorder(), r, "r", sid)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled request did not release its stream")
	}
	if h.toolSessions["r"][sid].stream != nil {
		t.Fatal("canceled request leaked a subscription")
	}
}
