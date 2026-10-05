package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"

	"github.com/gorilla/websocket"
)

func TestProviderBudgetHelpers(t *testing.T) {
	host := mcp.NewHost(newMemStore())

	// Configured chat timeout wins over the default.
	p := newACPProvider(host, Options{ChatTimeout: 42 * time.Second}).(*acpProvider)
	if p.chatTimeout() != 42*time.Second {
		t.Errorf("chatTimeout = %v", p.chatTimeout())
	}
	// Per-node override.
	req := NodeReq{Config: map[string]any{"timeout": 7}}
	if p.nodeChatTimeout(req) != 7*time.Minute {
		t.Errorf("nodeChatTimeout override = %v", p.nodeChatTimeout(req))
	}
	if p.nodeChatTimeout(NodeReq{Config: map[string]any{}}) != 42*time.Second {
		t.Error("nodeChatTimeout fallback")
	}

	// Default budget when unset.
	pd := newACPProvider(host, Options{}).(*acpProvider)
	if pd.chatTimeout() != 10*time.Minute {
		t.Errorf("default chatTimeout = %v", pd.chatTimeout())
	}
	if pd.sandboxAttempts() != 1 {
		t.Errorf("default attempts = %d", pd.sandboxAttempts())
	}
	pa := newACPProvider(host, Options{SandboxMaxAttempts: 4}).(*acpProvider)
	if pa.sandboxAttempts() != 4 {
		t.Errorf("attempts = %d", pa.sandboxAttempts())
	}

	// backoff returns false immediately on a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if pd.backoff(ctx, 1) {
		t.Error("backoff on cancelled ctx should return false")
	}
	// backoff with a tiny base returns true after waiting.
	pb := newACPProvider(host, Options{SandboxRetryBackoff: time.Millisecond}).(*acpProvider)
	if !pb.backoff(context.Background(), 1) {
		t.Error("backoff should complete")
	}

	// gitToken with no ProfilesRoot -> empty.
	if pd.gitToken(NodeReq{Config: map[string]any{"agent_profile": "x"}}) != "" {
		t.Error("gitToken with no profiles root should be empty")
	}
}

func TestTruncateText(t *testing.T) {
	if textutil.TruncateBytes("short", 10, "…(truncated)") != "short" {
		t.Error("no truncate")
	}
	if got := textutil.TruncateBytes("0123456789", 4, "…(truncated)"); got != "0123…(truncated)" {
		t.Errorf("truncate: %q", got)
	}
}

func TestMcpServersFromAgentConfig(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"mcp":[
		{"name":"artifact-store","url":"${GRASP_ARTIFACT_URL}","headers":{"Authorization":"Bearer ${GRASP_ARTIFACT_TOKEN}"}},
		{"name":"cmd","command":"run","args":["a"],"env":{"K":"v"}},
		{"name":"","url":"x"},
		{"name":"empty"}
	],"env":{"GITLAB_TOKEN":"tok"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{ProfilesRoot: root, MCPEndpoint: "http://mcp.local"}).(*acpProvider)
	req := NodeReq{RunID: "r1", Token: "t1", NodeID: "n1", Config: map[string]any{"agent_profile": "a"}}

	specs := p.resolvedMCPSpecs(req)
	if len(specs) != 2 {
		t.Fatalf("resolved specs = %d, want 2", len(specs))
	}
	js := p.mcpServers(req)
	s := string(js)
	if !strings.Contains(s, "artifact-store") || !strings.Contains(s, "\"command\":\"run\"") {
		t.Fatalf("mcpServers json missing url/command entries: %s", s)
	}
	if !strings.Contains(s, `"type":"http"`) {
		t.Fatalf("mcpServers HTTP entry missing type:http (codebuddy skips url-only): %s", s)
	}
	// gitToken resolves from the agent env.
	if tok := p.gitToken(req); tok != "tok" {
		t.Errorf("gitToken = %q, want tok", tok)
	}
	// No profile -> nil specs / nil json.
	if p.mcpServers(NodeReq{}) != nil {
		t.Error("no-profile mcpServers should be nil")
	}
}

func TestMcpURLUsesOptionsFallbackPassthrough(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(nil)

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{MCPEndpoint: "http://api.example.com"}).(*acpProvider)
	got := p.mcpURL(NodeReq{RunID: "run-spa"})
	want := "http://api.example.com/mcp/runs/run-spa"
	if got != want {
		t.Fatalf("mcpURL = %q, want %q", got, want)
	}
}

