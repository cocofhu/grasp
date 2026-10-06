package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func TestSandboxVNCEarlyNilAndNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/vnc4.db")
	if err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	skills := services.NewAgentService(t.TempDir())
	hostMCP := mcp.NewHost(services.NewArtifactService(db))
	sbx := services.NewSandboxService(db, mgr, skills, hostMCP, services.SandboxOptions{Max: 2, TTL: time.Minute})
	bsvc := browser.New(&nopSandboxExec{}, browser.Config{})

	r := gin.New()
	hNilBrowser := &Handlers{Sbx: sbx}
	r.GET("/ws/sandboxes/:sandboxId/vnc-nil-browser", hNilBrowser.SandboxVNC)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/1/vnc-nil-browser", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil browser: %d %s", w.Code, w.Body.String())
	}

	hNilSbx := &Handlers{Browser: bsvc}
	r.GET("/ws/sandboxes/:sandboxId/vnc-nil-sbx", hNilSbx.SandboxVNC)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/1/vnc-nil-sbx", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil sbx: %d %s", w.Code, w.Body.String())
	}

	h := &Handlers{Browser: bsvc, Sbx: sbx}
	r.GET("/ws/sandboxes/:sandboxId/vnc", h.SandboxVNC)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/bad/vnc", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/99/vnc", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", w.Code, w.Body.String())
	}

	// Empty name row → not found
	row := models.Sandbox{Name: "", Purpose: "test", Status: "running"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/"+utoa(row.ID)+"/vnc", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("empty name: %d %s", w.Code, w.Body.String())
	}
}

func TestPreviewVNCNilDeps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handlers{}
	r.GET("/ws/preview/:runId/:nodeId/:port/vnc", h.PreviewVNC)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/preview/r/n/3000/vnc", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil browser: %d %s", w.Code, w.Body.String())
	}
}
