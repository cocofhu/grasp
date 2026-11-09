package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerStartupFailureRetainsProviderAndDisk(t *testing.T) {
	for _, scenario := range []string{"provider_error", "timeout", "cancel", "configure"} {
		t.Run(scenario, func(t *testing.T) {
			workspace := filepath.Join(t.TempDir(), "retained-workspace")
			if err := os.WriteFile(workspace, []byte("original files"), 0o600); err != nil {
				t.Fatal(err)
			}
			var deletes, creates atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "configuration unavailable", http.StatusServiceUnavailable)
			}))
			defer backend.Close()
			host, portText, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
			port, _ := strconv.Atoi(portText)
			const resource = "retained-provider-id"
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sandboxes":
					creates.Add(1)
				case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/sandboxes/"+resource:
					deletes.Add(1)
					_ = os.Remove(workspace)
					w.WriteHeader(http.StatusNoContent)
					return
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sandboxes/"+resource:
					if scenario == "cancel" {
						cancel()
					}
				default:
					http.NotFound(w, r)
					return
				}
				status := "creating"
				if scenario == "provider_error" {
					status = "error"
				} else if scenario == "configure" {
					status = "running"
				}
				_ = json.NewEncoder(w).Encode(GWSandbox{ID: resource, Status: status, Labels: map[string]string{cfNameLabel: "original-correlation"}, Endpoints: map[string]string{"session": net.JoinHostPort(host, strconv.Itoa(port))}})
			}))
			defer gateway.Close()
			opts := ManagerOptions{}
			if scenario == "configure" {
				opts.WorkspaceOperations = &WorkspaceRuntimeConfig{Lineage: "original-lineage"}
			}
			manager := NewManager(NewGatewayClient(gateway.URL, ""), opts)
			manager.createTimeout = time.Nanosecond
			if scenario == "cancel" {
				manager.createTimeout = time.Minute
			}
			sb, err := manager.Create(ctx, Spec{Name: "original-correlation", Env: map[string]string{BridgePasswordEnv: testBridgePassword}})
			if !errors.Is(err, ErrCreateOutcomeUnknown) || sb == nil || sb.ID != resource || sb.CorrelationName != "original-correlation" || sb.Password != testBridgePassword {
				t.Fatalf("lost original startup receipt: sb=%+v err=%v", sb, err)
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("caller cancellation not preserved: %v", err)
			}
			if creates.Load() != 1 || deletes.Load() != 0 {
				t.Fatalf("startup changed resources: creates=%d deletes=%d", creates.Load(), deletes.Load())
			}
			if body, err := os.ReadFile(workspace); err != nil || string(body) != "original files" {
				t.Fatalf("startup removed original disk: %q %v", body, err)
			}
			// Explicit destruction remains available; only implicit startup
			// cleanup is forbidden from deleting a retained provider resource.
			if err := manager.DestroyByName(t.Context(), resource); err != nil || deletes.Load() != 1 {
				t.Fatalf("explicit delete stopped working: deletes=%d err=%v", deletes.Load(), err)
			}
		})
	}
}

func TestManagerEndpointAdaptFailureKeepsAcceptedProviderIdentity(t *testing.T) {
	client, gateway := newInlineGW(t)
	client.id = "original-k8s-gateway"
	client.bridge = newGatewayBridge(client)
	client.bridge.metadata = &GatewayInfo{Version: 2, Driver: "k8s", Transport: tunnelProtocol, Ports: map[string]int{"session": 8765, "ide": 8080, "ssh": 2222, "cdp": 9222, "novnc": 6080}}
	client.bridge.close() // POST succeeds; local endpoint publication fails.
	manager := NewManager(client, ManagerOptions{})
	sb, err := manager.Create(t.Context(), Spec{Name: "accepted-original", Env: map[string]string{BridgePasswordEnv: testBridgePassword}})
	if !errors.Is(err, ErrCreateOutcomeUnknown) || sb == nil || sb.ID != qualifiedGatewayID(client.id, "gw-1") || sb.CorrelationName != "accepted-original" {
		t.Fatalf("accepted provider ID lost on endpoint failure: %+v %v", sb, err)
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.seq != 1 || gateway.recs["gw-1"] == nil {
		t.Fatal("local bridge failure destroyed or repeated provider creation")
	}
}
