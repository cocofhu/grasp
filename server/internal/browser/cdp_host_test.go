package browser

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPreferIPAddrResolvesClusterDNS(t *testing.T) {
	prev := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = prev })
	lookupIPAddr = func(_ context.Context, host string) ([]net.IP, error) {
		if host != "sbx-m1.sandbox-gateway.svc.cluster.local" {
			t.Fatalf("lookup host=%q", host)
		}
		return []net.IP{net.ParseIP("10.43.9.9"), net.ParseIP("2001:db8::9")}, nil
	}
	got := preferIPAddr(context.Background(), "sbx-m1.sandbox-gateway.svc.cluster.local:9222")
	if got != "10.43.9.9:9222" {
		t.Fatalf("got %q want 10.43.9.9:9222", got)
	}
}

func TestPreferIPAddrLeavesIPUnchanged(t *testing.T) {
	prev := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = prev })
	lookupIPAddr = func(context.Context, string) ([]net.IP, error) {
		t.Fatal("must not lookup bare IP")
		return nil, nil
	}
	if got := preferIPAddr(context.Background(), "10.88.0.12:9222"); got != "10.88.0.12:9222" {
		t.Fatalf("got %q", got)
	}
}

func TestAttachSandboxDialsResolvedIP(t *testing.T) {
	prev := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = prev })
	lookupIPAddr = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.43.1.2")}, nil
	}
	s, _ := newResolverService(map[string]string{
		"cdp":   "sbx-m1.sandbox-gateway.svc.cluster.local:9222",
		"novnc": "sbx-m1.sandbox-gateway.svc.cluster.local:6080",
	})
	var dialed string
	s.dial = func(_ context.Context, httpBase string) (Engine, error) {
		dialed = httpBase
		return &fakeEngine{name: "sandbox"}, nil
	}
	// EnsureSandboxVNC ignores readyProbe when a resolver is present; exercise
	// attach directly so we only assert the CDP dial host rewrite.
	s.mu.Lock()
	_, err := s.attachSandboxLocked(context.Background(), "sb", "203.0.113.9",
		"sbx-m1.sandbox-gateway.svc.cluster.local:9222",
		"sbx-m1.sandbox-gateway.svc.cluster.local:6080")
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if dialed != "http://10.43.1.2:9222" {
		t.Fatalf("dialed %q want http://10.43.1.2:9222", dialed)
	}
}

// Chromium requires an IP or localhost Host even when the gateway is dialed
// by service DNS. Discovery may return a loopback or an internal address.
func hiddenTargetProbeMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackCDPHost(r.Host) {
			t.Errorf("discovery Host = %q", r.Host)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": "ws://127.0.0.1:1/devtools/browser/test"})
	})
	mux.HandleFunc("/devtools/browser/test", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackCDPHost(r.Host) {
			t.Errorf("WebSocket Host = %q", r.Host)
		}
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var cmd struct {
			ID     int
			Method string
			Params struct {
				URL                string
				Background, Hidden bool
			}
		}
		if err := conn.ReadJSON(&cmd); err != nil || cmd.Method != "Target.createTarget" ||
			cmd.Params.URL != "about:blank" || !cmd.Params.Hidden || !cmd.Params.Background {
			t.Errorf("unexpected readiness command: %+v, %v", cmd, err)
			return
		}
		_ = conn.WriteJSON(map[string]any{"id": cmd.ID, "result": map[string]string{"targetId": "probe"}})
	})
	return mux
}

func TestProbeTabCreateUsesLoopbackHostHeader(t *testing.T) {
	srv := httptest.NewServer(hiddenTargetProbeMux(t))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	s, _, _ := newFakeService(Config{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if !s.probeTabCreate(ctx, "localhost", port) {
		t.Fatal("background target probe failed")
	}
}

func TestProbeTabCreateRejectsDNSHostWithoutRewrite(t *testing.T) {
	// Document the Chrome failure mode we are guarding against: if Host stays
	// as Service DNS, /json/version returns 500 and readiness fails.
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackCDPHost(r.Host) {
			http.Error(w, "Host header is specified and is not an IP address or localhost.", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "x"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/json/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "sbx-x.sandbox-gateway.svc.cluster.local:9222"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500 for DNS Host", resp.StatusCode)
	}

	// With rewrite, same server accepts.
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/json/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	setCDPRequestHost(req2, port)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200 after Host rewrite", resp2.StatusCode)
	}
}

func isLoopbackCDPHost(hostport string) bool {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		h = hostport
	}
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}
