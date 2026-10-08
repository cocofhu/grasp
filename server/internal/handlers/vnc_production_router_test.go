package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/router"
)

// g4.1: production paths registered by router.New(). /sandbox-vnc/:id/ws is
// the only signed-in VNC address; per-port preview-vnc addresses are gone.

func wsUpgradeRequest(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return req
}

func assertNoWSUpgrade(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if strings.EqualFold(w.Header().Get("Upgrade"), "websocket") {
		t.Fatalf("must not upgrade websocket, status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Code == http.StatusSwitchingProtocols {
		t.Fatalf("must not switch protocols: %d %s", w.Code, w.Body.String())
	}
}

func TestProductionSandboxVNCRequiresSessionNoCookie(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, wsUpgradeRequest("/sandbox-vnc/1/ws"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d %s", w.Code, w.Body.String())
	}
	assertNoWSUpgrade(t, w)
}

func TestProductionSandboxVNCValidSessionEntersHandler(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	req := wsUpgradeRequest("/sandbox-vnc/1/ws")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("valid session must not 401: %s", w.Body.String())
	}
	// sandbox 1 is missing → 404; Browser unset on harness → 503. Either is past RequireSession.
	if w.Code != http.StatusNotFound && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 404/503 business response, got %d %s", w.Code, w.Body.String())
	}
	assertNoWSUpgrade(t, w)
}

func TestProductionPerPortPreviewVNCRouteRemoved(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	req := wsUpgradeRequest("/preview-vnc/r/n/5173/ws")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for removed per-port VNC route, got %d %s", w.Code, w.Body.String())
	}
	assertNoWSUpgrade(t, w)
}

func TestProductionVNCAuthNilDoesNotRequireSession(t *testing.T) {
	hn := newHarness(t)
	hn.h.Auth = nil
	r := router.New(hn.h)
	for _, path := range []string{"/sandbox-vnc/1/ws"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, wsUpgradeRequest(path))
		if w.Code == http.StatusUnauthorized {
			t.Fatalf("%s Auth==nil must not 401: %s", path, w.Body.String())
		}
		assertNoWSUpgrade(t, w)
	}
}

// g4.2: Preview HTTP reverse-proxy stays outside Session (iframe cannot send cf_session).
func TestPreviewHTTPProxyDoesNotRequireSession(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/preview/r/n/5173/", nil))
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("Preview HTTP must not require session, got 401 %s", w.Body.String())
	}
}
