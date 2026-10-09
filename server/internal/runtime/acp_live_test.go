package runtime

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
)

// memStore is an in-memory mcp.Store for the live test.
type memStore struct {
	mu   sync.Mutex
	data map[string]map[string]string // runID -> name -> content
	node map[string]map[string]string // runID -> name -> nodeID
	rev  map[string]map[string]int    // runID -> name -> revision
}

func newMemStore() *memStore {
	return &memStore{
		data: map[string]map[string]string{},
		node: map[string]map[string]string{},
	}
}

func (s *memStore) Save(runID, nodeID, name, kind, content string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[runID] == nil {
		s.data[runID] = map[string]string{}
	}
	if s.node[runID] == nil {
		s.node[runID] = map[string]string{}
	}
	if s.rev == nil {
		s.rev = map[string]map[string]int{}
	}
	if s.rev[runID] == nil {
		s.rev[runID] = map[string]int{}
	}
	if prev, ok := s.data[runID][name]; !ok {
		s.rev[runID][name] = 1
	} else if prev != content {
		if s.rev[runID][name] < 1 {
			s.rev[runID][name] = 1
		}
		s.rev[runID][name]++
	}
	s.data[runID][name] = content
	s.node[runID][name] = nodeID
	return runID + "/" + name, nil
}

func (s *memStore) Stamps(runID string) []mcp.ArtifactStamp {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []mcp.ArtifactStamp
	for name := range s.data[runID] {
		out = append(out, mcp.ArtifactStamp{Name: name, Node: s.node[runID][name], Revision: s.rev[runID][name]})
	}
	return out
}

func (s *memStore) Get(runID, name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.data[runID][name]
	return c, ok
}

func (s *memStore) Delete(runID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[runID] != nil {
		delete(s.data[runID], name)
	}
	if s.node[runID] != nil {
		delete(s.node[runID], name)
	}
	if s.rev[runID] != nil {
		delete(s.rev[runID], name)
	}
	return nil
}

func (s *memStore) List(runID string) []mcp.ArtifactInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []mcp.ArtifactInfo
	for name, c := range s.data[runID] {
		out = append(out, mcp.ArtifactInfo{Name: name, Node: s.node[runID][name], Size: len(c)})
	}
	return out
}

// TestCursorLiveRunAgent exercises the real sandbox path end-to-end:
// launch a container, drive cursor-agent over ACP, harvest the declared
// produces file and write it through the run-scoped MCP host.
//
// Gated by GRASP_LIVE=1 (needs Docker + GRASP_CURSOR_API_KEY); the
// default test run stays credential-free on the mock provider.
func TestCursorLiveRunAgent(t *testing.T) {
	if os.Getenv("GRASP_LIVE") != "1" {
		t.Skip("set GRASP_LIVE=1 (and GRASP_CURSOR_API_KEY) to run the live sandbox test")
	}
	apiKey := os.Getenv("GRASP_CURSOR_API_KEY")
	if apiKey == "" {
		t.Fatal("GRASP_CURSOR_API_KEY required for live test")
	}

	store := newMemStore()
	host := mcp.NewHost(store)
	runID := "live-run-1"
	token := host.RegisterRun(runID)
	defer host.UnregisterRun(runID)

	image := os.Getenv("GRASP_SANDBOX_IMAGE")
	gatewayURL := os.Getenv("GRASP_SANDBOX_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://127.0.0.1:8899"
	}
	// Auth keys must come from Agent env (platform opts.Env skips CURSOR_API_KEY).
	model := os.Getenv("GRASP_ACP_BRIDGE_MODEL")
	if model == "" {
		model = "cursor-grok-4.5-high-fast"
	}
	// Local MCP endpoint so artifact-store / node_complete are actually injected
	// (previous live runs omitted this and only left BROWSER_MCP chrome-devtools).
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	mcpPort := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/runs/", func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimPrefix(r.URL.Path, "/mcp/runs/")
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		host.ServeRunHTTP(w, r, rid, token)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	agentEnv := map[string]string{
		"GRASP_CURSOR_API_KEY": apiKey,
		"CURSOR_API_KEY":       apiKey,
		"ACP_BRIDGE_MODEL":     model,
	}
	envJSON, _ := json.Marshal(agentEnv)
	profiles := writeAgent(t, "backend-dev", `{"acpBackend":"cursor","mcp":[{"name":"artifact-store","url":"${GRASP_ARTIFACT_URL}","headers":{"Authorization":"Bearer ${GRASP_ARTIFACT_TOKEN}"}}],"env":`+string(envJSON)+`}`)
	provider := newACPProvider(host, Options{
		SandboxImage: image, // empty → universal-sandbox:local
		GatewayURL:   gatewayURL,
		ProfilesRoot: profiles,
		NodeHardCap:  8 * time.Minute,
		MCPEndpoint:  "http://host.docker.internal:" + strconv.Itoa(mcpPort),
	})

	req := NodeReq{
		RunID:    runID,
		Token:    token,
		NodeID:   "implement",
		NodeType: "agent", Caps: testPlainCaps,
		Config: map[string]any{
			"agent_profile": "backend-dev",
			"prompt": "在工作目录创建文件 report.md。" +
				"文件必须包含至少两行非空 Markdown，第一行是标题 `# Live Selftest`，第二行是一句项目说明。" +
				"创建后用 read 工具确认文件非空。不要创建其他文件。",
			"produces": "report.md",
		},
		Vars: map[string]any{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	res, err := provider.RunAgent(ctx, req)
	if err != nil {
		t.Fatalf("RunAgent failed: %v", err)
	}
	if res.OutputMd == "" {
		t.Error("expected non-empty narration output")
	}
	content, ok := store.Get(runID, "report.md")
	if !ok {
		t.Fatal("produces contract not satisfied: report.md missing from store")
	}
	if len(content) == 0 {
		t.Error("report.md harvested but empty")
	}
	t.Logf("harvested report.md (%d bytes), events=%d", len(content), len(res.Events))
}
