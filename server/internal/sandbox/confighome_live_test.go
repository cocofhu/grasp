package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
)

// Live smoke: gateway config.bundleUrl inject before start.
// Requires GRASP_LIVE_GATEWAY (e.g. http://sandbox-gateway.example.com).
// Serves /sandbox-inject on a host IP reachable from the sandbox (or
// GRASP_LIVE_INJECT_ADVERTISE).
func TestLiveGatewayConfigHomeInject(t *testing.T) {
	base := strings.TrimSpace(os.Getenv("GRASP_LIVE_GATEWAY"))
	if base == "" {
		t.Skip("set GRASP_LIVE_GATEWAY to run live gateway inject test")
	}

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	mcpBody := `{"mcpServers":{"artifact-store":{"url":"http://api.example.com/mcp/runs/live-inject-test","headers":{"Authorization":"Bearer live-tok"}}}}`
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), []byte(mcpBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "rules", "base.md"), []byte("# live inject rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewBundleStore()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/sandbox-inject/", func(w http.ResponseWriter, r *http.Request) {
		store.ServeHTTP(w, r, strings.TrimPrefix(r.URL.Path, "/sandbox-inject/"))
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	advertise := strings.TrimSpace(os.Getenv("GRASP_LIVE_INJECT_ADVERTISE"))
	if advertise == "" {
		advertise = liveReachableAdvertise(t, ln.Addr().(*net.TCPAddr).Port)
	}
	t.Logf("inject advertise base=%s", advertise)

	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: advertise}})

	gw := NewGatewayClient(base, os.Getenv("GRASP_SANDBOX_GATEWAY_API_KEY"))
	m := NewManager(gw, ManagerOptions{
		WorkspaceDir:    "/root/workspace",
		InstallHelpers:  true,
		InjectStore:     store,
		InjectAdvertise: advertise,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	sb, err := m.Create(ctx, Spec{
		Name:       NewContainerName(),
		ConfigHome: home,
		ConfigRoot: "/root/.cursor",
		Env: map[string]string{
			"GRASP_ARTIFACT_URL":   "http://api.example.com/mcp/runs/live-inject-test",
			"GRASP_ARTIFACT_TOKEN": "live-tok",
			"GRASP_RUN_ID":         "live-inject-test",
			"SKIP_INNER_DOCKER":    "1",
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = m.DestroyByName(context.Background(), sb.ID)
	})

	lsCmd, _ := newSafeCmd("ls", "-la", "/root/.cursor", "/root/.cursor/rules")
	listing, _ := sb.creds().run(ctx, 30*time.Second, lsCmd)
	catMCP, err := newSafeCmd("cat", "--", "/root/.cursor/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sb.creds().run(ctx, 30*time.Second, catMCP)
	if err != nil {
		t.Fatalf("read mcp.json: %v\nls:\n%s\ncat:\n%s", err, listing, raw)
	}
	if !strings.Contains(string(raw), "api.example.com/mcp/runs/live-inject-test") {
		t.Fatalf("mcp.json missing expected URL (inject before start failed?)\nls:\n%s\nbody:\n%s", listing, raw)
	}
	catRule, err := newSafeCmd("cat", "--", "/root/.cursor/rules/base.md")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := sb.creds().run(ctx, 30*time.Second, catRule)
	if err != nil || !strings.Contains(string(rule), "live inject rule") {
		t.Fatalf("rules/base.md missing: %v\n%s\nls:\n%s", err, rule, listing)
	}
	// Confirm inject marker if image records it (best-effort).
	envCmd, _ := newSafeCmd("cat", "/proc/1/environ")
	envInj, _ := sb.creds().run(ctx, 15*time.Second, envCmd)
	envText := strings.ReplaceAll(string(envInj), "\x00", "\n")
	injLine := ""
	for _, line := range strings.Split(envText, "\n") {
		if strings.HasPrefix(line, "SANDBOX_INJECT=") {
			injLine = line
			break
		}
	}
	t.Logf("SANDBOX_INJECT=%s", strings.TrimSpace(injLine))
	t.Logf("live inject OK id=%s ssh=%s:%d", sb.ID, sb.SSHHost, sb.SSHPort)
}

func liveReachableAdvertise(t *testing.T, port int) string {
	t.Helper()
	// Prefer an address that can route toward the gateway-published sandbox IP.
	conn, err := net.DialTimeout("udp", "192.168.2.185:22", 2*time.Second)
	if err == nil {
		defer conn.Close()
		if la, ok := conn.LocalAddr().(*net.UDPAddr); ok && la.IP != nil {
			ip := la.IP.To4()
			if ip == nil {
				ip = la.IP
			}
			return fmt.Sprintf("http://%s:%d", ip.String(), port)
		}
	}
	// Fallback: first non-loopback IPv4.
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return fmt.Sprintf("http://%s:%d", ip.String(), port)
		}
	}
	t.Fatal("cannot derive GRASP_LIVE_INJECT_ADVERTISE host IP")
	return ""
}
