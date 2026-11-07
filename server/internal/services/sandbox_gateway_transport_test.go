package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestGatewayTransportRefreshesPersistedPortsAndNeverFallsBackWhenUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-key" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/api/v2/info" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "driver": "e2b", "transport": "grasp-tcp-v1", "ports": map[string]int{"session": 8765, "ide": 8744, "ssh": 22, "cdp": 9222, "novnc": 6080}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "retained", "status": "running", "endpoints": map[string]string{"session": "192.0.2.200:8765", "ide": "192.0.2.200:8744"}})
	}))
	t.Cleanup(server.Close)
	g, err := sandbox.NewConfiguredGatewayClient(config.SandboxConfig{GatewayURL: server.URL, GatewayAPIKey: "synthetic-key", GatewayTransport: "gateway"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	s := newSandboxService(t, newTestDB(t), &dockerState{})
	s.mgr = sandbox.NewManager(g, sandbox.ManagerOptions{})
	row := models.Sandbox{Name: "retained", Token: "persisted-token", Host: "127.0.0.1", ACPPort: 9, CodeServerPort: 9}
	if err := s.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	// Even an old live object must not override the newly resolved endpoint.
	s.live[row.ID] = &liveSandbox{sb: &sandbox.Sandbox{Host: "127.0.0.1", Port: 9, Password: "stale-token"}}
	host, port, token, err := s.acpConnection(context.Background(), row.ID)
	if err != nil || host != "127.0.0.1" || port == 9 || port == 0 || token != row.Token {
		t.Fatalf("stale ACP connection retained: %s:%d token-match=%t err=%v", host, port, token == row.Token, err)
	}
	ide, err := s.IDEUpstream(context.Background(), row.ID)
	if err != nil || !strings.HasPrefix(ide, "127.0.0.1:") || ide == "127.0.0.1:9" {
		t.Fatalf("stale IDE retained: %s %v", ide, err)
	}
	view, err := s.GetView(context.Background(), row.ID)
	if err != nil || view.EndpointTransport != "gateway" {
		t.Fatalf("server-local endpoints were not identified: %+v %v", view, err)
	}
	server.Close()
	var staleProbes atomic.Int32
	stalePreview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		staleProbes.Add(1)
		w.Header().Set("X-Grasp-Upstream", "5173")
	}))
	t.Cleanup(stalePreview.Close)
	preview := &PreviewService{mgr: s.mgr}
	if base, healthy := preview.ProbeRegisteredPort(context.Background(), mcp.PreviewPort{SandboxName: row.Name, Host: stalePreview.URL, Port: 5173}); healthy || base != "" || staleProbes.Load() != 0 {
		t.Fatalf("preview probed a stale local endpoint: base=%s healthy=%t probes=%d", base, healthy, staleProbes.Load())
	}
	if _, _, _, err := s.acpConnection(context.Background(), row.ID); err == nil {
		t.Fatal("ACP fell back to saved loopback port")
	}
	if _, err := s.ACPUpstream(context.Background(), row.ID); err == nil {
		t.Fatal("ACP proxy fell back to saved loopback port")
	}
	if _, err := s.IDEUpstream(context.Background(), row.ID); err == nil {
		t.Fatal("IDE fell back to saved loopback port")
	}
	row.Name = "gw1_removed_cmV0YWluZWQ"
	if err := s.db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.acpConnection(context.Background(), row.ID); err == nil {
		t.Fatal("removed gateway binding fell back to saved loopback port")
	}
	if _, err := s.IDEUpstream(context.Background(), row.ID); err == nil {
		t.Fatal("removed gateway binding fell back to saved IDE port")
	}
}
