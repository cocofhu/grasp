package previewinject

import (
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
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

func TestDirectPreviewReservesGraspSessionCookie(t *testing.T) {
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "cf_session", Value: "login-fixture", Path: "/", HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "app_session", Value: "app-fixture", Path: "/"})
	}))
	defer login.Close()
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("cf_session"); err != http.ErrNoCookie {
			t.Error("the app received the platform login cookie")
		}
		if cookie, err := r.Cookie("app_session"); err != nil || cookie.Value != "app-fixture" {
			t.Error("the app did not receive its own cookie")
		}
		http.SetCookie(w, &http.Cookie{Name: "cf_session", Value: "overwrite-fixture", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "app_session", Value: "app-updated", Path: "/"})
		w.WriteHeader(http.StatusNoContent)
	}))
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	for _, address := range []string{login.URL, proxy.URL} {
		resp, err := client.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	loginURL, err := url.Parse(login.URL)
	if err != nil {
		t.Fatal(err)
	}
	cookies := map[string]string{}
	for _, cookie := range jar.Cookies(loginURL) {
		cookies[cookie.Name] = cookie.Value
	}
	if cookies["cf_session"] != "login-fixture" || cookies["app_session"] != "app-updated" {
		t.Fatal("the preview must update only its own cookie")
	}
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

func TestProxy_ServesPageControlScript(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("page-control path must not hit upstream")
	}))
	resp, err := http.Get(proxy.URL + PageControlPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("status=%d type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(body), "__graspPageControl") {
		t.Fatalf("script body: %s", body[:min(len(body), 80)])
	}
}

func TestProxy_ServesLiveOverlay(t *testing.T) {
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("live-overlay path must not hit upstream")
	}))
	resp, err := http.Get(proxy.URL + LiveOverlayPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("status=%d type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(body), "__graspLiveOverlay") {
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

func TestProxy_ExtendsCSPWithoutDroppingProtection(t *testing.T) {
	reportOnly := "default-src 'none'; report-uri /csp-report"
	proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Add(cspHeader, "default-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; object-src 'none'")
		w.Header().Add(cspHeader, "script-src 'self'; style-src 'unsafe-inline'; connect-src 'none', img-src 'none'")
		w.Header().Set("Content-Security-Policy-Report-Only", reportOnly)
		w.Header().Set("ETag", `"upstream"`)
		_, _ = w.Write([]byte(`<head><meta http-equiv="Content-Security-Policy" content="style-src 'self'"></head><body></body>`))
	}))
	resp, err := http.Get(proxy.URL + "/demo/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	policies := resp.Header.Values(cspHeader)
	if len(policies) != 2 ||
		!strings.Contains(policies[0], "frame-ancestors 'none'; base-uri 'none'; form-action 'none'; object-src 'none'") ||
		!strings.Contains(policies[0], "script-src 'self'") ||
		!strings.Contains(policies[0], "script-src-elem 'self' "+scriptURL) ||
		!strings.Contains(policies[1], "style-src 'unsafe-inline';connect-src "+proxy.URL+EmbedOriginPath+";") ||
		!strings.HasSuffix(policies[1], ", img-src 'none'") ||
		strings.Contains(policies[1], "style-src-elem") {
		t.Fatalf("policies: %q", policies)
	}
	if resp.Header.Get("Content-Security-Policy-Report-Only") != reportOnly ||
		resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("ETag") != "" {
		t.Fatalf("headers: %v", resp.Header)
	}
	_, nonceWithEnd, ok := strings.Cut(string(body), ` nonce="`)
	nonce, _, _ := strings.Cut(nonceWithEnd, `"`)
	if !ok || nonce == "" || !strings.Contains(string(body), "style-src-elem &#39;self&#39; &#39;nonce-"+nonce+"&#39;") ||
		!strings.Contains(policies[0], "'nonce-"+nonce+"'") {
		t.Fatalf("body: %s", body)
	}
}

func TestProxy_LeavesHTMLIntactWhenCSPProhibitsInjection(t *testing.T) {
	for _, tc := range []struct{ name, policy, meta string }{
		{"sandbox header", "script-src 'self', sandbox; default-src 'none'", "style-src 'none'"},
		{"script deny header", "default-src 'self'; script-src-elem 'none'", "style-src 'none'"},
		{"script deny meta", "default-src 'self'; connect-src 'none'", "script-src 'none'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `<head><meta http-equiv=Content-Security-Policy content="` + tc.meta + `"></head><body>upload</body>`
			proxy, _ := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set(cspHeader, tc.policy)
				_, _ = io.WriteString(w, body)
			}))
			resp, err := http.Get(proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			got, err := io.ReadAll(resp.Body)
			if err != nil || string(got) != body || resp.Header.Get(cspHeader) != tc.policy {
				t.Fatalf("protected HTML changed: %s, %v, %v", got, resp.Header, err)
			}
		})
	}
}

func TestProxy_LimitsFrameAndConnectSources(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set(cspHeader, "default-src 'none'; script-src 'self'; child-src 'self'; frame-ancestors 'none'")
		_, _ = io.WriteString(w, "<body>app</body>")
	}))
	defer up.Close()
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(NewHandlerWithEmbed(u, ScriptPath, EmbedLookup{
		RunID: "run-1", RuntimeURL: "http://internal.example/sandbox-inject/runtime",
	}))
	defer proxy.Close()
	req, err := http.NewRequest(http.MethodGet, proxy.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Host", "app.preview.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	policy := resp.Header.Get(cspHeader)
	for _, want := range []string{
		"script-src 'self'", "child-src 'self'", "frame-ancestors 'none'",
		"connect-src https://app.preview.example" + EmbedOriginPath,
		"frame-src 'self' http://*:*/embed/runs/run-1/nodes/",
	} {
		if !strings.Contains(policy, want) {
			t.Errorf("policy %q lacks %q", policy, want)
		}
	}
	if strings.Contains(policy, "internal.example") {
		t.Fatalf("unexpected source: %s", policy)
	}
}

func TestProxy_SkipsWebsocketUpgrade(t *testing.T) {
	modify := modifyResponse(scriptURL, "")
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
