package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func seedPrivatePathPreview(t *testing.T, hn *harness, upstream string) {
	t.Helper()
	hn.host.SetPrivateEndpoints(true)
	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	hn.host.SetPreviewSandboxOps(preview)
	row := models.Sandbox{Name: "private-sandbox", RunID: "private-run", NodeID: "app", Purpose: "run", Status: "running"}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	hn.fg.Seed(row.Name)
	hn.fg.SetEndpoints(row.Name, map[string]string{
		"18080": strings.TrimPrefix(upstream, "http://"),
		"5174":  "127.0.0.1:1",
	})
	if err := preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "private-run", NodeID: "app", Kind: mcp.PreviewKindPort, Port: 5174,
		ProxyURL: "https://0123456789abcdef.preview.example.net/", Host: "http://127.0.0.1:9", InjectorPort: 18080,
		SandboxName: row.Name, Healthy: true, RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPrivatePathPreviewStripsProviderHeadersAndChecksUpstream(t *testing.T) {
	hn := newHarness(t)
	upstreamPort := "5174"
	seen := make(chan *http.Request, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/x" {
			seen <- r.Clone(r.Context())
		}
		w.Header().Set("X-Grasp-Upstream", upstreamPort)
		w.Header().Set("E2b-Traffic-Access-Token", "provider-secret")
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	t.Cleanup(up.Close)
	seedPrivatePathPreview(t, hn, up.URL)

	ports := hn.host.ListPreviewPorts("private-run", "app")
	if len(ports) != 1 || ports[0].ProxyURL != "/preview/private-run/app/5174/" || ports[0].DirectURL != "" {
		t.Fatalf("stored subdomain URL not served by the path proxy: %+v", ports)
	}

	req := httptest.NewRequest(http.MethodGet, "/preview/private-run/app/5174/api/x", nil)
	req.Header.Set("E2b-Traffic-Access-Token", "provider-secret")
	req.Header.Set("X-E2b-Sandbox-Id", "provider-id")
	req.Header.Set("Proxy-Authorization", "Basic secret")
	req.Header.Set("Authorization", "Bearer app-secret")
	w := serve(hn, req)
	if w.Code != http.StatusOK || w.Body.String() != "/api/x" || w.Header().Get("E2b-Traffic-Access-Token") != "" {
		t.Fatalf("private path proxy: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	got := <-seen
	if got.Header.Get("E2b-Traffic-Access-Token") != "" || got.Header.Get("X-E2b-Sandbox-Id") != "" ||
		got.Header.Get("Proxy-Authorization") != "" || got.Header.Get("Authorization") != "Bearer app-secret" {
		t.Fatalf("request headers leaked or app headers lost: %v", got.Header)
	}

	upstreamPort = "3000"
	if w := serve(hn, httptest.NewRequest(http.MethodGet, "/preview/private-run/app/5174/", nil)); w.Code == http.StatusOK {
		t.Fatal("injector serving another app was proxied")
	}

	upstreamPort = "5174"
	if err := hn.db.Model(&models.Sandbox{}).Where("name = ?", "private-sandbox").Update("name", "next-generation").Error; err != nil {
		t.Fatal(err)
	}
	if w := serve(hn, httptest.NewRequest(http.MethodGet, "/preview/private-run/app/5174/", nil)); w.Code != http.StatusGone {
		t.Fatalf("old sandbox generation remained reachable: %d", w.Code)
	}
}

func serve(hn *harness, req *http.Request) *httptest.ResponseRecorder {
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
	hn.r.ServeHTTP(w, req)
	return w.ResponseRecorder
}
