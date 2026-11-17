package services

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestVisitFastModePersistsIndependentlyAndRejectsCompatibleAccounts(t *testing.T) {
	db := newTestDB(t)
	for _, config := range []models.ModelConfiguration{
		{ID: "official", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "key", ModelID: "gpt-6.2-sol", AuxiliaryModelIDs: []string{"gpt-6.2-astra", "custom"}}},
		{ID: "account", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: "responses", ModelID: "gpt-6.2-sol"}},
	} {
		if err := db.Create(&config).Error; err != nil {
			t.Fatal(err)
		}
	}
	agent := Agent{Name: "planner", AcpBackend: "codex", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "official", ConfigurationIDs: []string{"official", "account"}}}
	conv := models.ReactConversation{RunID: "fast-run", NodeID: "clarify", Iteration: 1}
	if err := db.Create(&conv).Error; err != nil {
		t.Fatal(err)
	}
	on := true
	high := "high"
	choice, err := SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, nil, nil, &high, &on)
	if err != nil || !choice.FastMode || !choice.FastModeSupported || choice.CurrentModelID != "" || choice.ReasoningEffort != high {
		t.Fatalf("Fast-only primary selection: %+v %v", choice, err)
	}
	reloaded, err := GetVisitModel(db, conv.RunID, conv.NodeID, 1)
	if err != nil || !reloaded.FastMode || !reloaded.FastModeSupported {
		t.Fatalf("Fast not persisted: %+v %v", reloaded, err)
	}
	astra := "gpt-6.2-astra"
	choice, err = SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, nil, &astra, nil)
	if err != nil || !choice.FastMode || choice.ReasoningEffort != high {
		t.Fatalf("known model lost compatible Fast/effort: %+v %v", choice, err)
	}
	custom := "custom"
	choice, err = SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, nil, &custom, effortPointer(""))
	if err != nil || choice.FastMode || choice.FastModeSupported {
		t.Fatalf("unknown model inherited Fast: %+v %v", choice, err)
	}
	account, primary := "account", ""
	_, err = SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, &account, &primary, nil, &on)
	var inputErr *SoloError
	if !errors.As(err, &inputErr) || inputErr.Code != "fast_mode_unavailable" {
		t.Fatalf("API account silently accepted platform Fast: %v", err)
	}
	reloaded, err = GetVisitModel(db, conv.RunID, conv.NodeID, 1)
	if err != nil || reloaded.CurrentConfigurationID != "official" || reloaded.CurrentModelID != custom || reloaded.FastMode {
		t.Fatalf("rejected request wrote the route: %+v %v", reloaded, err)
	}
}
