package previewinject

import (
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testProxy(t *testing.T, upstream http.Handler) (*httptest.Server, *url.URL) {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(NewHandler(u, scriptURL))
	t.Cleanup(proxy.Close)
	return proxy, u
}

func TestProxy_InjectsHTML(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body>app</body></html>"))
	}))
	resp, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `<script src="`+scriptURL+`"></script>`) {
		t.Fatalf("missing script: %s", body)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("encoding leftover: %s", resp.Header.Get("Content-Encoding"))
	}
}

func TestProxy_IdempotentUpstreamScript(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><script src="` + ScriptPath + `"></script></body></html>`))
	}))
	resp, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if n := strings.Count(string(body), "preview-pick.js"); n != 1 {
		t.Fatalf("count=%d body=%s", n, body)
	}
}

func TestProxy_InjectsSameOriginBesideLocalhostTag(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><script src="http://localhost:8080/preview-pick.js"></script></body></html>`))
	}))
	t.Cleanup(up.Close)
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(NewHandler(u, ""))
	t.Cleanup(proxy.Close)
	resp, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `src="`+ScriptPath+`"`) {
		t.Fatalf("must inject same-origin beside localhost tag: %s", body)
	}
}

func TestProxy_ServesSameOriginScript(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("script path must not hit upstream")
	}))
	resp, err := http.Get(proxy.URL + ScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "grasp-embed:pick") {
		t.Fatalf("script body: %s", body[:min(len(body), 80)])
	}
}

func TestProxy_SkipsJSON(t *testing.T) {
	raw := `{"ok":true,"html":"<body></body>"}`
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(raw))
	}))
	resp, err := http.Get(proxy.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != raw {
		t.Fatalf("json rewritten: %s", body)
	}
}

func TestProxy_GzipHTML(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("<html><body>gz</body></html>"))
	_ = zw.Close()

	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	resp, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "preview-pick.js") || !strings.Contains(string(body), "gz") {
		t.Fatalf("gzip not decoded/injected: %s", body)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("gzip leftover: %q", resp.Header.Get("Content-Encoding"))
	}
}

func TestProxy_StripsAcceptEncoding(t *testing.T) {
	var seen string
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<body></body>"))
	}))
	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if seen != "identity" {
		t.Fatalf("Accept-Encoding=%q want identity", seen)
	}
}

func TestProxy_PreservesHost(t *testing.T) {
	var seen string
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Host
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<body></body>"))
	}))
	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/", nil)
	req.Host = "203.0.113.9:18080"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if seen != "203.0.113.9:18080" {
		t.Fatalf("Host=%q (Vite needs the public host, not 127.0.0.1)", seen)
	}
}

func TestProxy_LeavesLocation(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Fatalf("Location rewritten: %q", loc)
	}
}

func TestProxy_RelaxesCSP(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'")
		_, _ = w.Write([]byte("<body></body>"))
	}))
	resp, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "http://app.example") {
		t.Fatalf("csp: %s", csp)
	}
}

func TestProxy_SkipsWebsocketUpgrade(t *testing.T) {
	modify := modifyResponse(scriptURL, ScriptOrigin(scriptURL))
	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader("<body></body>")),
	}
	if err := modify(resp); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "preview-pick.js") {
		t.Fatalf("101 must not rewrite: %s", body)
	}
}

func TestProxy_SkipsBrotli(t *testing.T) {
	raw := []byte("not-really-br-but-must-pass-through")
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "br")
		_, _ = w.Write(raw)
	}))
	resp, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, raw) {
		t.Fatalf("br body mutated: %q", body)
	}
	if resp.Header.Get("Content-Encoding") != "br" {
		t.Fatalf("br encoding stripped: %q", resp.Header.Get("Content-Encoding"))
	}
}

func TestProxy_UpstreamDownClosesWithoutHTTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	upAddr := ln.Addr().String()
	_ = ln.Close()

	u, err := url.Parse("http://" + upAddr)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(NewHandler(u, scriptURL))
	defer proxy.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	_, err = client.Get(proxy.URL + "/")
	if err == nil {
		t.Fatal("expected transport error so ProbeHTTPPort stays false")
	}
}
