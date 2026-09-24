package handlers_test

import (
	"net/http"
	"net/http/httptest"
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
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `/preview/run-px/n1/9090/`) {
		t.Fatalf("expected rewritten html: %s", w.Body.String())
	}
}

func TestPreviewProxyRewritesOtherProjectLoginCookies(t *testing.T) {
	hn := newHarness(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login" {
			t.Errorf("upstream path = %q, want /login", r.URL.Path)
		}
		http.SetCookie(w, &http.Cookie{
			Name: "shop_session", Value: "abc", Path: "/", Domain: "preview.invalid", HttpOnly: true,
		})
		http.SetCookie(w, &http.Cookie{
			Name: "remember_me", Value: "yes", Path: "/account",
		})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head></head><body>signed-in</body></html>`))
	}))
	t.Cleanup(up.Close)

	preview := services.NewPreviewService(hn.db, nil)
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	_ = preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "run-px", NodeID: "n1", Port: 9090, Label: "shop",
		Host: up.URL, Healthy: true, RegisteredAt: time.Now(),
	})

	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
	req := httptest.NewRequest(http.MethodPost, "/preview/run-px/n1/9090/login", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
	vals := w.Result().Header.Values("Set-Cookie")
	joined := strings.ToLower(strings.Join(vals, "\n"))
	if strings.Contains(joined, "domain=") {
		t.Fatalf("Domain leaked: %v", vals)
	}
	if len(vals) != 2 {
		t.Fatalf("cookie count = %d, want 2: %v", len(vals), vals)
	}
	for _, v := range vals {
		if !strings.Contains(v, "Path=/preview/run-px/n1/9090/") {
			t.Fatalf("cookie path not scoped to this preview: %q", v)
		}
	}
	if !strings.Contains(vals[0], "shop_session=") || !strings.Contains(vals[1], "remember_me=") {
		t.Fatalf("expected both login cookies preserved by name, got %v", vals)
	}
	if strings.Contains(w.Body.String(), "Path=/") && !strings.Contains(w.Body.String(), "/preview/") {
		t.Fatalf("html should be re-anchored: %s", w.Body.String())
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
