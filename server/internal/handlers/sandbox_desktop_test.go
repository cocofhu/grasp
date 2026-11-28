package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type desktopFixture struct {
	db        *gorm.DB
	r         *gin.Engine
	h         *Handlers
	control   []map[string]string
	clipboard chan string
	fail      bool
}

func newDesktopFixture(t *testing.T) *desktopFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/desktop.db")
	if err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	agents := services.NewAgentService(t.TempDir())
	sbx := services.NewSandboxService(db, mgr, agents, mcp.NewHost(services.NewArtifactService(db)), services.SandboxOptions{Max: 2, IdleTTL: time.Minute})
	f := &desktopFixture{db: db, clipboard: make(chan string, 1)}
	f.h = &Handlers{Sbx: sbx, Agents: agents, Solo: services.NewSoloService(db, nil, agents, nil)}
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		if u := c.GetHeader("X-Test-User"); u != "" {
			c.Set("auth_session", &models.Session{Username: u})
		}
	})
	f.r.GET("/api/mcp-presets", f.h.ListMCPPresets)
	f.r.GET("/api/sandboxes/:id/desktop", f.h.SandboxDesktopStatus)
	f.r.PUT("/api/sandboxes/:id/desktop/control", f.h.SandboxDesktopControl)
	return f
}

