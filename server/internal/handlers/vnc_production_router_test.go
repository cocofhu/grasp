package handlers_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/router"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gorilla/websocket"
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

func TestProductionSandboxAudioRequiresSession(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, wsUpgradeRequest("/sandbox-audio/1/ws"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d %s", w.Code, w.Body.String())
	}
	assertNoWSUpgrade(t, w)

	w = httptest.NewRecorder()
	req := wsUpgradeRequest("/sandbox-audio/1/ws")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 404/503 business response, got %d %s", w.Code, w.Body.String())
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

func TestProductionSandboxACPClipboardRequiresSoloOwner(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
	platform := httptest.NewServer(hn.r)
	defer platform.Close()
	var calls atomic.Int32
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/desktop/clipboard" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"generation":"x11","revision":1,"owned":true}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer bridge.Close()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(bridge.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"other-user", "admin"} {
		session := models.SoloSession{ID: "solo-clipboard-" + owner, UserID: owner, AgentName: "a", Status: models.SoloStatusRunning}
		if err := hn.db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
		row := models.Sandbox{Name: session.ID, Host: host, ACPPort: port, Status: "running", Purpose: services.SandboxPurposeSolo, ThreadID: session.ID}
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		endpoint := platform.URL + "/sandbox-bridge/" + uintToStr(row.ID) + "/api/desktop/clipboard"
		for _, tc := range []struct {
			method string
			body   io.Reader
			status int
		}{
			{http.MethodGet, nil, http.StatusOK},
			{http.MethodPut, strings.NewReader(`{"text":"clipboard test"}`), http.StatusNoContent},
		} {
			wantStatus, wantCalls := http.StatusNotFound, calls.Load()
			if owner == "admin" {
				wantStatus = tc.status
				wantCalls++
			}
			req, err := http.NewRequestWithContext(t.Context(), tc.method, endpoint, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
			req.Header.Set("Content-Type", "application/json")
			resp, err := platform.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != wantStatus || calls.Load() != wantCalls {
				t.Fatalf("%s sandbox owned by %s: status %d, calls %d: %s", tc.method, owner, resp.StatusCode, calls.Load(), body)
			}
		}
	}
}

func TestProductionPreviewOriginCannotUseWorkspaceCookie(t *testing.T) {
	hn := newHarness(t)
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/auth/me"},
		{http.MethodGet, "/api/runs/other-run/events"},
		{http.MethodGet, "/api/sandboxes/1/terminal"},
		{http.MethodGet, "/sandbox-vnc/1/ws"},
		{http.MethodPost, "/api/auth/logout"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := wsUpgradeRequest(tc.path)
			req.Method = tc.method
			req.Host = "127.0.0.1:5173"
			req.Header.Set("Origin", "http://127.0.0.1:19000")
			req.Header.Set("Sec-Fetch-Site", "same-site")
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
			w := httptest.NewRecorder()
			hn.r.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("cross-origin session request got %d, want 403", w.Code)
			}
			assertNoWSUpgrade(t, w)
		})
	}
}

func TestProductionWorkspaceSocketRejectsPreviewOrigin(t *testing.T) {
	hn := newHarness(t)
	srv := httptest.NewServer(hn.r)
	defer srv.Close()
	address := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/runs/other-run/events"
	headers := http.Header{
		"Origin": {"http://127.0.0.1:19000"},
		"Cookie": {(&http.Cookie{Name: auth.CookieName, Value: hn.cookie}).String()},
	}
	conn, resp, err := websocket.DefaultDialer.Dial(address, headers)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("a preview origin upgraded an authenticated workspace socket")
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected cross-origin upgrade to return 403, got %v", err)
	}
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
