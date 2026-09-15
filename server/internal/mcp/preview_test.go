package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSetPreviewAllowed(t *testing.T) {
	if !SetPreviewAllowed("app_preview") || !SetPreviewAllowed("approve") || !SetPreviewAllowed("grasp") {
		t.Fatal("expected app_preview, grasp, and approve alias")
	}
	if SetPreviewAllowed("implement") || SetPreviewAllowed("react") || SetPreviewAllowed("") {
		t.Fatal("other node types must not get set_preview")
	}
}

func TestSetPreviewGateAndUpsert(t *testing.T) {
	h := NewHost(&memStore{})
	h.SetPreviewBaseURL("http://app.example.com")
	h.SetPreviewSandboxOps(&fakePreviewOps{name: "sb", ok: true, healthy: true, up: "http://10.0.0.1:5173"})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "preview1", "app_preview")

	resp := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":5173,"label":"前端"}}}`)
	text := previewResultText(t, resp)
	if text == "" || previewTextIsError(text) {
		t.Fatalf("set_preview failed: %s", text)
	}
	if !h.HasPreviewPorts("r1", "preview1") {
		t.Fatal("expected preview port registered")
	}
	ports := h.ListPreviewPorts("r1", "preview1")
	if len(ports) != 1 || ports[0].Port != 5173 || ports[0].Label != "前端" {
		t.Fatalf("unexpected ports: %+v", ports)
	}
	if !ports[0].Healthy || ports[0].KeepalivePID != 4242 {
		t.Fatalf("want healthy+keepalive pid, got %+v", ports[0])
	}
	wantURL := "/preview/r1/preview1/5173/"
	if ports[0].ProxyURL != wantURL {
		t.Fatalf("proxy url = %q, want %q", ports[0].ProxyURL, wantURL)
	}
	if strings.Contains(ports[0].ProxyURL, "://") {
		t.Fatalf("proxy url must be relative, got %q", ports[0].ProxyURL)
	}

	resp2 := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":5173,"label":"Web"}}}`)
	if previewTextIsError(previewResultText(t, resp2)) {
		t.Fatal("upsert failed")
	}
	ports = h.ListPreviewPorts("r1", "preview1")
	if len(ports) != 1 || ports[0].Label != "Web" {
		t.Fatalf("upsert label: %+v", ports)
	}

	h.SetActiveNode("r1", "preview1", "implement")
	bad := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":8080}}}`)
	if !previewResultIsError(bad) {
		t.Fatal("expected set_preview rejected on implement node")
	}
}

func TestSetPreviewAllowedOnApprove(t *testing.T) {
	h := NewHost(&memStore{})
	h.SetPreviewSandboxOps(&fakePreviewOps{name: "sb", ok: true, healthy: true, up: "http://10.0.0.1:5006"})
	tok := h.RegisterRun("r-approve")
	h.SetActiveNode("r-approve", "predev", "approve")

	resp := callPreviewTool(t, h, "r-approve", tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":5006,"label":"Demo"}}}`)
	text := previewResultText(t, resp)
	if text == "" || previewTextIsError(text) {
		t.Fatalf("set_preview on approve failed: %s", text)
	}
	if !h.HasHealthyPreviewPorts("r-approve", "predev") {
		t.Fatal("expected preview port registered on approve node")
	}
}

func TestSetPreviewURLGateAndUpsert(t *testing.T) {
	h := NewHost(&memStore{})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "preview1", "app_preview")

	resp := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"set_preview","arguments":{"url":"https://staging.example.com:8443/app","label":"Staging"}}}`)
	text := previewResultText(t, resp)
	if text == "" || previewTextIsError(text) {
		t.Fatalf("set_preview url failed: %s", text)
	}
	if !h.HasPreviewPorts("r1", "preview1") {
		t.Fatal("expected url preview registered")
	}
	ports := h.ListPreviewPorts("r1", "preview1")
	if len(ports) != 1 || ports[0].Kind != PreviewKindURL || ports[0].URL != "https://staging.example.com:8443/app" {
		t.Fatalf("unexpected url ports: %+v", ports)
	}
	if !ports[0].Healthy || ports[0].Port != 0 {
		t.Fatalf("url preview must be healthy with port=0: %+v", ports[0])
	}

	resp2 := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"set_preview","arguments":{"url":"https://staging.example.com:8443/app","label":"STG"}}}`)
	if previewTextIsError(previewResultText(t, resp2)) {
		t.Fatal("url upsert failed")
	}
	ports = h.ListPreviewPorts("r1", "preview1")
	if len(ports) != 1 || ports[0].Label != "STG" {
		t.Fatalf("url upsert label: %+v", ports)
	}
}

func TestSetPreviewMutualExclusive(t *testing.T) {
	h := NewHost(&memStore{})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "preview1", "app_preview")

	both := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":8080,"url":"https://x.example/"}}}`)
	if !previewResultIsError(both) {
		t.Fatal("expected both port+url to fail")
	}
	neither := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"set_preview","arguments":{"label":"x"}}}`)
	if !previewResultIsError(neither) {
		t.Fatal("expected neither port nor url to fail")
	}
}

func TestValidatePreviewURL(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want string
	}{
		{"https://staging.example.com:8443/path", true, "https://staging.example.com:8443/path"},
		{"http://127.0.0.1:3000/", true, "http://127.0.0.1:3000/"},
		{"javascript:alert(1)", false, ""},
		{"/relative", false, ""},
		{"ftp://x.example/", false, ""},
	}
	for _, tc := range cases {
		got, err := ValidatePreviewURL(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("ValidatePreviewURL(%q) = %q, %v want %q", tc.in, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("ValidatePreviewURL(%q) want error", tc.in)
		}
	}
}

func TestSetPreviewUnreachableFails(t *testing.T) {
	h := NewHost(&memStore{})
	h.SetPreviewSandboxOps(&fakePreviewOps{name: "sb", ok: true, healthy: false, up: "http://10.0.0.1:9"})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "preview1", "app_preview")
	resp := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":9}}}`)
	if !previewResultIsError(resp) {
		t.Fatal("expected unreachable set_preview to fail")
	}
	if h.HasHealthyPreviewPorts("r1", "preview1") {
		t.Fatal("unreachable port must not count as healthy preview")
	}
}

func callPreviewTool(t *testing.T, h *Host, runID, tok, body string) map[string]any {
	t.Helper()
	_, resp := h.ServeRPC(runID, tok, []byte(body))
	var out map[string]any
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func previewResultText(t *testing.T, resp map[string]any) string {
	t.Helper()
	r, _ := resp["result"].(map[string]any)
	if r == nil {
		return ""
	}
	content, _ := r["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	m, _ := content[0].(map[string]any)
	return m["text"].(string)
}

func previewResultIsError(resp map[string]any) bool {
	r, _ := resp["result"].(map[string]any)
	return r != nil && r["isError"] == true
}

func previewTextIsError(s string) bool {
	return len(s) >= 5 && (s[:5] == "error" || len(s) >= 18 && s[:18] == "set_preview failed")
}
