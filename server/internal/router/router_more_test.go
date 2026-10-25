package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/shutdown"

	"github.com/gin-gonic/gin"
)

func TestIsDrainBlocked(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/api/workflows", false},
		{http.MethodHead, "/api/x", false},
		{http.MethodOptions, "/api/x", false},
		{http.MethodPost, "/api/health", false},
		{http.MethodPost, "/api/live", false},
		{http.MethodPost, "/api/workflows", true},
		{http.MethodDelete, "/api/workflows/1", true},
		{http.MethodPost, "/v1/workflows/1/runs", false},
	}
	for _, tc := range cases {
		if got := isDrainBlocked(tc.method, tc.path); got != tc.want {
			t.Errorf("isDrainBlocked(%s %s)=%v want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestDrainMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	coord := shutdown.New(30 * time.Second)
	coord.BeginDraining()
	h := &handlers.Handlers{Shutdown: coord}
	r := New(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/workflows", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("draining POST: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
	// GET is allowed through drain middleware; may 401 without auth
	if w.Code == http.StatusServiceUnavailable {
		t.Fatalf("GET should not be drain-blocked: %d", w.Code)
	}
}

func TestNewLiveAndNodeRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := New(&handlers.Handlers{})
	for _, path := range []string{"/api/live", "/api/node-registry"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestErrorLogger5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(errorLogger())
	r.GET("/boom", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "x"})
	})
	r.GET("/warn", func(c *gin.Context) {
		_ = c.Error(http.ErrAbortHandler)
		c.JSON(http.StatusBadRequest, gin.H{"error": "y"})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if w.Code != 500 {
		t.Fatalf("boom: %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/warn", nil))
	if w.Code != 400 {
		t.Fatalf("warn: %d", w.Code)
	}
}

func TestPublicGateMiddlewareHeadersAndPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(cors(), publicGateMiddleware())
	r.GET("/public/approvals/preview", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.OPTIONS("/public/approvals/preview", func(c *gin.Context) {
		c.Status(http.StatusTeapot)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/public/approvals/preview", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status=%d", w.Code)
	}
	for name, want := range map[string]string{
		"Cache-Control":           "no-store",
		"Pragma":                  "no-cache",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "frame-ancestors 'none'",
	} {
		if got := w.Header().Get(name); !strings.Contains(got, want) {
			t.Errorf("%s=%q, want to contain %q", name, got, want)
		}
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("public route must not expose CORS, got %q", got)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/public/approvals/preview", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status=%d, want 204", w.Code)
	}
}

func TestRetiredGatePathsAre404WithoutWildcardCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := New(&handlers.Handlers{})
	paths := []struct{ method, path string }{
		{http.MethodGet, "/api/gates"},
		{http.MethodGet, "/api/gates?wf=wf-1&tag=urgent"},
		{http.MethodPost, "/api/runs/r1/gates/n1/resume"},
		{http.MethodPost, "/api/runs/r1/gates/n1/share-link"},
		{http.MethodPost, "/api/runs/r1/react/n1/reply"},
		{http.MethodPost, "/api/runs/r1/react/n1/cancel"},
		{http.MethodPost, "/api/runs/r1/reviews/n1/share-link"},
		{http.MethodGet, "/api/runs/r1/inbox-context?nodeId=n1&iteration=1"},
		{http.MethodGet, "/public/gate-approvals"},
		{http.MethodGet, "/public/gate-approvals/preview"},
		{http.MethodOptions, "/public/gate-approvals/preview"},
		{http.MethodPost, "/public/gate-approvals/decide"},
		{http.MethodOptions, "/public/gate-approvals/queue/remove"},
	}
	for _, tc := range paths {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if strings.HasPrefix(tc.path, "/public/gate-approvals") {
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("%s %s CORS=%q", tc.method, tc.path, got)
			}
		}
	}
}

func TestPublicGateQueuePreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := New(&handlers.Handlers{})
	for _, path := range []string{
		"/public/approvals/queue/remove",
		"/public/approvals/queue/reorder",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, path, nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("OPTIONS %s status=%d, want 204", path, w.Code)
		}
	}
}
