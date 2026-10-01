package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

func newChatsEngine(chats *service.ChatManager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", WebSocket(chats))
	r.GET("/api/chats", ChatsList(chats))
	r.POST("/api/chats", ChatsCreate(chats))
	r.PATCH("/api/chats/:id", ChatsRename(chats))
	r.DELETE("/api/chats/:id", ChatsDelete(chats))
	r.GET("/api/events", EventsBefore(chats))
	r.GET("/api/prompt_queue", PromptQueue(chats))
	r.GET("/api/models", ModelsGET(chats))
	r.POST("/api/model", ModelPOST(chats))
	return r
}

func do(t *testing.T, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestChatsCRUD(t *testing.T) {
	t.Setenv("SANDBOX_MAX_CHATS", "2")
	chats := service.NewChatManager()
	chats.SetDefaultModel("env-model")
	r := newChatsEngine(chats)

	w := do(t, r, http.MethodGet, "/api/chats", "")
	var list struct {
		Chats        []service.ChatInfo `json:"chats"`
		Max          int                `json:"max"`
		DefaultModel string             `json:"defaultModel"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != http.StatusOK {
		t.Fatalf("list code=%d err=%v body=%s", w.Code, err, w.Body)
	}
	if len(list.Chats) != 1 || list.Chats[0].ID != service.DefaultChatID || list.Max != 2 || list.DefaultModel != "env-model" {
		t.Fatalf("list = %+v", list)
	}

	w = do(t, r, http.MethodPost, "/api/chats", `{"title":"tab 2","model":"m2"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", w.Code, w.Body)
	}
	var created service.ChatInfo
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" || created.Title != "tab 2" || created.Model != "m2" {
		t.Fatalf("created = %+v", created)
	}

	if w = do(t, r, http.MethodPost, "/api/chats", ""); w.Code != http.StatusConflict {
		t.Fatalf("over cap code=%d", w.Code)
	}
	if w = do(t, r, http.MethodPost, "/api/chats", `{bad`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json code=%d", w.Code)
	}

	w = do(t, r, http.MethodPatch, "/api/chats/"+created.ID, `{"title":"renamed"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"renamed"`) {
		t.Fatalf("rename code=%d body=%s", w.Code, w.Body)
	}
	if w = do(t, r, http.MethodPatch, "/api/chats/nope", `{"title":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("rename unknown code=%d", w.Code)
	}
	if w = do(t, r, http.MethodPatch, "/api/chats/"+created.ID, `{bad`); w.Code != http.StatusBadRequest {
		t.Fatalf("rename bad json code=%d", w.Code)
	}

	if w = do(t, r, http.MethodDelete, "/api/chats/default", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("delete default code=%d", w.Code)
	}
	if w = do(t, r, http.MethodDelete, "/api/chats/"+created.ID, ""); w.Code != http.StatusOK {
		t.Fatalf("delete code=%d", w.Code)
	}
	if w = do(t, r, http.MethodDelete, "/api/chats/"+created.ID, ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete again code=%d", w.Code)
	}

	// Chunked request with an empty body (ContentLength -1) means "no options".
	req := httptest.NewRequest(http.MethodPost, "/api/chats", strings.NewReader(""))
	req.ContentLength = -1
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("empty chunked create code=%d body=%s", w.Code, w.Body)
	}
}

func TestChatQueryRouting(t *testing.T) {
	chats := service.NewChatManager()
	tab, err := chats.Create("", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newChatsEngine(chats)

	for _, path := range []string{
		"/api/events",
		"/api/events?chat=default",
		"/api/events?chat=" + tab.ID(),
		"/api/prompt_queue",
		"/api/prompt_queue?chat=" + tab.ID(),
	} {
		if w := do(t, r, http.MethodGet, path, ""); w.Code != http.StatusOK {
			t.Fatalf("%s code=%d body=%s", path, w.Code, w.Body)
		}
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/events?chat=nope"},
		{http.MethodGet, "/api/prompt_queue?chat=nope"},
		{http.MethodGet, "/api/models?chat=nope"},
		{http.MethodPost, "/api/model?chat=nope"},
		{http.MethodGet, "/ws?chat=nope"},
	} {
		if w := do(t, r, tc.method, tc.path, `{"model":""}`); w.Code != http.StatusNotFound {
			t.Fatalf("%s %s code=%d", tc.method, tc.path, w.Code)
		}
	}
	if w := do(t, r, http.MethodPost, "/api/model?chat="+tab.ID(), `{bad`); w.Code != http.StatusBadRequest {
		t.Fatalf("model bad json code=%d", w.Code)
	}
}
