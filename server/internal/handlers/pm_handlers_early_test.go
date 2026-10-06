package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func TestPmThreadChatEarlyFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hn, pid, _ := setupPmEnabledHarness(t)

	hn.h.PmTurns = nil
	w := hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/any/chat", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil turns: %d %s", w.Code, w.Body.String())
	}

	hn.h.PmTurns = services.NewPmTurnRunner(hn.h.Pm, hn.h.Sbx)
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/missing/chat", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing thread: %d %s", w.Code, w.Body.String())
	}
}

func TestPmMessagesHandlersBranches(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)
	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "msgs"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	tid := jsonField(w.Body.String(), "id")

	hn.h.Pm = nil
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("list nil pm: %d", w.Code)
	}
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", map[string]any{"content": "x"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("append nil pm: %d", w.Code)
	}
}

func TestPmHandlersRequireAuthWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/pm-auth.db")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Users: []config.AuthUser{
				{Username: "admin", PasswordHash: "$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi"},
			},
			MaxFailures: 100, LockDuration: "1m", SessionTTL: "168h",
		},
	}
	config.StoreConfig(cfg)
	authSvc := auth.NewService(db, config.GetConfig)
	pm := services.NewPmService(db, nil)
	h := &handlers.Handlers{Pm: pm, Auth: authSvc}
	r := gin.New()
	r.GET("/api/projects/:id/pm/threads/:tid/messages", h.ListPmMessages)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/projects/p1/pm/threads/t1/messages", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list messages: %d", w.Code)
	}
}

func TestAppendPmMessageInvalidJSON(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)
	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "bad-json"})
	tid := jsonField(w.Body.String(), "id")
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", "not-json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d %s", w.Code, w.Body.String())
	}
}
