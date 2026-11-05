package e2b

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type relayFixture struct {
	server    *httptest.Server
	mu        sync.Mutex
	ports     map[int]bool
	token     string
	active    map[*websocket.Conn]bool
	cdpStatus int
}

func newRelayFixture(t *testing.T) *relayFixture {
	t.Helper()
	f := &relayFixture{ports: make(map[int]bool), token: "traffic-one", active: make(map[*websocket.Conn]bool)}
	upgrade := websocket.Upgrader{Subprotocols: []string{"binary"}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		token := f.token
		f.mu.Unlock()
		if r.Header.Get("E2b-Sandbox-Id") != "test-guest" || r.Header.Get("E2b-Sandbox-Port") != "8081" ||
			r.Header.Get("e2b-traffic-access-token") != token || r.Header.Get("X-Grasp-Relay-Key") != "relay-key" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/_bridge/ports":
			var body struct{ Ports []int }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "body", 400)
				return
			}
			f.mu.Lock()
			for _, p := range body.Ports {
				f.ports[p] = true
			}
			f.mu.Unlock()
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/_bridge/cdp":
			f.mu.Lock()
			status := f.cdpStatus
			f.mu.Unlock()
			if status != 0 {
				http.Error(w, "not ready", status)
				return
			}
			if !cdpDiscoveryPath(r.Header.Get("X-Grasp-CDP-Path")) {
				http.Error(w, "path", 400)
				return
			}
			_, _ = io.WriteString(w, `[{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/page/existing?x=1"}]`)
		case "/_bridge/tcp":
			port, _ := strconv.Atoi(r.URL.Query().Get("port"))
			f.mu.Lock()
			allowed := f.ports[port]
			f.mu.Unlock()
			if !allowed {
				http.Error(w, "port", 403)
				return
			}
			conn, err := upgrade.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.active[conn] = true
			f.mu.Unlock()
			defer func() {
				_ = conn.Close()
				f.mu.Lock()
				delete(f.active, conn)
				f.mu.Unlock()
			}()
			for {
				kind, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if kind == websocket.TextMessage && string(data) == "EOF" {
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("after-eof"))
					return
				}
				if err := conn.WriteMessage(kind, data); err != nil {
					return
				}
			}
		default:
			http.Error(w, "missing", 404)
		}
	}))
	t.Cleanup(func() {
		f.mu.Lock()
		for conn := range f.active {
			_ = conn.Close()
		}
		f.mu.Unlock()
		f.server.Close()
	})
	return f
}

func fixtureManager(t *testing.T) (*TunnelManager, *relayFixture, RelayCredentials) {
	t.Helper()
	f := newRelayFixture(t)
	m, err := NewTunnelManager("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.CloseAll() })
	return m, f, RelayCredentials{SandboxID: "test-guest", TrafficToken: "traffic-one", RelayKey: "relay-key", EndpointURL: f.server.URL}
}

func echoAt(t *testing.T, address, text string) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, text); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len(text))
	if _, err := io.ReadFull(conn, data); err != nil || string(data) != text {
		t.Fatalf("TCP echo %q: %v", data, err)
	}
	return conn.(*net.TCPConn)
}

