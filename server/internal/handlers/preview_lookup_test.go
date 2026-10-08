package handlers

import (
	"context"
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

func TestLookupPreviewAndResolveDesktop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/p.db")
	if err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	fg.Seed("sb1")
	fg.SetEndpoints("sb1", map[string]string{
		"session": "10.0.0.5:34567",
		"3000":    "10.0.0.5:3000",
	})
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	skills := services.NewAgentService(t.TempDir())
	hostMCP := mcp.NewHost(services.NewArtifactService(db))
	sbx := services.NewSandboxService(db, mgr, skills, hostMCP, services.SandboxOptions{Max: 2, TTL: time.Minute})
	preview := services.NewPreviewService(db, mgr)
	hostMCP.SetPreviewStore(preview)

	_ = preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "r1", NodeID: "n1", Port: 3000, Label: "web",
		Host: "http://10.0.0.5:3000", SandboxName: "sb1", Healthy: true,
		RegisteredAt: time.Now(),
	})
	_ = db.Create(&models.Sandbox{
		Name: "sb1", RunID: "r1", NodeID: "n1", Purpose: "run", Status: "running",
	}).Error

	h := &Handlers{MCP: hostMCP, Preview: preview, Sbx: sbx}
	gotHost, gotName := h.lookupPreviewRegistration("r1", "n1", 3000)
	if gotHost == "" || gotName != "sb1" {
		t.Fatalf("lookup = %q %q", gotHost, gotName)
	}

	ip, code, msg := h.resolveDesktopSandbox(context.Background(), "sb1")
	if code != 0 || ip == "" {
		t.Fatalf("resolve = %q %d %s", ip, code, msg)
	}

	fg.SetStatus("sb1", "stopped")
	if _, code, _ = h.resolveDesktopSandbox(context.Background(), "sb1"); code != http.StatusGone {
		t.Fatalf("stopped resolve = %d", code)
	}
	if _, code, _ = (&Handlers{}).resolveDesktopSandbox(context.Background(), "sb1"); code != http.StatusServiceUnavailable {
		t.Fatalf("nil sbx resolve = %d", code)
	}
}

func TestSandboxVNCDisabledPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/vnc.db")
	if err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	skills := services.NewAgentService(t.TempDir())
	hostMCP := mcp.NewHost(services.NewArtifactService(db))
	sbx := services.NewSandboxService(db, mgr, skills, hostMCP, services.SandboxOptions{Max: 2, TTL: time.Minute})

	h := &Handlers{Sbx: sbx} // Browser nil
	r := gin.New()
	r.GET("/ws/sandboxes/:sandboxId/vnc", h.SandboxVNC)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/1/vnc", nil))
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != "vnc preview disabled" {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}

	h.Browser = browser.New(&nopSandboxExec{}, browser.Config{})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/abc/vnc", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/99/vnc", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing sandbox: %d %s", w.Code, w.Body.String())
	}

	// Sbx nil
	h2 := &Handlers{Browser: browser.New(&nopSandboxExec{}, browser.Config{})}
	r2 := gin.New()
	r2.GET("/ws/sandboxes/:sandboxId/vnc", h2.SandboxVNC)
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/1/vnc", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil sbx: %d", w.Code)
	}
}

type nopSandboxExec struct{}

func (nopSandboxExec) Exec(context.Context, string, time.Duration, ...string) (string, error) {
	return "", nil
}
