package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func TestUnconfiguredAgentDraftDoesNotDeriveModelBinding(t *testing.T) {
	agents := services.NewAgentService(t.TempDir())
	if err := agents.Save(services.Agent{Name: "draft", AcpBackend: "cursor", Env: map[string]string{"PROJECT": "demo"}}); err != nil {
		t.Fatal(err)
	}
	validated := false
	agents.SetModelBindingResolver(func(_ string, binding *modelrouting.Binding) error {
		validated = true
		return modelrouting.ValidateConfigurationReference(binding)
	})
	h := &handlers.Handlers{Agents: agents}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/agents/:name", h.GetAgent)
	router.PUT("/agents/:name", h.SaveAgent)
	call := func(method string, body []byte) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/agents/draft", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		return response
	}
	response := call("GET", nil)
	if response.Code != 200 || bytes.Contains(response.Body.Bytes(), []byte(`"modelBinding"`)) {
		t.Fatalf("draft read: %d %s", response.Code, response.Body)
	}
	var draft services.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	draft.Files = []services.AgentFile{{Path: "notes.md", Content: "Draft work"}}
	body, _ := json.Marshal(draft)
	if response = call("PUT", body); response.Code != 200 {
		t.Fatalf("save draft: %d %s", response.Code, response.Body)
	}
	if validated {
		t.Fatal("draft invoked model resolver")
	}
	saved, _ := agents.Get("draft")
	if saved.ModelBinding != nil {
		t.Fatal("draft acquired an implicit model binding")
	}
	draft.ModelBinding = &modelrouting.Binding{Kind: "platform", CredentialID: "old-key"}
	body, _ = json.Marshal(draft)
	if response = call("PUT", body); response.Code != 400 {
		t.Fatalf("direct binding accepted: %d %s", response.Code, response.Body)
	}
}
