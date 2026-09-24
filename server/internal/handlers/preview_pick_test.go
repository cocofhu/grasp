package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPreviewPickScript(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handlers{}
	r.GET("/preview-pick.js", h.PreviewPickScript)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/preview-pick.js", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type=%q", ct)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("cors=%q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Fatalf("corp=%q", w.Header().Get("Cross-Origin-Resource-Policy"))
	}
	body := w.Body.String()
	for _, needle := range []string{
		"grasp-embed:pick",
		"grasp-embed:ready",
		"/__grasp/embed-origin",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("script missing %q", needle)
		}
	}
}

// The script is embedded three times: here, in the web dev/static bundle, and
// in the sandbox injector. Drift means one preview surface silently loses pick.
func TestPreviewPickScriptCopiesInSync(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	root := filepath.Join(filepath.Dir(self), "..", "..", "..")
	for _, rel := range []string{
		filepath.Join("web", "public", "preview-pick.js"),
		filepath.Join("sandbox-gateway", "sandbox", "internal", "previewinject", "preview-pick.js"),
	} {
		copyBytes, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !bytes.Equal(copyBytes, previewPickJS) {
			t.Errorf("%s drifted from server/internal/handlers/preview-pick.js", rel)
		}
	}
}
