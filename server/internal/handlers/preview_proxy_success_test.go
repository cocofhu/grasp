package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/services"
)

// closeNotifyRecorder wraps httptest.ResponseRecorder so httputil.ReverseProxy
// can type-assert http.CloseNotifier (required by gin's ResponseWriter).
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	notify chan bool
}

func (c *closeNotifyRecorder) CloseNotify() <-chan bool { return c.notify }

func TestPreviewProxySuccess(t *testing.T) {
	hn := newHarness(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(auth.CookieName); err != http.ErrNoCookie {
			t.Error("the preview app received the workspace session cookie")
		}
		if cookie, err := r.Cookie("app_session"); err != nil || cookie.Value != "app-fixture" {
			t.Error("the preview app did not receive its own cookie")
		}
		if r.Header.Get("Authorization") != "Bearer app-token" {
			t.Error("the preview app did not receive its own Authorization header")
		}
		http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "preview-fixture", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "app_session", Value: "app-updated", Path: "/"})
		w.Header().Set("Access-Control-Allow-Origin", "https://app.example")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head></head><body><a href="/x">x</a></body></html>`))
	}))
	t.Cleanup(up.Close)

	preview := services.NewPreviewService(hn.db, nil) // no manager → skip health
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	_ = preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "run-px", NodeID: "n1", Port: 9090, Label: "web",
		Host: up.URL, Healthy: true, RegisteredAt: time.Now(),
	})

	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
	req := httptest.NewRequest(http.MethodGet, "/preview/run-px/n1/9090/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	req.AddCookie(&http.Cookie{Name: "app_session", Value: "app-fixture"})
	req.Header.Set("Authorization", "Bearer app-token")
	req.Header.Set("Origin", "null")
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `/preview/run-px/n1/9090/`) {
		t.Fatalf("expected rewritten html: %s", body)
	}
	if !strings.Contains(body, `"localStorage","sessionStorage"`) {
		t.Fatalf("expected the storage shim in the document head: %s", body)
	}
	cookies := map[string]string{}
	for _, cookie := range w.Result().Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	if _, ok := cookies[auth.CookieName]; ok || cookies["app_session"] != "app-updated" {
		t.Fatalf("the preview must update only its own cookies: %v", cookies)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads" {
		t.Fatalf("the preview response was not isolated from the workspace origin: %q", got)
	}
	if got := w.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != "null" {
		t.Fatalf("opaque preview requests need one Access-Control-Allow-Origin: %v", got)
	}
}

func TestPreviewProxyAnswersOpaqueOriginPreflight(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/preview/run-px/n1/9090/api/items", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "x-csrf-token, content-type")
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight: %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "null" ||
		w.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		w.Header().Get("Access-Control-Allow-Headers") != "x-csrf-token, content-type" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatalf("preflight headers: %v", w.Header())
	}
}

func TestPreviewProxyUsesRegisteredInjectorPort(t *testing.T) {
	hn := newHarness(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Grasp-Upstream", "8899")
		_, _ = w.Write([]byte("demo"))
	}))
	defer up.Close()

	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	hn.fg.RequirePublishedPort = true
	hn.fg.Seed("sb-solo")
	hn.fg.SetEndpoints("sb-solo", map[string]string{"18081": up.Listener.Addr().String()})
	for injectorPort, host := range map[int]string{0: up.URL, 18081: "http://127.0.0.1:1"} {
		runID := "solo-session-" + strconv.Itoa(injectorPort)
		if err := preview.UpsertPreviewPort(mcp.PreviewPort{
			RunID: runID, NodeID: "solo", Port: 8899, InjectorPort: injectorPort,
			Host: host, SandboxName: "sb-solo", Healthy: true,
		}); err != nil {
			t.Fatal(err)
		}
		w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
		req := httptest.NewRequest(http.MethodGet, "/preview/"+runID+"/solo/8899/", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
		hn.r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || w.Body.String() != "demo" {
			t.Fatalf("proxy with injector port %d: %d %s", injectorPort, w.Code, w.Body)
		}
	}
}

func TestPreviewProxyNotRegistered(t *testing.T) {
	hn := newHarness(t)
	preview := services.NewPreviewService(hn.db, nil)
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/run-x/n/9090/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d %s", w.Code, w.Body.String())
	}
}
