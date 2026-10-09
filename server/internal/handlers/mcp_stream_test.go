package handlers_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

type mcpHTTPClient struct {
	t     *testing.T
	ctx   context.Context
	http  *http.Client
	url   string
	token string
	sid   string
}

func newMCPHTTPClient(t *testing.T, srv *httptest.Server, runID, token string) *mcpHTTPClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c := &mcpHTTPClient{t: t, ctx: ctx, http: srv.Client(), url: srv.URL + "/mcp/runs/" + runID, token: token}
	resp := c.request(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize: %d", resp.StatusCode)
	}
	c.sid = resp.Header.Get("Mcp-Session-Id")
	if c.sid == "" {
		t.Fatal("initialize did not assign an MCP session")
	}
	var out struct {
		Result struct {
			Capabilities struct {
				Tools struct{ ListChanged bool } `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || !out.Result.Capabilities.Tools.ListChanged {
		t.Fatalf("initialize must advertise listChanged, err=%v result=%+v", err, out)
	}
	return c
}

func (c *mcpHTTPClient) request(method, body, accept string) *http.Response {
	c.t.Helper()
	req, err := http.NewRequestWithContext(c.ctx, method, c.url, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.sid != "" {
		req.Header.Set("Mcp-Session-Id", c.sid)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp
}

func (c *mcpHTTPClient) rpc(body string) map[string]any {
	c.t.Helper()
	resp := c.request(http.MethodPost, body, "application/json, text/event-stream")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("RPC status=%d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.t.Fatal(err)
	}
	if out["error"] != nil {
		c.t.Fatalf("RPC error: %v", out)
	}
	return out["result"].(map[string]any)
}

func (c *mcpHTTPClient) tools() map[string]bool {
	c.t.Helper()
	names := map[string]bool{}
	for _, tool := range c.rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)["tools"].([]any) {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	return names
}

func (c *mcpHTTPClient) listen() (*http.Response, *bufio.Scanner) {
	c.t.Helper()
	resp := c.request(http.MethodGet, "", "text/event-stream")
	c.t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		c.t.Fatalf("SSE status=%d headers=%v", resp.StatusCode, resp.Header)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		c.t.Fatal("SSE must disable proxy buffering")
	}
	return resp, bufio.NewScanner(resp.Body)
}

func expectToolsChanged(t *testing.T, scan *bufio.Scanner) {
	t.Helper()
	event, data := "", ""
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		} else if line == "" && data != "" {
			var notification map[string]any
			if err := json.Unmarshal([]byte(data), &notification); err != nil {
				t.Fatal(err)
			}
			if event != "message" || notification["jsonrpc"] != "2.0" || notification["method"] != "notifications/tools/list_changed" || notification["id"] != nil {
				t.Fatalf("unexpected SSE notification: event=%q data=%s", event, data)
			}
			return
		}
	}
	t.Fatalf("SSE ended before notification: %v", scan.Err())
}

func TestMCPStreamRefreshesCachedPhase1Tools(t *testing.T) {
	h := newHarness(t)
	token := h.host.RegisterRun("run-stream")
	h.host.SetActiveNode("run-stream", "clarify", &models.AgentCapabilities{Interaction: models.InteractionClarify})
	srv := httptest.NewServer(h.r)
	defer srv.Close()
	c := newMCPHTTPClient(t, srv, "run-stream", token)
	cached := c.tools()
	if cached["node_complete"] {
		t.Fatal("Phase1 must hide node_complete")
	}
	_, scan := c.listen()
	h.host.SetOutcomeAllowed("run-stream", true)
	expectToolsChanged(t, scan)
	// The simulated client only refreshes its cache in response to the SSE
	// notification, then permits the tool call through that refreshed cache.
	cached = c.tools()
	if !cached["node_complete"] {
		t.Fatal("notification did not lead to Phase2 tool discovery")
	}
	call := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success","summary":"confirmed"}}}`
	if out := c.rpc(call); out["isError"] == true || !h.host.HasOutcome("run-stream", "clarify") {
		t.Fatalf("Phase2 tool call failed: %v", out)
	}
	h.host.SetOutcomeAllowed("run-stream", false)
	expectToolsChanged(t, scan)
	if c.tools()["node_complete"] || c.rpc(call)["isError"] != true {
		t.Fatal("revoking Phase2 must hide and reject node_complete")
	}
	h.host.UnregisterRun("run-stream")
	if scan.Scan() {
		t.Fatalf("unregister must close stream, got %q", scan.Text())
	}
}

func TestMCPStreamSessionsReconnectAndDelete(t *testing.T) {
	h := newHarness(t)
	token := h.host.RegisterRun("run-stream")
	srv := httptest.NewServer(h.r)
	defer srv.Close()
	a := newMCPHTTPClient(t, srv, "run-stream", token)
	b := newMCPHTTPClient(t, srv, "run-stream", token)
	if a.sid == b.sid {
		t.Fatal("different clients need distinct sessions")
	}
	_, old := a.listen()
	_, current := a.listen()
	if old.Scan() {
		t.Fatal("reconnecting a session must close its previous stream")
	}
	_, other := b.listen()
	h.host.SetOutcomeAllowed("run-stream", true)
	expectToolsChanged(t, current)
	expectToolsChanged(t, other)
	resp := a.request(http.MethodDelete, "", "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || current.Scan() {
		t.Fatal("DELETE must close only its own session's stream")
	}
	resp = a.request(http.MethodPost, `{"jsonrpc":"2.0","id":4,"method":"ping"}`, "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted session should be 404, got %d", resp.StatusCode)
	}
	h.host.SetOutcomeAllowed("run-stream", false)
	expectToolsChanged(t, other)
	// Disconnect B, change the surface offline, and reconnect. The new GET
	// must invalidate even though no stream existed when the change occurred.
	resp = b.request(http.MethodGet, "", "text/event-stream")
	_ = resp.Body.Close()
	h.host.SetOutcomeAllowed("run-stream", true)
	_, reconnected := b.listen()
	expectToolsChanged(t, reconnected)
	h.host.UnregisterRun("run-stream")
	if reconnected.Scan() {
		t.Fatal("unregister must close reconnected stream")
	}
}

func TestMCPStreamRunIsolationAndAuthorization(t *testing.T) {
	h := newHarness(t)
	tokenA := h.host.RegisterRun("run-a")
	tokenB := h.host.RegisterRun("run-b")
	srv := httptest.NewServer(h.r)
	defer srv.Close()
	a := newMCPHTTPClient(t, srv, "run-a", tokenA)
	b := newMCPHTTPClient(t, srv, "run-b", tokenB)
	b.sid = a.sid
	resp := b.request(http.MethodGet, "", "text/event-stream")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-run session must be 404, got %d", resp.StatusCode)
	}
	a.token = tokenB
	resp = a.request(http.MethodGet, "", "text/event-stream")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token must be 401, got %d", resp.StatusCode)
	}
	a.token = tokenA
	resp = a.request(http.MethodGet, "", "application/json")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("GET without SSE Accept must be 406, got %d", resp.StatusCode)
	}
	h.host.SetOutcomeAllowed("run-a", true)
	_, stream := a.listen()
	expectToolsChanged(t, stream) // catches a change made before the first GET
	h.host.UnregisterRun("run-a")
	if stream.Scan() {
		t.Fatal("unregister must close stream")
	}
}
