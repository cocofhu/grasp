package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestLaunchModelUsesLiveInheritedConfigurations(t *testing.T) {
	graph := launchModelGraph()
	eng, db, _ := setupEngineGraphP(t, graph)
	seedLaunchModels(t, db)
	shared := services.NewSharedAgentService(t.TempDir())
	shared.SetBindingResolvers(func(*modelrouting.Binding) (string, error) { return "codex", nil }, nil)
	if err := shared.Save(services.SharedAgentConfig{ModelBinding: &modelrouting.Binding{
		Kind: "configuration", ConfigurationID: "group", AllConfigurations: true,
	}}); err != nil {
		t.Fatal(err)
	}
	agents := services.NewAgentService(t.TempDir())
	agents.SetSharedAgent(shared)
	// The local binding is absent: the launch must resolve shared defaults.
	if err := agents.Save(services.Agent{Name: "pm", AcpBackend: "codex", InheritModel: true}); err != nil {
		t.Fatal(err)
	}
	eng.SetAgents(agents)
	// A group configured after the Agent was saved becomes selectable.
	if err := db.Create(&models.ModelConfiguration{
		ID: "later", Name: "Later", Backend: "codex", State: "active",
		Binding: modelrouting.Binding{Kind: "connection", ConnectionID: "conn", ModelID: "m1"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	choice, err := eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true, ConfigurationID: "later", ModelID: "m1"})
	if err != nil || choice.ConfigurationID != "later" || choice.ModelID != "" {
		t.Fatalf("inherited later group rejected: %+v %v", choice, err)
	}
	choice, err = eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true})
	if err != nil || choice.ConfigurationID != "group" {
		t.Fatalf("all groups changed the explicit default: %+v %v", choice, err)
	}
	stored, ok := agents.Get("pm")
	if !ok || stored.ModelBinding != nil || !stored.InheritModel {
		t.Fatal("launch changed the saved Agent's inheritance")
	}
	if err := db.Model(&models.ModelConfiguration{}).Where("id = ?", "later").Update("state", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"later", "missing"} {
		if _, err := eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true, ConfigurationID: id}); err == nil {
			t.Fatalf("inherited all groups accepted unavailable group %q", id)
		}
	}
	// Turning off model inheritance keeps this Agent's local allow-list.
	if err := agents.Save(services.Agent{Name: "pm", AcpBackend: "codex", ModelBinding: modelrouting.ConfigurationReference("group")}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ModelConfiguration{}).Where("id = ?", "later").Update("state", "active").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true, ConfigurationID: "later"}); err == nil {
		t.Fatal("workspace all groups expanded a non-inheriting Agent")
	}
	choice, err = eng.normalizeLaunchModel(&graph, LaunchChoice{Set: true})
	if err != nil || choice.ConfigurationID != "group" {
		t.Fatalf("local default changed: %+v %v", choice, err)
	}
}
