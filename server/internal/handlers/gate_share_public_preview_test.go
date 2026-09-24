package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestPublicPreviewAPIProxyAllowsSameOriginFraming(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-ap-api", "app_preview_api")

	created := parseJSON(t, hn.do(http.MethodPost, "/api/runs/run-ap-api/reviews/app_preview_api/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		_, _ = w.Write([]byte(`<!doctype html><html><head></head><body>api-ok</body></html>`))
	}))
	t.Cleanup(up.Close)

	preview := services.NewPreviewService(hn.db, nil)
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	hn.host.SetPreviewSandboxOps(preview)
	var run models.Run
	if err := hn.db.Where("id = ?", "run-ap-api").First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if n := run.Graph.FindNode("app_preview_api"); n != nil {
		n.Config = map[string]any{"direct_preview": true}
	}
	if err := hn.db.Save(&run).Error; err != nil {
		t.Fatal(err)
	}
	_ = preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "run-ap-api", NodeID: "app_preview_api", Port: 8080, Label: "API · 8080",
		Host: up.URL, Healthy: true, RegisteredAt: time.Now(),
		Mode: "direct", DirectURL: "http://127.0.0.1:8080/",
	})

	ticketRes := parseJSON(t, hn.doPublic(http.MethodPost, "/public/gate-approvals/preview-ticket", map[string]any{
		"port": 8080, "purpose": "api",
	}, map[string]string{
		headerShareToken:   token,
		headerShareRequest: "1",
		"Origin":           "http://" + publicHost,
	}))
	if ticketRes["status"] != models.ShareLinkStateActive {
		t.Fatalf("ticket: %+v", ticketRes)
	}
	ticket, _ := ticketRes["ticket"].(string)
	if ticket == "" {
		t.Fatalf("missing ticket: %+v", ticketRes)
	}

	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
	req := httptest.NewRequest(http.MethodGet, "/public/gate-approvals/preview-api/"+ticket+"/", nil)
	req.Host = "pv." + publicHost
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Frame-Options"); strings.EqualFold(got, "DENY") {
		t.Fatalf("API proxy must not set X-Frame-Options DENY, got %q", got)
	}
	csp := strings.ToLower(w.Header().Get("Content-Security-Policy"))
	if strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, `frame-ancestors "none"`) {
		t.Fatalf("API proxy must not keep frame-ancestors none: %q", csp)
	}
	if !strings.Contains(csp, "frame-ancestors http://"+publicHost) {
		t.Fatalf("API proxy should allow the approval origin to frame the preview host, csp=%q", csp)
	}
	if !strings.Contains(w.Body.String(), "api-ok") {
		t.Fatalf("expected upstream body: %s", w.Body.String())
	}
}
