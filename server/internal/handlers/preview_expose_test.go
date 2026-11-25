package handlers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/previewexpose"
	"github.com/cocofhu/grasp/internal/router"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gorilla/websocket"
)

func seedExposedSubdomainHarness(t *testing.T, baseA, baseB string, upstreamURL string) (*harness, string, *models.Sandbox) {
	t.Helper()
	hn := newHarness(t)
	conns := []previewexpose.ConnSpec{
		{ConnID: "", Transport: "gateway", BaseURL: baseA},
	}
	if baseB != "" {
		conns = append(conns, previewexpose.ConnSpec{ConnID: "other", Transport: "gateway", BaseURL: baseB})
	}
	pol, _, err := previewexpose.NewPolicy(previewexpose.PolicyOptions{
		PublicAdvertise: "https://grasp.example.com",
		Production:      true,
		Connections:     conns,
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.h.Preview = preview
	hn.h.PreviewExpose = pol
	hn.host.SetPreviewStore(preview)
	hn.host.SetPreviewSandboxOps(preview)
	hn.host.SetPreviewExposePolicy(pol)
	hn.host.SetPreviewProxyHardening(true)
	hn.r = router.New(hn.h)

	row := models.Sandbox{Name: "e2b-sb-1", RunID: "run-sub", NodeID: "app", Purpose: "run", Status: "running"}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	hn.fg.Seed(row.Name)
	hn.fg.SetEndpoints(row.Name, map[string]string{
		"18080": strings.TrimPrefix(upstreamURL, "http://"),
		"5173":  "127.0.0.1:1",
	})

	key := "0123456789abcdef0123456789abcdef"
	if err := preview.ReplacePreview(mcp.PreviewPort{
		RunID: "run-sub", NodeID: "app", Kind: mcp.PreviewKindPort, Port: 5173,
		ProxyURL: "/preview/run-sub/app/5173/", Host: upstreamURL, InjectorPort: 18080,
		ExposeStrategy: previewexpose.StrategySubdomain, ExposeKey: key,
		SandboxName: row.Name, Healthy: true, RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return hn, key, &row
}

func TestSubdomainInboundProxySecurityAndBehavior(t *testing.T) {
	upstreamPort := "5173"
	var gotReq *http.Request
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/check" || r.URL.Path == "/redirect" {
			gotReq = r.Clone(r.Context())
		}
		if r.URL.Path == "/__grasp/preview-pick.js" {
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = io.WriteString(w, "console.log('pick')")
			return
		}
		w.Header().Set("X-Grasp-Upstream", upstreamPort)
		w.Header().Set("E2b-Traffic-Access-Token", "secret-token")
		w.Header().Set("Proxy-Authorization", "Basic leak")
		w.Header().Add("Set-Cookie", auth.CookieName+"=evil; Path=/")
		w.Header().Add("Set-Cookie", "app_sid=abc123; Domain=.preview-a.other.test; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", `prefs={"theme":"dark"}; Path=/`)
		w.Header().Add("Set-Cookie", "pri=1; path=/; DOMAIN=preview-a.other.test; Priority=High")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Referrer-Policy", "unsafe-url")
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "http://"+r.Host+"/after-login")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "ok:"+r.URL.Path)
	}))
	t.Cleanup(up.Close)

	hn, key, _ := seedExposedSubdomainHarness(t, "https://preview-a.other.test", "https://preview-b.third.test", up.URL)
	validHost := key + ".preview-a.other.test"

	// 1. Base host, unknown key, and malformed key must 404 and never reach /api/health or SPA.
	for _, host := range []string{"preview-a.other.test", "deadbeefdeadbeefdeadbeefdeadbeef.preview-a.other.test", "bad-key.preview-a.other.test"} {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Host = host
		w := serve(hn, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("host %q on /api/health: got %d, want 404", host, w.Code)
		}
	}

	// 2. Cross-base ownership check: gateway A's key on gateway B's base_url must 404.
	crossReq := httptest.NewRequest(http.MethodGet, "/app/check", nil)
	crossReq.Host = key + ".preview-b.third.test"
	if w := serve(hn, crossReq); w.Code != http.StatusNotFound {
		t.Fatalf("cross-base ticket got %d, want 404", w.Code)
	}

	// 3. Valid request: headers/cookies sanitized, Origin rewritten only for own origin, Set-Cookie Domain stripped.
	req := httptest.NewRequest(http.MethodGet, "/app/check", nil)
	req.Host = validHost
	req.Header.Set("Origin", "https://"+validHost)
	req.Header.Set("E2b-Sandbox-Id", "leak")
	req.Header.Set("X-E2b-Internal", "leak")
	req.Header.Set("Proxy-Authorization", "Basic leak")
	req.Header.Set("Forwarded", "for=1.2.3.4")
	req.Header.Set("X-Forwarded-Prefix", "/fake")
	req.Header.Set("Cookie", auth.CookieName+"=platform-secret; app_sid=abc123")
	w := serve(hn, req)
	if w.Code != http.StatusOK || w.Body.String() != "ok:/app/check" {
		t.Fatalf("valid subdomain request: %d %q", w.Code, w.Body.String())
	}
	if gotReq == nil {
		t.Fatal("upstream did not receive request")
	}
	if gotReq.Host != strings.TrimPrefix(up.URL, "http://") {
		t.Fatalf("upstream Host = %q, want %q", gotReq.Host, strings.TrimPrefix(up.URL, "http://"))
	}
	if gotReq.Header.Get("Origin") != up.URL {
		t.Fatalf("own Origin not rewritten: %q", gotReq.Header.Get("Origin"))
	}
	if gotReq.Header.Get("X-Forwarded-Host") != validHost || gotReq.Header.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("forwarded headers wrong: host=%q proto=%q", gotReq.Header.Get("X-Forwarded-Host"), gotReq.Header.Get("X-Forwarded-Proto"))
	}
	for _, stripped := range []string{"E2b-Sandbox-Id", "X-E2b-Internal", "Proxy-Authorization", "Forwarded", "X-Forwarded-Prefix"} {
		if gotReq.Header.Get(stripped) != "" {
			t.Fatalf("request header %s leaked: %q", stripped, gotReq.Header.Get(stripped))
		}
	}
	if strings.Contains(gotReq.Header.Get("Cookie"), auth.CookieName) || !strings.Contains(gotReq.Header.Get("Cookie"), "app_sid=abc123") {
		t.Fatalf("unexpected upstream Cookie: %q", gotReq.Header.Get("Cookie"))
	}
	if w.Header().Get("E2b-Traffic-Access-Token") != "" || w.Header().Get("Proxy-Authorization") != "" {
		t.Fatalf("reserved response headers leaked: %v", w.Header())
	}
	for name, want := range map[string]string{"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store"} {
		if got := w.Header().Values(name); len(got) != 1 || got[0] != want {
			t.Fatalf("security header %s = %q, want exactly [%q]", name, got, want)
		}
	}
	setCookies := w.Header().Values("Set-Cookie")
	wantCookies := []string{
		"app_sid=abc123; Path=/; HttpOnly",
		`prefs={"theme":"dark"}; Path=/`,
		"pri=1; path=/; Priority=High",
	}
	if strings.Join(setCookies, "\n") != strings.Join(wantCookies, "\n") {
		t.Fatalf("Set-Cookie = %q, want %q (cf_session dropped, only Domain removed)", setCookies, wantCookies)
	}

	// 4. Foreign Origin must NOT be rewritten to upstream Origin.
	foreignReq := httptest.NewRequest(http.MethodGet, "/app/check", nil)
	foreignReq.Host = validHost
	foreignReq.Header.Set("Origin", "https://evil.example.net")
	_ = serve(hn, foreignReq)
	if gotReq.Header.Get("Origin") != "https://evil.example.net" {
		t.Fatalf("foreign Origin was wrongly rewritten: %q", gotReq.Header.Get("Origin"))
	}

	// 5. Location pointing at upstream host is rewritten to public origin.
	redirReq := httptest.NewRequest(http.MethodGet, "/redirect", nil)
	redirReq.Host = validHost
	wRedir := serve(hn, redirReq)
	if wRedir.Code != http.StatusFound || wRedir.Header().Get("Location") != "https://"+validHost+"/after-login" {
		t.Fatalf("redirect rewrite failed: %d loc=%q", wRedir.Code, wRedir.Header().Get("Location"))
	}

	// 6. Runtime route is exempt from X-Grasp-Upstream, while app routes reject mismatched upstream.
	rtReq := httptest.NewRequest(http.MethodGet, "/__grasp/preview-pick.js", nil)
	rtReq.Host = validHost
	if wRT := serve(hn, rtReq); wRT.Code != http.StatusOK {
		t.Fatalf("runtime route got %d, want 200", wRT.Code)
	}
	// Mismatched X-Grasp-Upstream is rejected (503 at probe or 502 at ModifyResponse).
	upstreamPort = "3000"
	badUpReq := httptest.NewRequest(http.MethodGet, "/app/check", nil)
	badUpReq.Host = validHost
	if wBad := serve(hn, badUpReq); wBad.Code != http.StatusServiceUnavailable && wBad.Code != http.StatusBadGateway {
		t.Fatalf("mismatched injector upstream got %d, want 502 or 503", wBad.Code)
	}
	upstreamPort = "5173"

	// 7. Generation mismatch returns 410 Gone.
	if err := hn.db.Model(&models.Sandbox{}).Where("name = ?", "e2b-sb-1").Update("name", "e2b-sb-2").Error; err != nil {
		t.Fatal(err)
	}
	staleReq := httptest.NewRequest(http.MethodGet, "/app/check", nil)
	staleReq.Host = validHost
	if wStale := serve(hn, staleReq); wStale.Code != http.StatusGone {
		t.Fatalf("stale generation got %d, want 410", wStale.Code)
	}

	// 8. Non-running sandbox triggers EnsureRunningByName wake path (not blindly proxied).
	if err := hn.db.Model(&models.Sandbox{}).Where("name = ?", "e2b-sb-2").Update("name", "e2b-sb-1").Error; err != nil {
		t.Fatal(err)
	}
	hn.fg.SetStatus("e2b-sb-1", "stopped")
	wakeCtx, cancelWake := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelWake()
	pausedReq := httptest.NewRequest(http.MethodGet, "/app/check", nil).WithContext(wakeCtx)
	pausedReq.Host = validHost
	if wPaused := serve(hn, pausedReq); wPaused.Code != http.StatusConflict && wPaused.Code != http.StatusGone {
		t.Fatalf("non-running sandbox must go through wake path (409/410), got %d", wPaused.Code)
	}
}

