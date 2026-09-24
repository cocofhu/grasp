package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestToPreviewDocumentHost(t *testing.T) {
	if got := toPreviewDocumentHost("app.example.com"); got != "pv.app.example.com" {
		t.Fatalf("dns host = %q", got)
	}
	if got := toPreviewDocumentHost("app.example.com:8443"); got != "pv.app.example.com:8443" {
		t.Fatalf("dns host with port = %q", got)
	}
	if got := toPreviewDocumentHost("pv.app.example.com"); got != "pv.app.example.com" {
		t.Fatalf("already preview = %q", got)
	}
	if got := toPreviewDocumentHost("10.0.0.8:18081"); got != "pv.10.0.0.8.sslip.io:18081" {
		t.Fatalf("ipv4 = %q", got)
	}
	if got := toPreviewDocumentHost("127.0.0.1:18081"); got != "pv.127.0.0.1.localhost:18081" {
		t.Fatalf("loopback ipv4 = %q", got)
	}
	if got := toPreviewDocumentHost("localhost:18082"); got != "pv.localhost:18082" {
		t.Fatalf("localhost = %q", got)
	}
	if got := toPreviewDocumentHost("[::1]:18081"); got != "pv.v6-0-0-0-0-0-0-0-1.localhost:18081" {
		t.Fatalf("loopback ipv6 = %q", got)
	}
	if !isPreviewDocumentHost("pv.app.example.com:8443") || isPreviewDocumentHost("app.example.com") {
		t.Fatal("preview host detection")
	}
	parent := approvalHostFromPreview("pv.app.example.com:8443")
	if parent != "app.example.com:8443" {
		t.Fatalf("parent = %q", parent)
	}
	if parent := approvalHostFromPreview("pv.10.0.0.8.sslip.io:18081"); parent != "10.0.0.8:18081" {
		t.Fatalf("ip parent = %q", parent)
	}
	if parent := approvalHostFromPreview("pv.127.0.0.1.localhost:18081"); parent != "127.0.0.1:18081" {
		t.Fatalf("loopback parent = %q", parent)
	}
	if parent := approvalHostFromPreview("pv.localhost:18082"); parent != "localhost:18082" {
		t.Fatalf("localhost parent = %q", parent)
	}
	if parent := approvalHostFromPreview("pv.v6-0-0-0-0-0-0-0-1.localhost:18081"); parent != "[::1]:18081" {
		t.Fatalf("ipv6 parent = %q", parent)
	}
}

func TestShouldPartitionSameOriginLoginOnCrossSitePreview(t *testing.T) {
	loopback := httptest.NewRequest(http.MethodPost, "http://pv.127.0.0.1.localhost:18081/preview/run-e2e/n1/9090/login", nil)
	loopback.Host = "pv.127.0.0.1.localhost:18081"
	loopback.Header.Set("Sec-Fetch-Site", "same-origin")
	if !shouldPartitionPreviewCookies(loopback, loopback.Host) {
		t.Fatal("loopback preview must partition a same-origin login post")
	}
	local := httptest.NewRequest(http.MethodPost, "http://pv.localhost:18082/preview/run-e2e/n1/9090/login", nil)
	local.Host = "pv.localhost:18082"
	local.Header.Set("Sec-Fetch-Site", "same-origin")
	if !shouldPartitionPreviewCookies(local, local.Host) {
		t.Fatal("localhost preview must partition a same-origin login post")
	}
	sameSite := httptest.NewRequest(http.MethodPost, "http://pv.app.example.com/login", nil)
	sameSite.Host = "pv.app.example.com"
	sameSite.Header.Set("Sec-Fetch-Site", "same-origin")
	if shouldPartitionPreviewCookies(sameSite, sameSite.Host) {
		t.Fatal("same-site DNS preview must keep Lax cookies")
	}
	plainIP := httptest.NewRequest(http.MethodPost, "http://pv.10.0.0.8.sslip.io:18081/login", nil)
	plainIP.Host = "pv.10.0.0.8.sslip.io:18081"
	plainIP.Header.Set("Sec-Fetch-Site", "same-origin")
	if shouldPartitionPreviewCookies(plainIP, plainIP.Host) {
		t.Fatal("http sslip.io is not trustworthy; do not add Secure")
	}
	plainIP.Header.Set("X-Forwarded-Proto", "https")
	if !shouldPartitionPreviewCookies(plainIP, plainIP.Host) {
		t.Fatal("https sslip.io must partition even when the form post is same-origin")
	}

	const prefix = "/preview/run-e2e/n1/9090/"
	raw := rewritePreviewSetCookie("shop_session=abc; Path=/; HttpOnly; SameSite=Lax", prefix, true)
	for _, want := range []string{
		"pv.shop_session=abc",
		"Path=/preview/run-e2e/n1/9090/",
		"HttpOnly",
		"SameSite=None",
		"Secure",
		"Partitioned",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("cookie %q missing %q", raw, want)
		}
	}
	if strings.Contains(raw, "SameSite=Lax") {
		t.Fatalf("Lax must be replaced: %q", raw)
	}
	other := rewritePreviewSetCookie("remember_me=yes; Path=/account", prefix, true)
	if !strings.Contains(other, "pv.remember_me=yes") || !strings.Contains(other, "Partitioned") || !strings.Contains(other, "Path=/preview/run-e2e/n1/9090/account") {
		t.Fatalf("second cookie = %q", other)
	}
}

func TestForwardPreviewCookiesDropsPlatformSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "cf_session=platform; pv.shop_session=abc; pv.remember_me=yes; other=no")
	forwardPreviewCookies(req)
	got := req.Header.Get("Cookie")
	if strings.Contains(got, "cf_session") || strings.Contains(got, "other=") {
		t.Fatalf("platform cookies leaked: %q", got)
	}
	if got != "shop_session=abc; remember_me=yes" {
		t.Fatalf("cookie = %q", got)
	}

	empty := httptest.NewRequest(http.MethodGet, "/", nil)
	empty.Header.Set("Cookie", "cf_session=only")
	forwardPreviewCookies(empty)
	if empty.Header.Get("Cookie") != "" {
		t.Fatalf("expected no cookie, got %q", empty.Header.Get("Cookie"))
	}
}

func TestPublicGateCSPAllowsPreviewHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	csp := PublicGateCSP("app.example.com")
	if !strings.Contains(csp, "http://pv.app.example.com") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("csp = %q", csp)
	}
}
