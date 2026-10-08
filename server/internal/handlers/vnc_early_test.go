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

func TestSandboxVNCRecycledAndBadHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/vnc3.db")
	if err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	skills := services.NewAgentService(t.TempDir())
	hostMCP := mcp.NewHost(services.NewArtifactService(db))
	sbx := services.NewSandboxService(db, mgr, skills, hostMCP, services.SandboxOptions{Max: 2, TTL: time.Minute})
	bsvc := browser.New(&nopSandboxExec{}, browser.Config{})
	h := &Handlers{Browser: bsvc, Sbx: sbx}
	r := gin.New()
	r.GET("/ws/sandboxes/:sandboxId/vnc", h.SandboxVNC)

	row := models.Sandbox{Name: "sb-gone", Purpose: "test", Status: "running"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	// not in gateway → recycled
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws/sandboxes/"+utoa(row.ID)+"/vnc", nil))
	if w.Code != http.StatusGone {
		t.Fatalf("gone: %d %s", w.Code, w.Body.String())
	}
}

func utoa(u uint) string {
	return stringsForUint(u)
}

func stringsForUint(u uint) string {
	if u == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}

func TestPagePersistedEvents(t *testing.T) {
	ev := []models.AcpEvent{{Kind: "a"}, {Kind: "b"}, {Kind: "c"}, {Kind: "d"}}
	page, cur, more := pagePersistedEvents(ev, "", 2)
	if len(page) != 2 || cur == "" || !more {
		t.Fatalf("first: %+v cur=%s more=%v", page, cur, more)
	}
	page, cur, more = pagePersistedEvents(ev, cur, 2)
	if len(page) != 2 {
		t.Fatalf("second: %+v cur=%s more=%v", page, cur, more)
	}
	page, _, more = pagePersistedEvents(ev, "bad", 2)
	if page != nil || more {
		t.Fatal("bad cursor")
	}
	page, _, more = pagePersistedEvents(nil, "", 2)
	if page != nil || more {
		t.Fatal("empty")
	}
}
