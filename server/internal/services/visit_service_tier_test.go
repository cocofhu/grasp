package services

import (
	"errors"
	"slices"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestVisitServiceTierPersistsIndependentlyAndRejectsCompatibleAccounts(t *testing.T) {
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
	fast, turbo := "fast", "turbo"
	high := "high"
	choice, err := SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, nil, nil, &high, &fast)
	if err != nil || choice.ServiceTier != "fast" || !slices.Equal(choice.ServiceTiers, []string{"fast"}) || choice.CurrentModelID != "" || choice.ReasoningEffort != high {
		t.Fatalf("tier-only primary selection: %+v %v", choice, err)
	}
	reloaded, err := GetVisitModel(db, conv.RunID, conv.NodeID, 1)
	if err != nil || reloaded.ServiceTier != "fast" || !slices.Equal(reloaded.ServiceTiers, []string{"fast"}) {
		t.Fatalf("tier not persisted: %+v %v", reloaded, err)
	}
	if _, err := SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, nil, nil, nil, &turbo); err == nil {
		t.Fatal("an undeclared tier was accepted")
	}
	astra := "gpt-6.2-astra"
	choice, err = SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, nil, &astra, nil, nil)
	if err != nil || choice.ServiceTier != "fast" || choice.ReasoningEffort != high {
		t.Fatalf("known model lost compatible tier/effort: %+v %v", choice, err)
	}
	custom := "custom"
	choice, err = SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, nil, &custom, effortPointer(""), nil)
	if err != nil || choice.ServiceTier != "" || len(choice.ServiceTiers) != 0 {
		t.Fatalf("unknown model inherited the tier: %+v %v", choice, err)
	}
	account, primary := "account", ""
	_, err = SetVisitModel(db, agent, conv.RunID, conv.NodeID, 1, &account, &primary, nil, &fast)
	var inputErr *SoloError
	if !errors.As(err, &inputErr) || inputErr.Code != "service_tier_unavailable" {
		t.Fatalf("API account silently accepted a platform tier: %v", err)
	}
	reloaded, err = GetVisitModel(db, conv.RunID, conv.NodeID, 1)
	if err != nil || reloaded.CurrentConfigurationID != "official" || reloaded.CurrentModelID != custom || reloaded.ServiceTier != "" {
		t.Fatalf("rejected request wrote the route: %+v %v", reloaded, err)
	}
}
