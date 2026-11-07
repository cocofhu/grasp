package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

func transportFixture(t *testing.T, driver string, targets map[int]string) (*GatewayClient, *httptest.Server, *atomic.Int32) {
	t.Helper()
	var channels atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-gateway-key" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/api/v2/info" {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "driver": driver, "transport": tunnelProtocol, "ports": map[string]int{"session": 8765, "ide": 8744, "ssh": 22, "cdp": 9222, "novnc": 6080}, "profiles": map[string]any{"universal": map[string]string{"image": "approved-image"}}})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v2/sandboxes/sb-one/connect/") {
			port, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/v2/sandboxes/sb-one/connect/"))
			target := targets[port]
			if target == "" {
				http.Error(w, "missing", 404)
				return
			}
			tcp, err := net.DialTimeout("tcp", target, 3*time.Second)
			if err != nil {
				http.Error(w, "unavailable", 502)
				return
			}
			defer tcp.Close()
			upgrader := websocket.Upgrader{Subprotocols: []string{tunnelProtocol}}
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			channels.Add(1)
			relayTunnel(r.Context(), ws, tcp)
			return
		}
		if strings.Contains(r.URL.Path, "/hosts/") || strings.HasSuffix(r.URL.Path, "/ports") {
			_ = json.NewEncoder(w).Encode(map[string]string{"address": "192.0.2.200:45173"})
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		eps := map[string]string{}
		for key, port := range map[string]int{"session": 8765, "ide": 8744, "ssh": 22, "cdp": 9222, "novnc": 6080} {
			if targets[port] != "" {
				eps[key], eps[strconv.Itoa(port)] = "192.0.2.200:40000", "192.0.2.200:40000"
			}
		}
		_ = json.NewEncoder(w).Encode(GWSandbox{ID: "sb-one", Status: "running", Endpoints: eps})
	}))
	t.Cleanup(server.Close)
	g, err := NewConfiguredGatewayClient(config.SandboxConfig{GatewayURL: server.URL, GatewayAPIKey: "synthetic-gateway-key", GatewayTransport: "gateway"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g, server, &channels
}

func TestGatewayTransportHTTPWebSocketCDPAndPreview(t *testing.T) {
	for _, backend := range []string{"docker", "kubernetes", "e2b"} {
		t.Run(backend, func(t *testing.T) {
			var keyLeaked atomic.Bool
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					keyLeaked.Store(true)
				}
				if r.URL.Path == "/json/version" {
					_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": "ws://192.0.2.200:9222/devtools/browser/retained"})
					return
				}
				if websocket.IsWebSocketUpgrade(r) {
					ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer ws.Close()
					for {
						kind, data, err := ws.ReadMessage()
						if err != nil {
							return
						}
						if ws.WriteMessage(kind, data) != nil {
							return
						}
					}
				}
				w.Header().Set("X-Target-Cookie", r.Header.Get("Cookie"))
				if r.Method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					_, _ = w.Write(body)
				} else {
					_, _ = io.WriteString(w, "retained-workspace")
				}
			}))
			t.Cleanup(target.Close)
			targets := map[int]string{}
			for _, port := range []int{8765, 8744, 9222, 6080, 5173} {
				targets[port] = strings.TrimPrefix(target.URL, "http://")
			}
			g, _, channels := transportFixture(t, backend, targets)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := g.WaitTransportReady(ctx); err != nil {
				t.Fatal(err)
			}
			sb, err := g.Get(ctx, "sb-one")
			if err != nil {
				t.Fatal(err)
			}
			payload := bytes.Repeat([]byte{0, 1, 2, 255}, 40_000)
			httpClient := &http.Client{Timeout: 5 * time.Second}
			defer httpClient.CloseIdleConnections()
			for _, key := range []string{"session", "ide", "novnc"} {
				addr := sb.Endpoint(key)
				if !strings.HasPrefix(addr, "127.0.0.1:") || addr == "192.0.2.200:40000" {
					t.Fatalf("not adapted: %s", addr)
				}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/events", bytes.NewReader(payload))
				req.Header.Set("Cookie", "agentchat_session=retained")
				resp, err := httpClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil || !bytes.Equal(body, payload) || resp.Header.Get("X-Target-Cookie") != "agentchat_session=retained" {
					t.Fatalf("%s HTTP body/cookie changed: status=%d bytes=%d cookie=%q err=%v", key, resp.StatusCode, len(body), resp.Header.Get("X-Target-Cookie"), err)
				}
				ws, _, err := websocket.DefaultDialer.DialContext(ctx, "ws://"+addr+"/websocket", nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := ws.WriteMessage(websocket.BinaryMessage, payload); err != nil {
					t.Fatal(err)
				}
				kind, body, err := ws.ReadMessage()
				_ = ws.Close()
				if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(body, payload) {
					t.Fatalf("%s WS data changed: %v", key, err)
				}
			}
			wsURL, err := launcher.ResolveURL("http://" + sb.Endpoint("cdp"))
			if err != nil || !strings.HasPrefix(wsURL, "ws://"+sb.Endpoint("cdp")+"/devtools/") {
				t.Fatalf("CDP discovery escaped the gateway: %s %v", wsURL, err)
			}
			ws, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = ws.Close()
			mgr := NewManager(g, ManagerOptions{})
			preview, err := mgr.HostForPort(ctx, "sb-one", 5173)
			if err != nil {
				t.Fatal(err)
			}
			published, err := mgr.PublishPort(ctx, "sb-one", 5173)
			if err != nil || published != preview {
				t.Fatalf("dynamic preview mapping: %s %s %v", preview, published, err)
			}
			resp, err := httpClient.Get("http://" + preview)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(body) != "retained-workspace" || keyLeaked.Load() || channels.Load() < 8 {
				t.Fatalf("transport changed application behavior: body=%s leaked=%t channels=%d", body, keyLeaked.Load(), channels.Load())
			}
			if err := g.Destroy(ctx, "sb-one"); err != nil {
				t.Fatal(err)
			}
			if conn, err := net.DialTimeout("tcp", preview, time.Second); err == nil {
				_ = conn.Close()
				t.Fatal("destroy retained a listener")
			}
		})
	}
}

