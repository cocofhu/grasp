package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/gorilla/websocket"
)

func TestSandboxAudioEarlyFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/audio.db")
	if err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	skills := services.NewAgentService(t.TempDir())
	hostMCP := mcp.NewHost(services.NewArtifactService(db))
	sbx := services.NewSandboxService(db, mgr, skills, hostMCP, services.SandboxOptions{Max: 2, IdleTTL: time.Minute})
	bsvc := browser.New(&nopSandboxExec{}, browser.Config{})

	r := gin.New()
	r.GET("/a-nil-browser/:sandboxId", (&Handlers{Sbx: sbx}).SandboxAudio)
	r.GET("/a-nil-sbx/:sandboxId", (&Handlers{Browser: bsvc}).SandboxAudio)
	r.GET("/a/:sandboxId", (&Handlers{Browser: bsvc, Sbx: sbx}).SandboxAudio)

	stopped := models.Sandbox{Name: "sb-stopped", Purpose: "test", Status: "stopped"}
	if err := db.Create(&stopped).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/a-nil-browser/1", http.StatusServiceUnavailable},
		{"/a-nil-sbx/1", http.StatusServiceUnavailable},
		{"/a/bad", http.StatusBadRequest},
		{"/a/99", http.StatusNotFound},
		// A stopped sandbox is not woken for sound.
		{"/a/" + utoa(stopped.ID), http.StatusGone},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("%s: want %d got %d %s", tc.path, tc.want, w.Code, w.Body.String())
		}
	}
}

func TestServeDesktopAudioRelaysBinary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	viewerLeft := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio" {
			http.NotFound(w, r)
			return
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte("ignored"))
		_ = c.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3, 4})
		if _, _, err := c.ReadMessage(); err != nil {
			close(viewerLeft)
		}
	}))
	defer up.Close()
	upURL := "ws" + strings.TrimPrefix(up.URL, "http")

	r := gin.New()
	r.GET("/ok", func(c *gin.Context) { serveDesktopAudio(c, upURL+"/audio") })
	r.GET("/down", func(c *gin.Context) { serveDesktopAudio(c, upURL+"/nope") })
	srv := httptest.NewServer(r)
	defer srv.Close()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")

	_, resp, err := websocket.DefaultDialer.Dial(base+"/down", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("down: want 503, got %v %v", resp, err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(base+"/ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	typ, data, err := conn.ReadMessage()
	if err != nil || typ != websocket.BinaryMessage || string(data) != "\x01\x02\x03\x04" {
		t.Fatalf("frame type=%d data=%v err=%v", typ, data, err)
	}
	conn.Close()
	select {
	case <-viewerLeft:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not closed after viewer left")
	}

	// A plain (non-upgrade) request reaches the upstream but stops at the upgrader.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plain GET: %d", w.Code)
	}
}