func TestTunnelPrivateRoutingRotationAndHalfClose(t *testing.T) {
	m, f, creds := fixtureManager(t)
	address, err := m.Start(context.Background(), creds, 22, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatalf("non-loopback endpoint %s", address)
	}
	conn := echoAt(t, address, "real TCP over private WS")
	if err := conn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	remaining, err := io.ReadAll(conn)
	if err != nil || string(remaining) != "after-eof" {
		t.Fatalf("half-close lost response %q: %v", remaining, err)
	}
	f.mu.Lock()
	f.token = "traffic-two"
	f.mu.Unlock()
	creds.TrafficToken = "traffic-two"
	if err := m.UpdateCredentials(creds); err != nil {
		t.Fatal(err)
	}
	echoAt(t, address, "rotated token, same listener")
	if got, ok := m.Get(creds.SandboxID, 22); !ok || got != address || m.List(creds.SandboxID)[22] != address {
		t.Fatal("endpoint changed after credential refresh")
	}
	if err := m.Close(creds.SandboxID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(creds.SandboxID, 22); ok {
		t.Fatal("closed endpoint retained")
	}
	if c, err := net.DialTimeout("tcp", address, 100*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatal("closed listener still accepts")
	}
}

func TestTunnelPreferredPortAndPreviewMapping(t *testing.T) {
	m, f, creds := fixtureManager(t)
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	preferred := occupied.Addr().(*net.TCPAddr).Port
	if _, err := m.Start(context.Background(), creds, 5173, preferred); err == nil {
		t.Fatal("occupied preferred port silently changed")
	}
	_ = occupied.Close()
	address, err := m.Start(context.Background(), creds, 5173, preferred)
	if err != nil || address != net.JoinHostPort("127.0.0.1", strconv.Itoa(preferred)) {
		t.Fatalf("preferred port not retained: %s, %v", address, err)
	}
	if got, err := m.Start(context.Background(), creds, 5173, preferred); err != nil || got != address {
		t.Fatal("idempotent start changed endpoint")
	}
	if _, err := m.StartMapped(context.Background(), creds, 5173, 5174, preferred); err == nil {
		t.Fatal("changed mapping silently accepted")
	}
	preview, err := m.StartMapped(context.Background(), creds, 18080, 17980, 0)
	if err != nil {
		t.Fatal(err)
	}
	echoAt(t, preview, "injector ingress")
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.ports[5173] || !f.ports[17980] || f.ports[18080] {
		t.Fatalf("incorrect published target ports: %v", f.ports)
	}
}

func TestTunnelCDPDiscoveryAndWebSocketUpgrade(t *testing.T) {
	m, _, creds := fixtureManager(t)
	address, err := m.Start(context.Background(), creds, 9222, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/json/version", "/json/list", "/json", "/json/"} {
		resp, err := http.Get("http://" + address + path) // #nosec G107 -- owned loopback test endpoint.
		if err != nil {
			t.Fatal(err)
		}
		var value []map[string]string
		err = json.NewDecoder(resp.Body).Decode(&value)
		_ = resp.Body.Close()
		if err != nil || len(value) != 1 || value[0]["webSocketDebuggerUrl"] != "ws://"+address+"/devtools/page/existing?x=1" {
			t.Fatalf("unreachable CDP discovery URL: %v, %v", value, err)
		}
	}
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := "GET /devtools/browser/id HTTP/1.1\r\nHost: " + address + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
	_, _ = io.WriteString(conn, request)
	data := make([]byte, len(request))
	if _, err := io.ReadFull(conn, data); err != nil || string(data) != request {
		t.Fatalf("CDP upgrade bytes changed: %q, %v", data, err)
	}
}

func TestTunnelValidationControlAndClose(t *testing.T) {
	m, f, creds := fixtureManager(t)
	for _, origin := range []string{"", "file:///tmp/x", "http://u:p@example.com", "http://host/path", "http://host/?token=x"} {
		if _, err := relayOrigin(origin); err == nil {
			t.Fatalf("accepted invalid origin %q", origin)
		}
	}
	for _, port := range []int{0, -1, 65536, 8081, 49983} {
		if _, err := m.Start(context.Background(), creds, port, 0); err == nil {
			t.Fatalf("accepted forbidden port %d", port)
		}
	}
	bad := creds
	bad.TrafficToken = "wrong-secret-never-log"
	err := m.Control(context.Background(), bad, http.MethodGet, "/_bridge/health", nil, nil)
	if err == nil || strings.Contains(err.Error(), bad.TrafficToken) || !strings.Contains(err.Error(), "403") {
		t.Fatalf("unsafe/unhelpful auth failure: %v", err)
	}
	if err := m.Control(context.Background(), creds, http.MethodGet, "http://other/_bridge/health", nil, nil); err == nil {
		t.Fatal("control accepted external URL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Start(ctx, creds, 5174, 0); err == nil {
		t.Fatal("cancelled publication succeeded")
	}
	m.origin = f.server.URL
	creds.EndpointURL = ""
	address, err := m.Start(context.Background(), creds, 8744, 0)
	if err != nil {
		t.Fatal(err)
	}
	conn := echoAt(t, address, "active connection")
	_ = m.CloseAll()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("manager shutdown left active connection open")
	}
	if _, err := m.Start(context.Background(), creds, 22, 0); err == nil {
		t.Fatal("closed manager restarted")
	}
}

func TestTunnelControlResponseBoundaries(t *testing.T) {
	if _, err := NewTunnelManager("file:///not-an-endpoint"); err == nil {
		t.Fatal("manager accepted non-HTTP endpoint")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_bridge/redirect":
			http.Redirect(w, r, "http://outside.invalid/", http.StatusFound)
		case "/_bridge/large":
			_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", 4*1024*1024+1)))
		case "/_bridge/bad-json":
			_, _ = io.WriteString(w, "not json")
		default:
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	defer server.Close()
	m, err := NewTunnelManager(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.CloseAll() }()
	creds := RelayCredentials{SandboxID: "guest", TrafficToken: "token", RelayKey: "key"}
	if err := m.UpdateCredentials(RelayCredentials{}); err == nil {
		t.Fatal("missing private credentials accepted")
	}
	var value any
	for _, path := range []string{"/_bridge/redirect", "/_bridge/large", "/_bridge/bad-json"} {
		if err := m.Control(context.Background(), creds, http.MethodGet, path, nil, &value); err == nil {
			t.Fatalf("unsafe control response accepted: %s", path)
		}
	}
	if err := m.Control(context.Background(), creds, http.MethodPost, "/_bridge/ports", make(chan int), nil); err == nil {
		t.Fatal("invalid JSON control body accepted")
	}
	if err := m.Control(context.Background(), creds, http.MethodGet, "/_bridge/%", nil, nil); err == nil {
		t.Fatal("invalid path escape accepted")
	}
}

func TestTunnelTLSAndDiscoveryFailure(t *testing.T) {
	_, f, creds := fixtureManager(t)
	server := httptest.NewTLSServer(f.server.Config.Handler)
	defer server.Close()
	m, err := NewTunnelManager(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.CloseAll() }()
	trust := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	m.http.Transport.(*http.Transport).TLSClientConfig = trust
	m.dialer.TLSClientConfig = trust
	creds.EndpointURL = ""
	address, err := m.Start(context.Background(), creds, 22, 0)
	if err != nil {
		t.Fatal(err)
	}
	echoAt(t, address, "TLS private routing")
	cdp, err := m.Start(context.Background(), creds, 9222, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.cdpStatus = http.StatusServiceUnavailable
	f.mu.Unlock()
	resp, err := http.Get("http://" + cdp + "/json/version") // #nosec G107 -- owned loopback test endpoint.
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatal("failed CDP discovery did not fail closed")
	}
	conn, err := net.DialTimeout("tcp", cdp, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, _ = io.WriteString(conn, "not HTTP\r\n\r\n")
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("malformed CDP request accepted")
	}
	_ = conn.Close()
	creds.TrafficToken = "stale"
	if err := m.UpdateCredentials(creds); err != nil {
		t.Fatal(err)
	}
	conn, err = net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("stale E2B traffic token upgraded a new stream")
	}
	_ = conn.Close()
}

// This subprocess runs only the embedded relay on owned loopback ports. It
// verifies real stdlib WS-to-TCP behavior, private auth, dynamic allowlists and
// an idle interval longer than the original ten-second connect-timeout bug.
func TestEmbeddedGuestRelayRealTCPAndIdle(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is required for the E2B guest transport test")
	}
	dir := t.TempDir()
	relayPath := filepath.Join(dir, "relay.py")
	if err := os.WriteFile(relayPath, []byte(GuestRelaySource), 0o600); err != nil {
		t.Fatal(err)
	}
	program := `import importlib.util, json, socket, threading, sys, tempfile, http.client
spec=importlib.util.spec_from_file_location('relay',sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
assert not m.valid_port(True) and not m.valid_port(8081) and not m.valid_port(49983)
assert m.valid_port(22) and m.valid_port(5174)
v={'nested':[{'webSocketDebuggerUrl':'ws://localhost:9222/devtools/a?x=1'}]};m.rewrite_cdp(v,'127.0.0.1:12345')
assert v['nested'][0]['webSocketDebuggerUrl']=='ws://127.0.0.1:12345/devtools/a?x=1'
try:m.RelayServer(('127.0.0.1',0),sys.argv[2],{'secret':'x','ports':[49983]});raise AssertionError('management port accepted')
except ValueError:pass
echo=socket.socket();echo.bind(('127.0.0.1',0));echo.listen();port=echo.getsockname()[1]
def accept():
 while True:
  c,_=echo.accept()
  def serve(c):
   try:
    while True:
     data=c.recv(65536)
     if not data:c.sendall(b'after-eof');return
     c.sendall(data)
   finally:c.close()
  threading.Thread(target=serve,args=(c,),daemon=True).start()
threading.Thread(target=accept,daemon=True).start()
server=m.RelayServer(('127.0.0.1',0),sys.argv[2],{'secret':'relay-key','marker':'owned-test','ports':[],'workspace':sys.argv[2]})
threading.Thread(target=server.serve_forever,daemon=True).start()
def request(method,path,body=None,secret='relay-key'):
 c=http.client.HTTPConnection('127.0.0.1',server.server_port,timeout=2)
 c.request(method,path,body,{'X-Grasp-Relay-Key':secret});r=c.getresponse();data=r.read();status=r.status;c.close();return status,data
assert request('GET','/_bridge/health',secret='wrong')[0]==403
health=json.loads(request('GET','/_bridge/health')[1]);assert health['marker']=='owned-test' and health['bootstrapExit'] is None
assert health['workspace']['totalBytes']>0 and 0<=health['workspace']['availableBytes']<=health['workspace']['freeBytes']<=health['workspace']['totalBytes']
assert request('POST','/_bridge/ports',json.dumps({'ports':[8081]}))[0]==400
assert request('POST','/_bridge/ports',json.dumps({'ports':[49983]}))[0]==400
assert request('POST','/_bridge/ports',json.dumps({'ports':[True]}))[0]==400
assert request('POST','/_bridge/ports','x'*8193)[0]==400
assert request('POST','/_bridge/ports',json.dumps({'ports':[port]}))[0]==200
print(json.dumps({'relay':server.server_port,'echo':port}),flush=True)
sys.stdin.read()
server.shutdown();server.server_close();echo.close()
`
	cmd := exec.Command(python, "-u", "-c", program, relayPath, dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("guest relay subprocess: %v; %s", err, stderr.String())
		}
	})
	var ports struct{ Relay, Echo int }
	if err := json.NewDecoder(bufio.NewReader(stdout)).Decode(&ports); err != nil {
		t.Fatalf("guest relay startup: %v; %s", err, stderr.String())
	}
	m, err := NewTunnelManager("http://127.0.0.1:" + strconv.Itoa(ports.Relay))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.CloseAll() })
	creds := RelayCredentials{SandboxID: "guest", TrafficToken: "traffic", RelayKey: "relay-key"}
	address, err := m.Start(context.Background(), creds, ports.Echo, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the actual guest's WebSocket authentication/allowlist and control
	// frames, separately from the public TCP stream forwarded by the manager.
	wsURL, headers, err := m.endpoint(creds, "/_bridge/tcp?port="+strconv.Itoa(ports.Echo))
	if err != nil {
		t.Fatal(err)
	}
	wsURL.Scheme = "ws"
	wrong := headers.Clone()
	wrong.Set("X-Grasp-Relay-Key", "wrong")
	if conn, resp, err := m.dialer.Dial(wsURL.String(), wrong); err == nil {
		_ = conn.Close()
		t.Fatal("guest WebSocket accepted wrong authentication")
	} else if resp == nil || resp.StatusCode != 403 {
		t.Fatalf("guest auth status: %v, %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	deniedURL := *wsURL
	deniedURL.RawQuery = "port=49983"
	if conn, resp, err := m.dialer.Dial(deniedURL.String(), headers); err == nil {
		_ = conn.Close()
		t.Fatal("guest WebSocket exposed envd management port")
	} else if resp == nil || resp.StatusCode != 403 {
		t.Fatalf("guest denied-port status: %v, %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	ws, _, err := m.dialer.Dial(wsURL.String(), headers)
	if err != nil {
		t.Fatal(err)
	}
	pong := false
	ws.SetPongHandler(func(data string) error { pong = data == "alive"; return nil })
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := ws.WriteControl(websocket.PingMessage, []byte("alive"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte("ping barrier")); err != nil {
		t.Fatal(err)
	}
	if _, data, err := ws.ReadMessage(); err != nil || string(data) != "ping barrier" || !pong {
		t.Fatalf("guest ping/pong or binary echo failed: %q pong=%v err=%v", data, pong, err)
	}
	_ = ws.Close()
	conn := echoAt(t, address, "actual Python guest and real TCP")
	_ = conn.SetDeadline(time.Time{})
	time.Sleep(11 * time.Second)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, "after idle"); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len("after idle"))
	if _, err := io.ReadFull(conn, data); err != nil || string(data) != "after idle" {
		t.Fatalf("connect timeout leaked into persistent stream: %q, %v", data, err)
	}
	if err := conn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	remaining, err := io.ReadAll(conn)
	if err != nil || string(remaining) != "after-eof" {
		t.Fatalf("real guest half-close: %q, %v", remaining, err)
	}
}