func (f *desktopFixture) bridge(t *testing.T) (string, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "c", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case f.fail:
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/api/desktop/status":
			_, _ = w.Write([]byte(`{"mode":"desktop","shellReady":true,"agentActive":true,"holder":"agent","apps":[]}`))
		case r.URL.Path == "/api/desktop/control":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.control = append(f.control, body)
			_, _ = w.Write([]byte(`{"mode":"desktop","holder":"` + body["holder"] + `","apps":[]}`))
		case r.URL.Path == "/api/desktop/clipboard" && r.Method == http.MethodPut:
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.clipboard <- body.Text
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func (f *desktopFixture) sandbox(t *testing.T, row models.Sandbox) uint {
	t.Helper()
	host, port := f.bridge(t)
	row.Name, row.Host, row.ACPPort, row.Token = "sb-desktop-"+strconv.Itoa(port), host, port, "pw"
	if row.Status == "" {
		row.Status = "running"
	}
	if err := f.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func (f *desktopFixture) do(method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func TestListMCPPresets(t *testing.T) {
	f := newDesktopFixture(t)
	w := f.do(http.MethodGet, "/api/mcp-presets", "", "")
	var out struct {
		Presets []services.MCPPreset `json:"presets"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Presets) != 4 {
		t.Fatalf("presets %d %s", w.Code, w.Body.String())
	}
}

func TestSandboxDesktopStatusAndControl(t *testing.T) {
	f := newDesktopFixture(t)
	id := utoa(f.sandbox(t, models.Sandbox{Purpose: "agent"}))

	w := f.do(http.MethodGet, "/api/sandboxes/"+id+"/desktop", "alice", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mode":"desktop"`) {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	w = f.do(http.MethodPut, "/api/sandboxes/"+id+"/desktop/control", "alice", `{"holder":"user","client":"tab1"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"holder":"user"`) {
		t.Fatalf("control %d %s", w.Code, w.Body.String())
	}
	if len(f.control) != 1 || f.control[0]["client"] != "alice:tab1" || f.control[0]["holder"] != "user" {
		t.Fatalf("forwarded %+v", f.control)
	}

	f.fail = true
	if w := f.do(http.MethodGet, "/api/sandboxes/"+id+"/desktop", "alice", ""); w.Code != http.StatusBadGateway {
		t.Fatalf("bridge failure status %d", w.Code)
	}
	if w := f.do(http.MethodPut, "/api/sandboxes/"+id+"/desktop/control", "alice", `{"holder":"agent"}`); w.Code != http.StatusBadGateway {
		t.Fatalf("bridge failure control %d", w.Code)
	}
}

func TestSandboxDesktopRejects(t *testing.T) {
	f := newDesktopFixture(t)
	stopped := utoa(f.sandbox(t, models.Sandbox{Purpose: "agent", Status: "stopped"}))
	running := utoa(f.sandbox(t, models.Sandbox{Purpose: "agent"}))
	cases := []struct {
		method, path, body string
		code               int
	}{
		{http.MethodGet, "/api/sandboxes/bad/desktop", "", http.StatusBadRequest},
		{http.MethodGet, "/api/sandboxes/999/desktop", "", http.StatusNotFound},
		{http.MethodGet, "/api/sandboxes/" + stopped + "/desktop", "", http.StatusConflict},
		{http.MethodPut, "/api/sandboxes/" + running + "/desktop/control", `{`, http.StatusBadRequest},
		{http.MethodPut, "/api/sandboxes/" + running + "/desktop/control", `{"holder":"root"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		if w := f.do(tc.method, tc.path, "alice", tc.body); w.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	if len(f.control) != 0 {
		t.Fatalf("rejected requests reached the sandbox: %+v", f.control)
	}

	nilSbx := &Handlers{}
	r := gin.New()
	r.GET("/d/:id", nilSbx.SandboxDesktopStatus)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/d/1", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil sbx %d", w.Code)
	}
}

func TestSandboxDesktopSoloOwnerOnly(t *testing.T) {
	f := newDesktopFixture(t)
	session := models.SoloSession{ID: "solo-desk", UserID: "alice", AgentName: "a", Status: models.SoloStatusRunning}
	if err := f.db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	id := utoa(f.sandbox(t, models.Sandbox{Purpose: services.SandboxPurposeSolo, ThreadID: "solo-desk"}))

	if w := f.do(http.MethodGet, "/api/sandboxes/"+id+"/desktop", "bob", ""); w.Code != http.StatusNotFound {
		t.Fatalf("other user status %d", w.Code)
	}
	if w := f.do(http.MethodPut, "/api/sandboxes/"+id+"/desktop/control", "bob", `{"holder":"user"}`); w.Code != http.StatusNotFound {
		t.Fatalf("other user control %d", w.Code)
	}
	if len(f.control) != 0 {
		t.Fatalf("non-owner reached the sandbox: %+v", f.control)
	}
	if w := f.do(http.MethodPut, "/api/sandboxes/"+id+"/desktop/control", "alice", `{"holder":"user"}`); w.Code != http.StatusOK {
		t.Fatalf("owner control %d %s", w.Code, w.Body.String())
	}
}

func TestDesktopClipboardWriterRequiresSoloOwner(t *testing.T) {
	f := newDesktopFixture(t)
	session := models.SoloSession{ID: "solo-clipboard", UserID: "alice", AgentName: "a", Status: models.SoloStatusRunning}
	if err := f.db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	id := f.sandbox(t, models.Sandbox{Purpose: services.SandboxPurposeSolo, ThreadID: session.ID})
	row, err := f.h.Sbx.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if f.h.desktopClipboardWriter(row, "bob") != nil {
		t.Fatal("non-owner received a VNC clipboard writer")
	}
	write := f.h.desktopClipboardWriter(row, "alice")
	if write == nil {
		t.Fatal("owner did not receive a VNC clipboard writer")
	}
	if err := write(t.Context(), "沙箱 🙂"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-f.clipboard:
		if got != "沙箱 🙂" {
			t.Fatalf("unexpected clipboard text %q", got)
		}
	default:
		t.Fatal("owner's write did not reach the sandbox")
	}
	if err := f.db.Delete(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := write(t.Context(), "expired ownership"); err == nil || len(f.clipboard) != 0 {
		t.Fatalf("writer must reject after the Solo session is removed, got %v", err)
	}
	if f.h.desktopClipboardWriter(&models.Sandbox{Purpose: services.SandboxPurposeSolo}, "alice") != nil {
		t.Fatal("Solo sandbox without a session received a clipboard writer")
	}
}
