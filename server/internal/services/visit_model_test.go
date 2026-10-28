package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestVisitModelStaysOnTheVisitAndRejectsUnboundModels(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.ModelConfiguration{
		ID: "group-a", Name: "Composer", Backend: "cursor", State: "active",
		Binding: modelrouting.Binding{Kind: "platform", CredentialID: "key", ModelID: "composer", AuxiliaryModelIDs: []string{"composer-fast"}},
	})
	db.Create(&models.ModelConfiguration{
		ID: "group-b", Name: "Other", Backend: "opencode", State: "active",
		Binding: modelrouting.Binding{Kind: "platform", CredentialID: "key", ModelID: "other"},
	})
	db.Create(&models.ModelConfiguration{
		ID: "group-off", Name: "Off", Backend: "cursor", State: "disabled",
		Binding: modelrouting.Binding{Kind: "platform", CredentialID: "key", ModelID: "gone"},
	})
	agent := Agent{
		Name: "澄清", AcpBackend: "cursor",
		ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "group-a", ConfigurationIDs: []string{"group-a", "group-b", "group-off"}},
	}
	node := models.Node{ID: "clarify", Type: "agent", Config: map[string]any{"agent_profile": "澄清"}}
	db.Create(&models.Run{ID: "run-1", Status: "waiting_human", Graph: models.Graph{Nodes: []models.Node{node}}})
	oldExecution := &models.ChatExecution{AgentName: "澄清", Backend: "cursor", ModelID: "composer"}
	db.Create(&models.ReactConversation{
		RunID: "run-1", NodeID: "clarify", Iteration: 1,
		Messages: []models.ReactMessage{{Role: "agent", Text: "earlier", Execution: oldExecution}},
	})
	db.Create(&models.ReactConversation{RunID: "run-1", NodeID: "clarify", Iteration: 2})

	primary := ""
	choice, err := SetVisitModel(db, agent, "run-1", "clarify", 1, &primary, &primary, &primary)
	if err != nil || !choice.CurrentModelSet || choice.CurrentModelID != "" || choice.CurrentConfigurationID != "group-a" {
		t.Fatalf("selecting the default primary: %+v %v", choice, err)
	}
	fast := "composer-fast"
	choice, err = SetVisitModel(db, agent, "run-1", "clarify", 1, nil, &fast, nil)
	if err != nil || choice.CurrentModelID != "composer-fast" || choice.CurrentConfigurationID != "group-a" {
		t.Fatalf("auxiliary: %+v %v", choice, err)
	}
	composer := "composer"
	choice, err = SetVisitModel(db, agent, "run-1", "clarify", 1, nil, &composer, nil)
	if err != nil || choice.CurrentModelID != "" || !choice.CurrentModelSet {
		t.Fatalf("selecting the primary id stores the default: %+v %v", choice, err)
	}
	otherGroup := "group-b"
	choice, err = SetVisitModel(db, agent, "run-1", "clarify", 1, &otherGroup, &primary, &primary)
	if err != nil || choice.CurrentConfigurationID != "group-b" || choice.CurrentModelID != "" || choice.Backend != "opencode" {
		t.Fatalf("other group: %+v %v", choice, err)
	}

	before := choice
	missing := "not-bound"
	if _, err = SetVisitModel(db, agent, "run-1", "clarify", 1, nil, &missing, nil); err == nil {
		t.Fatal("unbound model was accepted")
	}
	off := "group-off"
	if _, err = SetVisitModel(db, agent, "run-1", "clarify", 1, &off, &primary, nil); err == nil {
		t.Fatal("disabled group was accepted")
	}
	reloaded, err := GetVisitModel(db, "run-1", "clarify", 1)
	if err != nil || reloaded.CurrentConfigurationID != before.CurrentConfigurationID || reloaded.CurrentModelID != before.CurrentModelID {
		t.Fatalf("rejected switch changed the stored choice: %+v %v", reloaded, err)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND iteration = ?", "run-1", 1).First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	if conv.Messages[0].Execution == nil || conv.Messages[0].Execution.ModelID != "composer" {
		t.Fatalf("completed reply was rewritten: %+v", conv.Messages[0].Execution)
	}
	if conv.AppliedModelSet {
		t.Fatal("saving a choice aligned the sandbox")
	}
	next, err := GetVisitModel(db, "run-1", "clarify", 2)
	if err != nil || next.CurrentModelSet || next.CurrentModelID != "" {
		t.Fatalf("next visit inherited the choice: %+v %v", next, err)
	}
}

func TestVisitModelRejectsUnavailableEffort(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.ModelConnection{
		ID: "conn", State: "active", ProviderID: "openai", Protocol: modelrouting.ProtocolChat,
		Models: []modelrouting.Model{{ID: "m1", ReasoningEfforts: []string{"low", "high"}}},
	})
	db.Create(&models.ModelConfiguration{
		ID: "group", Name: "Codex", Backend: "codex", State: "active",
		Binding: modelrouting.Binding{Kind: "connection", ConnectionID: "conn", ModelID: "m1"},
	})
	agent := Agent{Name: "澄清", AcpBackend: "codex", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "group"}}
	db.Create(&models.Run{ID: "run-e", Status: "waiting_human", Graph: models.Graph{Nodes: []models.Node{{ID: "clarify", Type: "agent"}}}})
	db.Create(&models.ReactConversation{RunID: "run-e", NodeID: "clarify", Iteration: 1})
	empty := ""
	high := "high"
	choice, err := SetVisitModel(db, agent, "run-e", "clarify", 1, &empty, &empty, &high)
	if err != nil || choice.ReasoningEffort != "high" || choice.CurrentModelID != "" {
		t.Fatalf("effort: %+v %v", choice, err)
	}
	nope := "ultra"
	if _, err = SetVisitModel(db, agent, "run-e", "clarify", 1, nil, nil, &nope); err == nil {
		t.Fatal("unknown effort was accepted")
	}
	reloaded, err := GetVisitModel(db, "run-e", "clarify", 1)
	if err != nil || reloaded.ReasoningEffort != "high" {
		t.Fatalf("failed effort overwrote the choice: %+v %v", reloaded, err)
	}
	if err := db.Model(&models.ReactConversation{}).Where("run_id = ?", "run-e").Update("done", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = SetVisitModel(db, agent, "run-e", "clarify", 1, nil, nil, &empty); err == nil {
		t.Fatal("ended visit accepted a switch")
	}
}