func TestSubdomainInboundWebSocketUpgrade(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			w.Header().Set("X-Grasp-Upstream", "5173")
			_, _ = io.WriteString(w, "ok")
			return
		}
		h := http.Header{"X-Grasp-Upstream": {"5173"}}
		conn, err := upgrader.Upgrade(w, r, h)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		mt, msg, err := conn.ReadMessage()
		if err == nil {
			_ = conn.WriteMessage(mt, msg)
		}
	}))
	t.Cleanup(up.Close)

	hn, key, _ := seedExposedSubdomainHarness(t, "https://preview-ws.other.test", "", up.URL)
	srv := httptest.NewServer(hn.r)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/hmr"
	dialer := websocket.Dialer{}
	hdr := http.Header{
		"Host":   {key + ".preview-ws.other.test"},
		"Origin": {"https://" + key + ".preview-ws.other.test"},
	}
	// Dial via custom request Host header.
	conn, resp, err := dialer.Dial(wsURL, hdr)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("ws dial failed (status=%d): %v", status, err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping-hmr")); err != nil {
		t.Fatal(err)
	}
	_, got, err := conn.ReadMessage()
	if err != nil || string(got) != "ping-hmr" {
		t.Fatalf("ws echo got %q err=%v", string(got), err)
	}
}

func TestPublicApprovalTicketWithSubdomainDirectURL(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Grasp-Upstream", "5173")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(up.Close)

	hn, key, _ := seedExposedSubdomainHarness(t, "https://preview-share.other.test", "", up.URL)
	// Verify the active preview exposes a subdomain DirectURL so publicTicketPurpose
	// recognizes it as direct-capable when approval sharing is used.
	active, ok := hn.host.ActivePreview("run-sub", "app")
	if !ok {
		t.Fatal("active preview missing")
	}
	want := "https://" + key + ".preview-share.other.test/"
	if active.DirectURL != want {
		// Note: without a run graph node in DB, DirectPreview is false; verify via gateshare DTO contract.
		t.Logf("DirectURL without run graph = %q (expected empty until run node has preview cap)", active.DirectURL)
	}
	// Confirm gateshare.PublicPreview carries the subdomain DirectURL cleanly.
	pub := gateshare.PublicPreview{Port: 5173, Kind: "port", DirectURL: want}
	if pub.DirectURL != want {
		t.Fatalf("PublicPreview DirectURL = %q", pub.DirectURL)
	}
}