func TestSnapshotEvents(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	fallback := []models.AcpEvent{{Kind: "message", Text: "fb"}}

	// nil sandbox -> fallback.
	if got := p.snapshotEvents(context.Background(), nil, fallback); len(got) != 1 || got[0].Text != "fb" {
		t.Fatal("nil sandbox should return fallback")
	}
	// Unreachable host -> fallback.
	sbDead := &sandbox.Sandbox{Host: "127.0.0.1", Port: 1}
	if got := p.snapshotEvents(context.Background(), sbDead, fallback); len(got) != 1 {
		t.Fatal("dial failure should return fallback")
	}

	// Live event log server -> snapshot events.
	srv, host2, port := eventWSServer(t)
	defer srv.Close()
	sb := &sandbox.Sandbox{Host: host2, Port: port}
	got := p.snapshotEvents(context.Background(), sb, fallback)
	if len(got) == 0 || got[0].Text == "fb" {
		t.Fatalf("expected snapshot events, got %+v", got)
	}

	// Bracketed streamed turns keep their prompt/turn_end around the snapshot.
	bracketed := []models.AcpEvent{
		{Kind: models.AcpKindPrompt, Text: "Q"},
		{Kind: "message", Text: "streamed"},
		{Kind: models.AcpKindTurnEnd, Status: "completed"},
	}
	got = p.snapshotEvents(context.Background(), sb, bracketed)
	if len(got) != 4 || got[0].Text != "Q" || got[1].Text != "hello-snap" || got[2].Kind != models.AcpKindTimeline || got[3].Kind != models.AcpKindTurnEnd {
		t.Fatalf("expected re-bracketed snapshot, got %+v", got)
	}
}

func TestFindOrCreateMRCreatePath(t *testing.T) {
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		var script string
		if stdin != nil {
			b, _ := io.ReadAll(stdin)
			script = string(b)
		}
		body := command + "\n" + script
		switch {
		case strings.Contains(body, "glab mr list"):
			return []byte("[]"), nil // no existing MR -> take create path
		case strings.Contains(body, "glab mr create"):
			return []byte("Creating merge request\nhttps://gitlab.com/g/p/-/merge_requests/9\n"), nil
		}
		return []byte(""), nil
	})
	defer restore()
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	sb := &sandbox.Sandbox{Name: "sb", Host: "127.0.0.1", Port: 1, WorkspaceDir: "/root/workspace"}
	if url := p.findOrCreateMR(context.Background(), sb, "/root/workspace/p", "feature-x"); url != "https://gitlab.com/g/p/-/merge_requests/9" {
		t.Fatalf("create MR url = %q", url)
	}
}

func TestFirstMRURL(t *testing.T) {
	if firstMRURL(`[{"web_url":"http://x/1"}]`) != "http://x/1" {
		t.Error("firstMRURL")
	}
	if firstMRURL(`[]`) != "" {
		t.Error("firstMRURL empty")
	}
	if firstMRURL(`not-json`) != "" {
		t.Error("firstMRURL bad")
	}
	if firstMRURL(`[{"other":1}]`) != "" {
		t.Error("firstMRURL no web_url")
	}
}

// TestRunAgentRetiresToStore wires a registry that also implements
// RunSandboxRetirer so retireRunSandbox hands the container to the store's
// idle-TTL sweeper instead of destroying it inline.
func TestRunAgentRetiresToStore(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	mgr := newFakeManager(t, host, "r", "n", tok, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "ok", produces: map[string]string{"report.md": "x"}}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	reg := &retiringRegistry{}
	p.registry = reg

	req := reqWithProfile(NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testPlainCaps, Token: tok,
		Config: map[string]any{"prompt": "go", "produces": "report.md"}, Vars: map[string]any{}})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	reg.mu.Lock()
	retired, registered := reg.retired, reg.registered
	reg.mu.Unlock()
	if registered == 0 {
		t.Error("expected the sandbox to be registered")
	}
	if retired == 0 {
		t.Error("expected the sandbox to be retired to the store")
	}
}

// eventWSServer mimics the cursor-acp bridge event-log handshake so
// FetchEventLog returns one aggregated event.
func eventWSServer(t *testing.T) (*httptest.Server, string, int) {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if strings.Contains(string(msg), "connect") {
				c.WriteJSON(map[string]any{
					"op":        "connected",
					"sessionId": "s1",
					"eventLog": []map[string]any{{
						"op": "event", "data": map[string]any{
							"type": "session_update",
							"update": map[string]any{
								"sessionUpdate": "agent_message_chunk",
								"content":       map[string]any{"type": "text", "text": "hello-snap"},
							},
						},
					}},
					"totalTurns":   1,
					"hasMoreTurns": false,
				})
			}
		}
	}))
	_, portStr, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	port, _ := strconv.Atoi(portStr)
	return srv, "127.0.0.1", port
}
