package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func assertPmThreadAvailability(t *testing.T, thread map[string]any, agent string, want bool) {
	t.Helper()
	available, present := thread["agentAvailable"].(bool)
	if !present || available != want || thread["agentName"] != agent {
		t.Fatalf("thread Agent contract: %+v, want %s available=%v", thread, agent, want)
	}
	if !want {
		if errorText, ok := thread["agentError"].(string); !ok || errorText == "" {
			t.Fatalf("missing Agent error: %+v", thread)
		}
	}
}

func TestPmThreadResponsesExposeOriginalAgentAvailability(t *testing.T) {
	h, workspaceID, _ := setupPmEnabledHarness(t)
	created := h.do(http.MethodPost, "/api/workspace/pm/threads", map[string]any{"title": "Original Agent conversation"})
	if created.Code != http.StatusOK {
		t.Fatal(created.Body.String())
	}
	var thread map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &thread); err != nil {
		t.Fatal(err)
	}
	assertPmThreadAvailability(t, thread, "pm-agent", true)
	threadID := thread["id"].(string)
	base := "/api/workspace/pm/threads/" + threadID
	if err := h.h.Agents.Save(services.Agent{Name: "new-default", ProjectID: workspaceID, AcpBackend: services.AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	if response := h.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{"agentConfigRef": "new-default"}); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	// Configuration loss does not perform the explicit DELETE Agent API's purge.
	if err := h.h.Agents.Delete("new-default"); err != nil {
		t.Fatal(err)
	}
	binding := h.do(http.MethodGet, "/api/workspace/pm-leader", nil)
	var bindingDTO map[string]any
	if err := json.Unmarshal(binding.Body.Bytes(), &bindingDTO); err != nil {
		t.Fatal(err)
	}
	if bindingDTO["agentAvailable"] != false {
		t.Fatal("default Agent unexpectedly available", bindingDTO)
	}
	get := h.do(http.MethodGet, base, nil)
	if get.Code != http.StatusOK {
		t.Fatal(get.Body.String())
	}
	if err := json.Unmarshal(get.Body.Bytes(), &thread); err != nil {
		t.Fatal(err)
	}
	assertPmThreadAvailability(t, thread, "pm-agent", true)
	list := h.do(http.MethodGet, "/api/workspace/pm/threads", nil)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("list=%s %v", list.Body, err)
	}
	assertPmThreadAvailability(t, page.Items[0], "pm-agent", true)
	if response := h.do(http.MethodPost, base+"/messages", map[string]any{"role": "user", "content": "Continue original conversation"}); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	opened := h.do(http.MethodPost, base+"/sandbox", nil)
	if opened.Code != http.StatusOK {
		t.Fatalf("missing default blocked original Agent: %d %s", opened.Code, opened.Body)
	}
	var sandboxResponse struct {
		Thread  map[string]any `json:"thread"`
		Sandbox struct {
			Profile string `json:"profile"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &sandboxResponse); err != nil {
		t.Fatal(err)
	}
	assertPmThreadAvailability(t, sandboxResponse.Thread, "pm-agent", true)
	if sandboxResponse.Sandbox.Profile != "pm-agent" {
		t.Fatalf("conversation switched Agent: %+v", sandboxResponse)
	}
	if response := h.do(http.MethodPost, "/api/workspace/pm/threads", nil); response.Code != http.StatusConflict {
		t.Fatalf("new thread ignored missing default: %d %s", response.Code, response.Body)
	}
	if response := h.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{"enabled": false}); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if response := h.do(http.MethodPost, base+"/messages", map[string]any{"role": "user", "content": "disabled"}); response.Code != http.StatusConflict {
		t.Fatalf("send switch bypassed: %d %s", response.Code, response.Body)
	}
}

func TestPmThreadResponsesRetainHistoryWhenOriginalAgentIsUnavailable(t *testing.T) {
	h, workspaceID, _ := setupPmEnabledHarness(t)
	thread, err := h.h.Pm.CreateThread(workspaceID, "admin", "Retained history", "missing-original", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.h.Pm.AppendMessage(thread.ID, "assistant", "Retained reply", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	base := "/api/workspace/pm/threads/" + thread.ID
	get := h.do(http.MethodGet, base, nil)
	if get.Code != http.StatusOK {
		t.Fatalf("unavailable Agent hid history: %d %s", get.Code, get.Body)
	}
	var threadDTO map[string]any
	if err = json.Unmarshal(get.Body.Bytes(), &threadDTO); err != nil {
		t.Fatal(err)
	}
	assertPmThreadAvailability(t, threadDTO, "missing-original", false)
	list := h.do(http.MethodGet, "/api/workspace/pm/threads", nil)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err = json.Unmarshal(list.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("list=%s %v", list.Body, err)
	}
	assertPmThreadAvailability(t, page.Items[0], "missing-original", false)
	messages := h.do(http.MethodGet, base+"/messages", nil)
	var history struct {
		Items []models.ChatMessage `json:"items"`
	}
	if err = json.Unmarshal(messages.Body.Bytes(), &history); err != nil || len(history.Items) != 1 || history.Items[0].Content != "Retained reply" {
		t.Fatalf("history=%s %v", messages.Body, err)
	}
	if response := h.do(http.MethodPost, base+"/sandbox", nil); response.Code == http.StatusOK {
		t.Fatal("missing original Agent fell back to current default")
	}
}
