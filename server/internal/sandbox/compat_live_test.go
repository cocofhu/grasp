package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestCompatLiveGatewayCreate verifies create against the deployed gateway:
// one universal-sandbox image, ACP_BRIDGE_PASSWORD, VNC_PREVIEW, BROWSER_MCP.
//
//	GRASP_LIVE_GATEWAY=http://sandbox-gateway.example.com \
//	GRASP_CURSOR_API_KEY=crsr_… \
//	go test ./internal/sandbox/ -run TestCompatLiveGatewayCreate -v -timeout 25m
func TestCompatLiveGatewayCreate(t *testing.T) {
	base := strings.TrimSpace(os.Getenv("GRASP_LIVE_GATEWAY"))
	if base == "" {
		t.Skip("set GRASP_LIVE_GATEWAY (e.g. http://sandbox-gateway.example.com)")
	}
	apiKey := strings.TrimSpace(os.Getenv("GRASP_CURSOR_API_KEY"))
	if apiKey == "" {
		t.Fatal("GRASP_CURSOR_API_KEY required")
	}
	model := strings.TrimSpace(os.Getenv("GRASP_ACP_BRIDGE_MODEL"))
	if model == "" {
		model = "cursor-grok-4.5-high-fast"
	}

	wantImage := "universal-sandbox:local"
	gw := NewGatewayClient(base, os.Getenv("GRASP_SANDBOX_GATEWAY_API_KEY"))
	m := NewManager(gw, ManagerOptions{
		Image:          wantImage,
		WorkspaceDir:   "/root/workspace",
		InstallHelpers: true,
		CreateTimeout:  20 * time.Minute,
	})

	env := map[string]string{
		"SKIP_INNER_DOCKER": "1",
		"AGENT_PROVIDER":    "cursor",
		"ACP_BRIDGE_MODEL":  model,
		"BROWSER_MCP":       "1",
		"VNC_PREVIEW":       "1",
		"CURSOR_API_KEY":    apiKey,
	}
	ApplyPasswords(env, "compat-live-token")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	sb, err := m.Create(ctx, Spec{
		Name:      "compat-live-" + time.Now().Format("150405"),
		Image:     wantImage,
		Env:       env,
		Resources: &GWResources{CPUCores: 2, MemoryMB: 4096, DiskGi: 25},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		sb.Destroy(context.Background())
	})

	got, err := gw.Get(ctx, sb.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.Image, "universal-sandbox") {
		t.Fatalf("image = %q, want universal-sandbox", got.Image)
	}
	if got.Endpoint("session") == "" || got.Endpoint("ssh") == "" {
		t.Fatalf("missing endpoints: %+v", got.Endpoints)
	}
	if got.Endpoint("cdp") == "" || got.Endpoint("novnc") == "" {
		t.Fatalf("missing cdp/novnc (VNC_PREVIEW): %+v", got.Endpoints)
	}

	if err := WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 3*time.Minute); err != nil {
		t.Fatalf("ACP ready: %v", err)
	}
	// Env may live on the entrypoint child, not PID 1; assert via login cookie path
	// (already covered by WaitForACPReady) and gateway image/endpoints above.
	t.Logf("ok id=%s image=%s session=%s novnc=%s cdp=%s", sb.ID, got.Image, got.Endpoint("session"), got.Endpoint("novnc"), got.Endpoint("cdp"))
}
