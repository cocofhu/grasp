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
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("the preview app received workspace credentials")
		}
		http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "preview-fixture", Path: "/"})
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
	req.Header.Set("Authorization", "Bearer preview-test")
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `/preview/run-px/n1/9090/`) {
		t.Fatalf("expected rewritten html: %s", w.Body.String())
	}
	if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Content-Security-Policy") != "sandbox allow-scripts allow-forms" {
		t.Fatal("the preview response was not isolated from the workspace origin")
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
