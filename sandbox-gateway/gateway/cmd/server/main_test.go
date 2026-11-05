package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sandbox-gateway/internal/config"
)

func healthTestConfig(server *httptest.Server) *config.Config {
	cfg := config.Default()
	cfg.Driver = "e2b"
	cfg.Server.Listen = strings.TrimPrefix(server.URL, "http://")
	return cfg
}

func TestHealthcheckRequiresStatusAndConfiguredDriver(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		ok     bool
	}{
		"ready":            {200, `{"status":"ok","driver":"e2b"}`, true},
		"not ready":        {503, `{"status":"ok","driver":"e2b"}`, false},
		"wrong status":     {200, `{"status":"starting","driver":"e2b"}`, false},
		"driver mismatch":  {200, `{"status":"ok","driver":"docker"}`, false},
		"missing driver":   {200, `{"status":"ok"}`, false},
		"malformed":        {200, `synthetic-secret-untrusted-body`, false},
		"multiple objects": {200, `{"status":"ok","driver":"e2b"}{}`, false},
		"oversized":        {200, strings.Repeat(" ", 4097), false},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/healthz" || r.Header.Get("Authorization") != "" {
					t.Error("healthcheck sent unexpected path, method or credentials")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			err := checkGatewayHealth(t.Context(), healthTestConfig(server))
			if (err == nil) != tc.ok {
				t.Fatalf("healthcheck success=%v expected=%v", err == nil, tc.ok)
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("healthcheck echoed the response body")
			}
		})
	}
}

func TestHealthcheckMapsWildcardIPv4ToLoopbackAndIgnoresHTTPProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok","driver":"e2b"}`)
	}))
	defer server.Close()
	proxyUsed := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyUsed = true }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	for _, host := range []string{"", "0.0.0.0", "127.0.0.1"} {
		cfg := healthTestConfig(server)
		cfg.Server.Listen = net.JoinHostPort(host, port)
		if err := checkGatewayHealth(t.Context(), cfg); err != nil {
			t.Fatalf("wildcard bind did not target loopback: %v", err)
		}
	}
	if proxyUsed {
		t.Fatal("local healthcheck followed an environment HTTP proxy")
	}
}

func TestHealthcheckMapsWildcardIPv6ToLoopback(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("host does not support IPv6 loopback")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok","driver":"e2b"}`)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	cfg := healthTestConfig(server)
	cfg.Server.Listen = net.JoinHostPort("::", port)
	if err := checkGatewayHealth(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestHealthcheckDoesNotFollowRedirects(t *testing.T) {
	followed := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()
	if checkGatewayHealth(t.Context(), healthTestConfig(server)) == nil || followed {
		t.Fatal("healthcheck accepted or followed a redirect")
	}
}

func TestHealthcheckReadIsBoundedByContextAndThreeSecondLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := checkGatewayHealth(ctx, healthTestConfig(server)); err == nil || time.Since(start) > time.Second {
		t.Fatal("healthcheck did not honor its caller's shorter deadline")
	}
	start = time.Now()
	if err := checkGatewayHealth(t.Context(), healthTestConfig(server)); err == nil {
		t.Fatal("healthcheck accepted a permanently blocked response body")
	}
	if duration := time.Since(start); duration < 2*time.Second || duration > 5*time.Second {
		t.Fatalf("healthcheck did not use its three-second bound: %s", duration)
	}
}

func TestHealthcheckRejectsInvalidListenersAndClosedPort(t *testing.T) {
	for _, address := range []string{"", "http://localhost:8080", "localhost", ":0", ":65536", ":named-service"} {
		cfg := config.Default()
		cfg.Server.Listen = address
		if checkGatewayHealth(t.Context(), cfg) == nil {
			t.Fatal("invalid listener passed healthcheck")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	cfg := config.Default()
	cfg.Server.Listen = address
	if checkGatewayHealth(t.Context(), cfg) == nil {
		t.Fatal("closed listener passed healthcheck")
	}
}

func TestHealthcheckMainExitsWithoutConstructingE2BDriver(t *testing.T) {
	if os.Getenv("GRASP_HEALTHCHECK_TEST_HELPER") == "1" {
		os.Args = []string{"sandbox-gateway", "-config", os.Getenv("GRASP_HEALTHCHECK_TEST_CONFIG"), "-healthcheck"}
		main()
		os.Exit(0)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok","driver":"e2b"}`)
	}))
	defer server.Close()
	cfg := healthTestConfig(server)
	cfg.Image.Ref = "approved-image"
	cfg.E2B.APIKey = "synthetic-provider-key"
	cfg.E2B.OwnerID = "test-owner"
	// This is syntactically valid configuration, but initializing a driver's
	// recovery directory here always fails, including when tests run as root.
	file := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(file, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.E2B.StateDir = filepath.Join(file, "cannot-create-state")
	cfg.E2B.Templates = []config.E2BTemplateConfig{{Image: "approved-image", TemplateID: "prepared-template", CPUCores: 2, MemoryMB: 3072, DiskGi: 20}}
	// Config uses YAML tags, so build the public YAML/JSON names explicitly.
	configJSON := fmt.Sprintf(`{"driver":"e2b","server":{"listen":%q},"image":{"ref":"approved-image"},"e2b":{"apiKey":"synthetic-provider-key","ownerID":"test-owner","stateDir":%q,"templates":[{"image":"approved-image","templateID":"prepared-template","cpuCores":2,"memoryMB":3072,"diskGi":20}]}}`, cfg.Server.Listen, cfg.E2B.StateDir)
	filename := filepath.Join(t.TempDir(), "healthcheck.json")
	if err := os.WriteFile(filename, []byte(configJSON), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHealthcheckMainExitsWithoutConstructingE2BDriver$")
	cmd.Env = append(os.Environ(), "GRASP_HEALTHCHECK_TEST_HELPER=1", "GRASP_HEALTHCHECK_TEST_CONFIG="+filename)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("healthcheck initialized a driver or failed local readiness: %v; output=%s", err, output)
	}
}
