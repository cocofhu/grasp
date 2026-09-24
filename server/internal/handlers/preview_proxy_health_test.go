package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/services"
)

func TestPreviewProxySandboxRecycled(t *testing.T) {
	hn := newHarness(t)
	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	hn.fg.Seed("sb-dead")
	hn.fg.SetStatus("sb-dead", "stopped")
	_ = preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "run-d", NodeID: "n", Port: 3000,
		Host: "http://127.0.0.1:9", SandboxName: "sb-dead", RegisteredAt: time.Now(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/run-d/n/3000/", nil)
	req.Host = "pv.example.com"
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusGone {
		t.Fatalf("want gone got %d %s", w.Code, w.Body.String())
	}
}

func TestPreviewProxyUnhealthy(t *testing.T) {
	hn := newHarness(t)
	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	hn.fg.Seed("sb-uh")
	hn.fg.SetEndpoints("sb-uh", map[string]string{
		"session": "127.0.0.1:34567",
		"3000":    "127.0.0.1:1", // closed port → probe fail
	})
	_ = preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "run-u", NodeID: "n", Port: 3000,
		Host: "http://127.0.0.1:1", SandboxName: "sb-uh", RegisteredAt: time.Now(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/run-u/n/3000/", nil)
	req.Host = "pv.example.com"
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", w.Code, w.Body.String())
	}
}
