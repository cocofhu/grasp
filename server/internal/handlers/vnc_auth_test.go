package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func TestSandboxVNCRequiresSessionWhenAuthEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/vnc-auth.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Users: []config.AuthUser{
				{Username: "admin", PasswordHash: "$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi"},
			},
			SessionTTL: "168h",
		},
	}
	config.StoreConfig(cfg)
	authSvc := auth.NewService(db, config.GetConfig)
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	skills := services.NewAgentService(t.TempDir())
	hostMCP := mcp.NewHost(services.NewArtifactService(db))
	sbx := services.NewSandboxService(db, mgr, skills, hostMCP, services.SandboxOptions{Max: 2, TTL: time.Minute})
	bsvc := browser.New(&nopSandboxExec{}, browser.Config{})

	h := &Handlers{Browser: bsvc, Sbx: sbx, Auth: authSvc}
	r := gin.New()
	r.GET("/ws/sandboxes/:sandboxId/vnc", h.SandboxVNC)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/1/vnc", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestPreviewVNCRequiresSessionWhenAuthEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/pvnc-auth.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Users: []config.AuthUser{
				{Username: "admin", PasswordHash: "$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi"},
			},
			SessionTTL: "168h",
		},
	}
	config.StoreConfig(cfg)
	authSvc := auth.NewService(db, config.GetConfig)
	bsvc := browser.New(&nopSandboxExec{}, browser.Config{})
	h := &Handlers{Browser: bsvc, Auth: authSvc}
	r := gin.New()
	r.GET("/ws/preview/:runId/:nodeId/:port/vnc", h.PreviewVNC)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/preview/r/n/3000/vnc", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestPreviewVNCBadPort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{Browser: browser.New(&nopSandboxExec{}, browser.Config{})}
	r := gin.New()
	r.GET("/ws/preview/:runId/:nodeId/:port/vnc", h.PreviewVNC)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/preview/r/n/bad/vnc", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad port: %d %s", w.Code, w.Body.String())
	}
}
