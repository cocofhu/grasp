package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/textutil"
)

// TestCursorLiveMCP verifies the in-container cursor-agent natively connects
// to the run-scoped artifact-store MCP (injected at ACP session/new + via
// /root/.cursor/mcp.json) and calls write_artifact. No produces contract is
// set, so harvest does NOT run: an artifact in the store proves the MCP path.
//
// Gated by GRASP_LIVE_MCP=1 (+CURSOR_API_KEY). Needs Docker.
func TestCursorLiveMCP(t *testing.T) {
	if os.Getenv("GRASP_LIVE_MCP") != "1" {
		t.Skip("set GRASP_LIVE_MCP=1 (+CURSOR_API_KEY) to run")
	}
	apiKey := os.Getenv("GRASP_CURSOR_API_KEY")
	if apiKey == "" {
		t.Fatal("GRASP_CURSOR_API_KEY required")
	}
	// Empty → DefaultSandboxImage (universal-sandbox:local).
	image := os.Getenv("GRASP_SANDBOX_IMAGE")
	model := getenvOr("GRASP_ACP_BRIDGE_MODEL", "cursor-grok-4.5-high-fast")

	store := newMemStore()
	host := mcp.NewHost(store)
	runID := "mcp-run-1"
	tok := host.RegisterRun(runID)
	defer host.UnregisterRun(runID)

	// Serve the MCP route on all interfaces so the container can reach it via
	// host.docker.internal:<port>.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/runs/", func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimPrefix(r.URL.Path, "/mcp/runs/")
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if !host.AuthorizeRun(rid, token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, _ := io.ReadAll(r.Body)
		status, resp := host.ServeRPC(rid, token, body)
		t.Logf("MCP rpc: %s -> %d", textutil.TruncateBytes(string(body), 160, "…(truncated)"), status)
		if resp == nil {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(resp)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	gatewayURL := getenvOr("GRASP_SANDBOX_GATEWAY_URL", "http://127.0.0.1:8899")
	agentEnv := map[string]string{
		"GRASP_CURSOR_API_KEY": apiKey,
		"CURSOR_API_KEY":       apiKey,
		"ACP_BRIDGE_MODEL":     model,
	}
	envJSON, _ := json.Marshal(agentEnv)
	agentJSON := `{"acpBackend":"cursor","mcp":[{"name":"artifact-store","url":"${GRASP_ARTIFACT_URL}","headers":{"Authorization":"Bearer ${GRASP_ARTIFACT_TOKEN}"}}],"env":` + string(envJSON) + `}`
	profilesRoot := writeAgent(t, "go-backend", agentJSON)
	t.Logf("model=%s image=%q mcp=http://host.docker.internal:%d gateway=%s", model, image, port, gatewayURL)
	provider := newACPProvider(host, Options{
		SandboxImage: image,
		GatewayURL:   gatewayURL,
		NodeHardCap:  10 * time.Minute,
		MCPEndpoint:  fmt.Sprintf("http://host.docker.internal:%d", port),
		ProfilesRoot: profilesRoot,
	})

	req := NodeReq{
		RunID:    runID,
		Token:    tok,
		NodeID:   "mcp-node",
		NodeType: "agent", Caps: testPlainCaps,
		Config: map[string]any{
			"agent_profile": "go-backend",
			"prompt": "请直接调用 artifact-store MCP 的 write_artifact 工具,写入一个名为 result.json 的产物," +
				"内容为 {\"ok\": true, \"via\": \"mcp\"}。只用 MCP 工具完成,不要在磁盘创建文件。完成后回复一句确认。",
		},
		Vars: map[string]any{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	res, err := provider.RunAgent(ctx, req)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	t.Logf("narration: %s", firstLine(res.OutputMd))

	content, ok := store.Get(runID, "result.json")
	if !ok {
		t.Fatalf("result.json not in store: agent did not call write_artifact via MCP")
	}
	t.Logf("artifact via MCP: result.json = %s", content)
	if !strings.Contains(content, "ok") {
		t.Errorf("unexpected artifact content: %s", content)
	}
}

func getenvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
