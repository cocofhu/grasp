package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/driver/fake"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type transportDriver struct {
	*fake.Driver
	targets map[int]string
}

func (d *transportDriver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	if _, err := d.Get(ctx, id); err != nil {
		return nil, err
	}
	return d.targets, nil
}

func connectFixture(t *testing.T, backend string, targets map[int]string) (*httptest.Server, *service.SandboxService, string) {
	t.Helper()
	cfg := config.Default()
	cfg.Driver, cfg.Auth.APIKeys = backend, []string{"synthetic-transport-key"}
	cfg.Profiles = map[string]config.ProfileConfig{"universal": {Image: "approved-environment"}}
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "gateway.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	drv := &transportDriver{Driver: fake.New(), targets: targets}
	svc := service.New(drv, store.New(db), service.Config{Image: "legacy-image", Ports: []int{8765}, SessionPort: 8765, FinalizeTimeout: time.Second, Resources: config.ResourceDefaults{DefaultCPUCores: 2, DefaultMemoryMB: 4096, DefaultDiskGi: 25}})
	sb, err := svc.Create(context.Background(), service.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		record, err := svc.Get(sb.ID)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not running: %+v", record)
		}
		time.Sleep(time.Millisecond)
	}
	server := httptest.NewServer(NewRouter(NewHandler(svc, cfg.Image.Ports), cfg))
	t.Cleanup(server.Close)
	t.Cleanup(func() { waitSettled(t, svc) })
	return server, svc, sb.ID
}

func tcpResponseAfterEOF(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				input, _ := io.ReadAll(conn)
				_, _ = conn.Write(append([]byte("after-eof:"), input...))
			}()
		}
	}()
	return ln.Addr().String()
}

func TestConnectWirePreservesBinaryAndHalfCloseForEveryDriver(t *testing.T) {
	for _, backend := range []string{"docker", "kubernetes", "e2b"} {
		t.Run(backend, func(t *testing.T) {
			target := tcpResponseAfterEOF(t)
			server, _, id := connectFixture(t, backend, map[int]string{8765: target, 9222: target})
			dialer := websocket.Dialer{Subprotocols: []string{tunnelProtocol}, HandshakeTimeout: time.Second}
			ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v2/sandboxes/"+id+"/connect/9222", http.Header{"Authorization": []string{"Bearer synthetic-transport-key"}})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			payload := bytes.Repeat([]byte{0, 1, 255, 10}, 50_000)
			for offset := 0; offset < len(payload); offset += 10_000 {
				if err := ws.WriteMessage(websocket.BinaryMessage, payload[offset:offset+10_000]); err != nil {
					t.Fatal(err)
				}
			}
			if err := ws.WriteMessage(websocket.TextMessage, []byte("eof")); err != nil {
				t.Fatal(err)
			}
			var result []byte
			for {
				kind, body, err := ws.ReadMessage()
				if err != nil {
					t.Fatalf("truncated after %d bytes: %v", len(result), err)
				}
				if kind == websocket.TextMessage && string(body) == "eof" {
					break
				}
				if kind != websocket.BinaryMessage {
					t.Fatalf("unexpected message %d", kind)
				}
				result = append(result, body...)
			}
			if !bytes.Equal(result, append([]byte("after-eof:"), payload...)) {
				t.Fatal("binary stream changed or truncated")
			}
		})
	}
}

func TestConnectAuthenticationAndTargetScope(t *testing.T) {
	target := tcpResponseAfterEOF(t)
	server, svc, id := connectFixture(t, "docker", map[int]string{8765: target})
	for _, tc := range []struct {
		path, key, origin, protocol string
		status                      int
	}{
		{"/api/v2/info", "", "", "", 401},
		{"/api/v2/info", "wrong", "", "", 401},
		{"/api/v2/sandboxes/" + id + "/connect/0", "synthetic-transport-key", "", tunnelProtocol, 400},
		{"/api/v2/sandboxes/" + id + "/connect/65536", "synthetic-transport-key", "", tunnelProtocol, 400},
		{"/api/v2/sandboxes/" + id + "/connect/ssh", "synthetic-transport-key", "", tunnelProtocol, 400},
		{"/api/v2/sandboxes/" + id + "/connect/8765?host=127.0.0.1:22", "synthetic-transport-key", "", tunnelProtocol, 400},
		{"/api/v2/sandboxes/" + id + "/connect/8765", "synthetic-transport-key", "http://browser.test", tunnelProtocol, 400},
		{"/api/v2/sandboxes/" + id + "/connect/8765", "synthetic-transport-key", "", "wrong", 400},
		{"/api/v2/sandboxes/missing/connect/8765", "synthetic-transport-key", "", tunnelProtocol, 404},
		{"/api/v2/sandboxes/" + id + "/connect/22", "synthetic-transport-key", "", tunnelProtocol, 404},
	} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+tc.key)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-WebSocket-Protocol", tc.protocol)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status %d want %d", tc.path, resp.StatusCode, tc.status)
		}
	}
	if err := svc.Stop(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConnectionTarget(context.Background(), id, 8765); err == nil {
		t.Fatal("stopped sandbox allowed a connection")
	}
	r, _, _ := testRouter(t, []string{" "})
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/info", nil))
	if response.Code != 503 {
		t.Fatalf("anonymous transport enabled: %d", response.Code)
	}
}

func TestGatewayInfoReportsBackendSemantics(t *testing.T) {
	for backend, want := range map[string][2]string{"docker": {"cold", "unlimited"}, "kubernetes": {"cold", "pvc"}, "e2b": {"pause", "template-minimum-free"}} {
		cfg := config.Default()
		cfg.Driver = backend
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		gatewayInfo(cfg)(ctx)
		var info struct {
			Capabilities struct {
				StopMode string `json:"stopMode"`
				DiskMode string `json:"diskMode"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		if info.Capabilities.StopMode != want[0] || info.Capabilities.DiskMode != want[1] {
			t.Fatalf("%s capabilities: %+v", backend, info)
		}
	}
}

func TestEnvironmentProfileResolvesImageAndRejectsUnknownOrConflictingInput(t *testing.T) {
	target := tcpResponseAfterEOF(t)
	server, svc, _ := connectFixture(t, "docker", map[int]string{8765: target})
	for body, status := range map[string]int{`{"profile":"missing"}`: 400, `{"profile":"universal","image":"wrong"}`: 400, `{"profile":"universal"}`: 202} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/sandboxes", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer synthetic-transport-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var sb struct {
			Image string `json:"image"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&sb)
		_ = resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("%s: %d want %d", body, resp.StatusCode, status)
		}
		if status == 202 && sb.Image != "approved-environment" {
			t.Fatalf("profile image: %s", sb.Image)
		}
	}
	waitSettled(t, svc)
}
