package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloModelEndpointPreservesOmittedModelForEffortOnly(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	configuration := models.ModelConfiguration{ID: "solo-codex", Name: "Solo model", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "platform", ModelID: "gpt-6.1-sol", AuxiliaryModelIDs: []string{"gpt-6-astra"}}}
	if err := hn.db.Create(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.h.Agents.Save(services.Agent{Name: "Solo", AcpBackend: "codex", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}); err != nil {
		t.Fatal(err)
	}
	row := &models.SoloSession{ID: "solo-effort", UserID: "admin", AgentName: "Solo", Status: models.SoloStatusRunning, CurrentModelSet: true, CurrentModelID: "gpt-6-astra", CurrentReasoningEffort: "high"}
	if err := hn.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	response := hn.do(http.MethodPost, "/api/solo/sessions/solo-effort/model", map[string]string{"reasoningEffort": "ultra"})
	if response.Code != http.StatusOK {
		t.Fatalf("effort-only: %d %s", response.Code, response.Body)
	}
	var view services.SoloSessionView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.CurrentModelID != "gpt-6-astra" || view.CurrentReasoningEffort != "ultra" || len(view.ReasoningEfforts) == 0 {
		t.Fatalf("effort-only changed model or lost options: %+v", view)
	}
	response = hn.do(http.MethodPost, "/api/solo/sessions/solo-effort/model", map[string]string{"reasoningEffort": "invented"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported effort: %d %s", response.Code, response.Body)
	}
	var failure map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure["code"] != "reasoning_effort_unavailable" {
		t.Fatalf("error code: %+v", failure)
	}
	response = hn.do(http.MethodPost, "/api/solo/sessions/solo-effort/model", map[string]string{"reasoningEffort": ""})
	if response.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", response.Code, response.Body)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.CurrentModelID != "gpt-6-astra" || view.CurrentReasoningEffort != "" {
		t.Fatalf("reset changed model: %+v", view)
	}
}