func TestGatewayTransportSSHInputOutputAndExitStatus(t *testing.T) {
	output := bytes.Repeat([]byte{0, 1, 255, 'a'}, 32_000)
	creds := startSSHOutputServer(t, func(channel ssh.Channel, command string, input []byte) uint32 {
		if string(input) != "retained-file-contents" || !strings.Contains(command, "bash") {
			return 90
		}
		_, _ = channel.Write(output)
		_, _ = io.WriteString(channel.Stderr(), "stderr-retained")
		return 7
	})
	g, _, _ := transportFixture(t, "e2b", map[int]string{22: net.JoinHostPort(creds.host, strconv.Itoa(creds.port)), 8765: net.JoinHostPort(creds.host, strconv.Itoa(creds.port))})
	sb, err := g.Get(context.Background(), "sb-one")
	if err != nil {
		t.Fatal(err)
	}
	creds.host, creds.port = hostPortOf(sb.Endpoint("ssh"))
	command, _ := newSafeCmd("bash", "-s")
	got, err := creds.runInput(context.Background(), 5*time.Second, command, strings.NewReader("retained-file-contents"))
	var exit *ssh.ExitError
	// The two SSH channels can be read in either order. Preserve all bytes
	// without assuming stderr cannot land between two stdout packets.
	stdout := bytes.Replace(got, []byte("stderr-retained"), nil, 1)
	if !errors.As(err, &exit) || exit.ExitStatus() != 7 || !bytes.Equal(stdout, output) || !bytes.Contains(got, []byte("stderr-retained")) {
		t.Fatalf("SSH streams or exit lost: bytes=%d err=%v", len(got), err)
	}
}

func TestGatewayTransportTCPHalfCloseAndCancellation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		body, _ := io.ReadAll(conn)
		_, _ = conn.Write(append([]byte("after-stdin-eof:"), body...))
	}()
	g, _, _ := transportFixture(t, "docker", map[int]string{8765: ln.Addr().String()})
	sb, err := g.Get(context.Background(), "sb-one")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", sb.Endpoint("session"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(conn, "retained")
	_ = conn.(*net.TCPConn).CloseWrite()
	body, err := io.ReadAll(conn)
	_ = conn.Close()
	if err != nil || string(body) != "after-stdin-eof:retained" {
		t.Fatalf("half-close truncated output: %q %v", body, err)
	}
	<-done
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.bridge.endpoint("sb-one", 8765); err == nil {
		t.Fatal("closed bridge accepted endpoint")
	}
}

func TestGatewayTransportDiscoveryAndProfileFailClosed(t *testing.T) {
	for _, metadata := range []string{`{"version":1}`, `{"version":2,"driver":"e2b","transport":"grasp-tcp-v1","ports":{"ssh":22}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, metadata) }))
		g, err := NewConfiguredGatewayClient(config.SandboxConfig{GatewayURL: server.URL, GatewayAPIKey: "key", GatewayTransport: "gateway"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Create(context.Background(), GWCreateRequest{}); err == nil {
			t.Fatal("unsupported discovery accepted")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		if err := g.WaitTransportReady(ctx); err == nil {
			t.Fatal("unsupported gateway became ready")
		}
		cancel()
		_ = g.Close()
		server.Close()
	}
	g, _, _ := transportFixture(t, "kubernetes", nil)
	g.profile = "missing"
	if _, err := g.Create(context.Background(), GWCreateRequest{}); err == nil {
		t.Fatal("missing profile accepted")
	}
	g.profile = "universal"
	req := GWCreateRequest{Image: "old-OCI-image"}
	if err := g.prepareCreate(context.Background(), &req); err != nil || req.Profile != "universal" || req.Image != "" {
		t.Fatalf("provider-independent profile: %+v %v", req, err)
	}
}

func TestGatewayTransportClosesActiveConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		_, _ = io.Copy(io.Discard, conn)
	}()
	g, _, _ := transportFixture(t, "docker", map[int]string{8765: ln.Addr().String()})
	sb, err := g.Get(context.Background(), "sb-one")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", sb.Endpoint("session"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("channel never connected")
	}
	closed := make(chan struct{})
	go func() { _ = g.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown retained an active channel")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown retained the provider socket")
	}
}

func TestMissingTransportRouteDoesNotMeanSandboxWasDestroyed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/info" || strings.HasSuffix(r.URL.Path, "/missing") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(GWSandbox{ID: "retained", Status: "running", Endpoints: map[string]string{"session": "192.0.2.200:8765"}})
	}))
	t.Cleanup(server.Close)
	g, err := NewConfiguredGatewayClient(config.SandboxConfig{GatewayURL: server.URL, GatewayAPIKey: "synthetic-key", GatewayTransport: "gateway"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	_, err = g.Get(context.Background(), "retained")
	if !errors.Is(err, ErrGatewayTransport) || GatewayRecordMissing(err) {
		t.Fatalf("transport failure classified as destroyed sandbox: %v", err)
	}
	_, err = g.Get(context.Background(), "missing")
	if !GatewayRecordMissing(err) {
		t.Fatalf("missing sandbox record was not recognized: %v", err)
	}
}
