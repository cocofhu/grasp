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
