package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloFastOnlyEndpointPreservesModelAndReasoning(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	configuration := models.ModelConfiguration{ID: "fast-codex", Name: "Fast model", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "platform", ModelID: "gpt-6.2-sol", AuxiliaryModelIDs: []string{"custom"}}}
	if err := hn.db.Create(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.h.Agents.Save(services.Agent{Name: "Solo", AcpBackend: "codex", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}); err != nil {
		t.Fatal(err)
	}
	row := models.SoloSession{ID: "solo-fast", UserID: "admin", AgentName: "Solo", Status: models.SoloStatusRunning, CurrentConfigurationID: configuration.ID, CurrentModelSet: true, CurrentReasoningEffort: "high"}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   map[string]any
		fast   bool
		effort string
	}{
		{map[string]any{"fastMode": true}, true, "high"},
		{map[string]any{"reasoningEffort": "ultra"}, true, "ultra"},
		{map[string]any{"fastMode": false}, false, "ultra"},
	} {
		response := hn.do(http.MethodPost, "/api/solo/sessions/solo-fast/model", tc.body)
		if response.Code != http.StatusOK {
			t.Fatalf("settings %v: %d %s", tc.body, response.Code, response.Body)
		}
		var view services.SoloSessionView
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.CurrentModelID != "" || view.CurrentFastMode != tc.fast || !view.FastModeSupported || view.CurrentReasoningEffort != tc.effort {
			t.Fatalf("settings changed an independent value: %+v", view)
		}
	}
	response := hn.do(http.MethodPost, "/api/solo/sessions/solo-fast/model", map[string]any{"modelId": "custom", "reasoningEffort": "", "fastMode": true})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported Fast: %d %s", response.Code, response.Body)
	}
	var failure map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || failure["code"] != "fast_mode_unavailable" {
		t.Fatalf("error code: %+v %v", failure, err)
	}
}
