package handlers

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func htmlResponse(body string) *http.Response {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	resp.Header.Set("Content-Type", "text/html; charset=utf-8")
	return resp
}

func TestSandboxPreviewNavigateURLUsesMCPAdvertise(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })

	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "https://api.example.com"}})
	got := sandboxPreviewNavigateURL("localhost:8080", "run/1", "node a", 8080)
	want := "https://api.example.com/preview/run%2F1/node%20a/8080/"
	if got != want {
		t.Fatalf("k8s advertise: got %s", got)
	}

	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://host.docker.internal:8080"}})
	got = sandboxPreviewNavigateURL("ignored:1", "r", "n", 9)
	want = "http://host.docker.internal:8080/preview/r/n/9/"
	if got != want {
		t.Fatalf("docker advertise: got %s", got)
	}

	config.StoreConfig(nil)
	got = sandboxPreviewNavigateURL("127.0.0.1:8080", "r", "n", 9)
	want = "http://host.docker.internal:8080/preview/r/n/9/"
	if got != want {
		t.Fatalf("loopback fallback: got %s", got)
	}
}

func TestPreviewModifyResponse_RewritesHTML(t *testing.T) {
	const prefix = "/preview/run-1/node_a/9090/"
	body := `<!doctype html><html><head><meta charset="utf-8"></head><body>` +
		`<script src="/assets/app.js"></script>` +
		`<link href="/style.css" rel="stylesheet">` +
		`<a href="/foo">x</a>` +
		`<img src="rel.png">` +
		`<img src="//cdn.example.com/a.png">` +
		`<a href="https://ex.com/y">e</a>` +
		`</body></html>`

	resp := htmlResponse(body)
	if err := previewModifyResponse(prefix)(resp); err != nil {
		t.Fatalf("modify: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	got := string(out)

	// <base> injected right after <head>, exactly once.
	if !strings.Contains(got, `<head><base href="`+prefix+`">`) {
		t.Fatalf("base tag not injected after <head>: %s", got)
	}
	if strings.Count(got, "<base ") != 1 {
		t.Fatalf("expected exactly one <base>, got %d: %s", strings.Count(got, "<base "), got)
	}

	// Root-absolute attributes re-anchored under the prefix.
	if !strings.Contains(got, `<script src="`+prefix+`assets/app.js">`) {
		t.Fatalf("script src not rewritten: %s", got)
	}
	if !strings.Contains(got, `<link href="`+prefix+`style.css"`) {
		t.Fatalf("link href not rewritten: %s", got)
	}
	if !strings.Contains(got, `<a href="`+prefix+`foo">`) {
		t.Fatalf("anchor href not rewritten: %s", got)
	}

	// Relative, protocol-relative and absolute URLs are left untouched.
	if !strings.Contains(got, `<img src="rel.png">`) {
		t.Fatalf("relative src should be untouched: %s", got)
	}
	if !strings.Contains(got, `<img src="//cdn.example.com/a.png">`) {
		t.Fatalf("protocol-relative src should be untouched: %s", got)
	}
	if !strings.Contains(got, `<a href="https://ex.com/y">`) {
		t.Fatalf("absolute href should be untouched: %s", got)
	}

	if strings.Count(got, `<script src="/preview-pick.js"></script>`) != 1 {
		t.Fatalf("pick script not injected once: %s", got)
	}
	if strings.Contains(got, prefix+"preview-pick.js") {
		t.Fatalf("pick script src was rewritten under the prefix: %s", got)
	}

	// Content-Length reflects the rewritten body.
	if resp.Header.Get("Content-Length") == "" {
		t.Fatalf("Content-Length not set")
	}
}

func TestPreviewModifyResponse_RewritesLocation(t *testing.T) {
	const prefix = "/preview/run-1/node_a/9090/"

	resp := &http.Response{StatusCode: http.StatusFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	resp.Header.Set("Location", "/dashboard")
	if err := previewModifyResponse(prefix)(resp); err != nil {
		t.Fatalf("modify: %v", err)
	}
	if got := resp.Header.Get("Location"); got != prefix+"dashboard" {
		t.Fatalf("root-absolute Location not rewritten: %q", got)
	}

	resp2 := &http.Response{StatusCode: http.StatusFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	resp2.Header.Set("Location", "https://example.com/x")
	if err := previewModifyResponse(prefix)(resp2); err != nil {
		t.Fatalf("modify: %v", err)
	}
	if got := resp2.Header.Get("Location"); got != "https://example.com/x" {
		t.Fatalf("absolute Location should be untouched: %q", got)
	}
}

func TestPreviewModifyResponse_SkipsNonHTMLAndUpgrade(t *testing.T) {
	const prefix = "/preview/run-1/node_a/9090/"

	// Non-HTML body must be passed through byte-for-byte.
	js := `import "/assets/x.js"; fetch("/api")`
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(js))}
	resp.Header.Set("Content-Type", "application/javascript")
	if err := previewModifyResponse(prefix)(resp); err != nil {
		t.Fatalf("modify: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	if string(out) != js {
		t.Fatalf("JS body must be untouched, got: %s", out)
	}

	// Protocol switch (WebSocket upgrade) must be a no-op.
	up := &http.Response{StatusCode: http.StatusSwitchingProtocols, Header: http.Header{}}
	up.Header.Set("Content-Type", "text/html")
	if err := previewModifyResponse(prefix)(up); err != nil {
		t.Fatalf("modify upgrade: %v", err)
	}
}

func TestIsRootAbsolute(t *testing.T) {
	cases := map[string]bool{
		"/foo":             true,
		"/":                true,
		"//cdn/x":          false,
		"http://ex.com":    false,
		"https://ex.com/y": false,
		"relative/path":    false,
		"":                 false,
	}
	for in, want := range cases {
		if got := isRootAbsolute(in); got != want {
			t.Fatalf("isRootAbsolute(%q)=%v want %v", in, got, want)
		}
	}
}
