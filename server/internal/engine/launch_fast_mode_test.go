package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestFastOnlyLaunchUsesPrimaryAndCarriesSpeedToVisit(t *testing.T) {
	graph := launchModelGraph()
	eng, db, _ := setupEngineGraphP(t, graph)
	seedLaunchModels(t, db)
	if err := db.Save(&models.ModelConfiguration{ID: "group", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "login", ModelID: "gpt-6.2-sol"}}).Error; err != nil {
		t.Fatal(err)
	}
	eng.SetAgents(launchAgents())
	choice, err := eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true, FastMode: true})
	if err != nil || !choice.FastMode || choice.ModelID != "" || choice.ConfigurationID != "group" || choice.ReasoningEffort != "" {
		t.Fatalf("Fast-only launch changed model or effort: %+v %v", choice, err)
	}
	run := models.Run{Graph: graph, LaunchModelSet: choice.Set, LaunchConfigurationID: choice.ConfigurationID, LaunchModelID: choice.ModelID, LaunchFastMode: choice.FastMode}
	conv := models.ReactConversation{}
	eng.applyVisitLaunchModel(&conv, &run, graph.FindNode("predev"), 1, nil)
	if !conv.CurrentModelSet || !conv.CurrentFastMode || conv.CurrentModelID != "" || conv.CurrentReasoningEffort != "" {
		t.Fatalf("opening visit lost independent Fast selection: %+v", conv)
	}
}
