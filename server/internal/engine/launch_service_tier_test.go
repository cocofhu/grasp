package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestTierOnlyLaunchUsesPrimaryAndCarriesSpeedToVisit(t *testing.T) {
	graph := launchModelGraph()
	eng, db, _ := setupEngineGraphP(t, graph)
	seedLaunchModels(t, db)
	if err := db.Save(&models.ModelConfiguration{ID: "group", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "login", ModelID: "gpt-6.2-sol"}}).Error; err != nil {
		t.Fatal(err)
	}
	eng.SetAgents(launchAgents())
	if _, err := eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true, ServiceTier: "turbo"}); err == nil {
		t.Fatal("an undeclared launch tier was accepted")
	}
	choice, err := eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true, ServiceTier: "fast"})
	if err != nil || choice.ServiceTier != "fast" || choice.ModelID != "" || choice.ConfigurationID != "group" || choice.ReasoningEffort != "" {
		t.Fatalf("tier-only launch changed model or effort: %+v %v", choice, err)
	}
	run := models.Run{Graph: graph, LaunchModelSet: choice.Set, LaunchConfigurationID: choice.ConfigurationID, LaunchModelID: choice.ModelID, LaunchServiceTier: choice.ServiceTier}
	conv := models.ReactConversation{}
	eng.applyVisitLaunchModel(&conv, &run, graph.FindNode("predev"), 1, nil)
	if !conv.CurrentModelSet || conv.CurrentServiceTier != "fast" || conv.CurrentModelID != "" || conv.CurrentReasoningEffort != "" {
		t.Fatalf("opening visit lost independent speed selection: %+v", conv)
	}
}
