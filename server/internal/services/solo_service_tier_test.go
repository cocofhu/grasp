package services

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func serviceTierFixture(t *testing.T) *SoloService {
	t.Helper()
	base, _, _, db := newSoloFixture(t, 0, 3)
	configuration := models.ModelConfiguration{ID: "sandbox-fixture", Backend: "codex", State: "active", Binding: modelrouting.Binding{
		Kind: "platform", CredentialID: "key", ModelID: "gpt-6.2-sol", AuxiliaryModelIDs: []string{"gpt-6.2-astra", "custom"},
	}}
	if err := db.Save(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	base.agents.SetModelConfigurationResolver(func(*modelrouting.Binding) (string, error) { return "codex", nil })
	agent, _ := base.agents.Get("agentA")
	agent.AcpBackend = "codex"
	if err := base.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	return NewSoloService(db, nil, base.agents, NewProjectService(db))
}

func TestSoloServiceTierPreservesEffortAndAppliesOnlyAtNextTurn(t *testing.T) {
	solo := serviceTierFixture(t)
	row := models.SoloSession{ID: "fast-settings", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning,
		CurrentConfigurationID: "sandbox-fixture", AppliedConfigurationID: "sandbox-fixture", CurrentModelSet: true, AppliedModelSet: true,
		CurrentReasoningEffort: "high", AppliedReasoningEffort: "high"}
	if err := solo.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	viewer, stop, err := solo.Subscribe(row.ID, "alice", -1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	<-viewer
	fast := "fast"
	view, err := solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &fast)
	if err != nil || view.CurrentServiceTier != "fast" || !slices.Equal(view.ServiceTiers, []string{"fast"}) || view.AppliedServiceTier != "" || view.CurrentReasoningEffort != "high" || view.CurrentModelID != "" {
		t.Fatalf("tier-only selection changed another setting or applied early: %+v %v", view, err)
	}
	event := <-viewer
	if event.Event != SoloEventModel || event.Payload["serviceTier"] != "fast" || !slices.Equal(event.Payload["serviceTiers"].([]string), []string{"fast"}) || event.Payload["pendingChange"] != "serviceTier" {
		t.Fatalf("tier change notification incomplete: %+v", event)
	}
	view, err = solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", effortPointer("gpt-6.2-astra"), effortPointer("medium"), nil)
	if err != nil || view.CurrentServiceTier != "fast" || view.CurrentReasoningEffort != "medium" {
		t.Fatalf("compatible model/effort change lost its tier: %+v %v", view, err)
	}
	view, err = solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", effortPointer("custom"), nil, nil)
	if err != nil || view.CurrentServiceTier != "" || len(view.ServiceTiers) != 0 || view.ServiceTiers == nil {
		t.Fatalf("unsupported model inherited the tier: %+v %v", view, err)
	}
	_, err = solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &fast)
	var inputErr *SoloError
	if !errors.As(err, &inputErr) || inputErr.Code != "service_tier_unavailable" {
		t.Fatalf("unsupported tier accepted: %v", err)
	}
	stored, _ := solo.Require(row.ID, "alice")
	if stored.CurrentModelID != "custom" || stored.CurrentServiceTier != "" {
		t.Fatalf("rejected tier wrote the selection: %+v", stored)
	}
}

func TestSoloServiceTierFollowsTheResponsesConnection(t *testing.T) {
	setCredentialKey(t)
	solo := serviceTierFixture(t)
	key := newModelKey(t, NewCredentialService(solo.db), "API key")
	connections := NewModelConnectionService(solo.db)
	connection, err := connections.Create(modelrouting.Connection{
		Name: "OpenAI", ProviderID: "openai", Protocol: modelrouting.ProtocolResponses, BaseURL: "https://api.openai.com/v1", CredentialID: key.ID,
		Models:            []modelrouting.Model{{ID: "gpt-6.2-sol"}, {ID: "custom"}},
		ModelServiceTiers: map[string][]string{"custom": {"fast"}},
	})
	if err != nil || !slices.Equal(connection.ModelServiceTiers["gpt-6.2-sol"], []string{"fast"}) || len(connection.ModelServiceTiers) != 1 {
		t.Fatalf("official API capability = %v, err = %v", connection.ModelServiceTiers, err)
	}
	catalog, err := connections.List()
	if err != nil || len(catalog) != 1 || !slices.Equal(catalog[0].ModelServiceTiers["gpt-6.2-sol"], []string{"fast"}) {
		t.Fatalf("connection list did not advertise Fast: %+v, %v", catalog, err)
	}
	var configuration models.ModelConfiguration
	if err := solo.db.First(&configuration, "id = ?", "sandbox-fixture").Error; err != nil {
		t.Fatal(err)
	}
	configuration.Binding = modelrouting.Binding{
		Kind: "connection", ConnectionID: connection.ID, ModelID: "gpt-6.2-sol", AuxiliaryModelIDs: []string{"custom"},
	}
	if err := solo.db.Save(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	row := models.SoloSession{ID: "api-fast-settings", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning,
		CurrentConfigurationID: configuration.ID, AppliedConfigurationID: configuration.ID, CurrentModelSet: true, AppliedModelSet: true,
		CurrentReasoningEffort: "high", AppliedReasoningEffort: "high"}
	if err := solo.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	fast := "fast"
	view, err := solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &fast)
	if err != nil || view.CurrentServiceTier != "fast" || !slices.Equal(view.ServiceTiers, []string{"fast"}) || view.AppliedServiceTier != "" || view.CurrentReasoningEffort != "high" {
		t.Fatalf("API Fast selection changed another setting or was rejected: %+v, %v", view, err)
	}
	connection.BaseURL = "https://compatible.example.test/v1"
	updated, err := connections.Update(connection.ID, connection)
	if err != nil || len(updated.ModelServiceTiers) != 0 {
		t.Fatalf("compatible route inherited Fast: %v, %v", updated.ModelServiceTiers, err)
	}
	_, err = solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &fast)
	var inputErr *SoloError
	if !errors.As(err, &inputErr) || inputErr.Code != "service_tier_unavailable" {
		t.Fatalf("compatible route accepted Fast: %v", err)
	}
}

type tierSettingsAligner struct {
	before func()
	err    error
	tier   string
}

func (a *tierSettingsAligner) AlignChatSettings(_ context.Context, _ uint, _, _, tier string) (ChatSwitch, error) {
	a.tier = tier
	if a.before != nil {
		a.before()
	}
	return ChatSwitch{Restarted: true}, a.err
}

func TestSoloServiceTierAppliesAndRollsBackWithoutClobberingANewerChoice(t *testing.T) {
	for _, outcome := range []string{"success", "rollback", "newer-choice"} {
		t.Run(outcome, func(t *testing.T) {
			solo := serviceTierFixture(t)
			row := models.SoloSession{ID: "fast-apply", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning,
				CurrentConfigurationID: "sandbox-fixture", AppliedConfigurationID: "sandbox-fixture", CurrentModelSet: true, AppliedModelSet: true,
				CurrentReasoningEffort: "high", AppliedReasoningEffort: "high", CurrentServiceTier: "fast"}
			if err := solo.db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			aligner := &tierSettingsAligner{}
			if outcome != "success" {
				aligner.err = errors.New("CLI rejected speed tier")
			}
			if outcome == "newer-choice" {
				aligner.before = func() {
					standard := ""
					if _, err := solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &standard); err != nil {
						t.Fatal(err)
					}
				}
			}
			solo.SetModelAlignerForTest(aligner)
			viewer, stop, subscribeErr := solo.Subscribe(row.ID, "alice", -1)
			if subscribeErr != nil {
				t.Fatal(subscribeErr)
			}
			defer stop()
			<-viewer
			changed, err := solo.alignSessionModel(context.Background(), solo.lookup(row.ID), 1, &row)
			if aligner.tier != "fast" || (err != nil) != (outcome != "success") {
				t.Fatalf("tier not applied independently: changed=%t requested=%q err=%v", changed, aligner.tier, err)
			}
			if outcome == "rollback" {
				select {
				case event := <-viewer:
					if event.Event != SoloEventModel || !slices.Equal(event.Payload["serviceTiers"].([]string), []string{"fast"}) || event.Payload["serviceTier"] != "" {
						t.Fatalf("rollback did not restore capability: %+v", event)
					}
				default:
					t.Fatal("missing rollback notification")
				}
			}
			stored, _ := solo.Require(row.ID, "alice")
			if stored.CurrentReasoningEffort != "high" || stored.AppliedReasoningEffort != "high" {
				t.Fatalf("tier changed reasoning: %+v", stored)
			}
			if outcome == "success" && (!changed || stored.AppliedServiceTier != "fast" || stored.CurrentServiceTier != "fast") {
				t.Fatalf("successful tier not remembered: %+v", stored)
			}
			if outcome != "success" && (stored.CurrentServiceTier != "" || stored.AppliedServiceTier != "") {
				t.Fatalf("failed tier was not rolled back: %+v", stored)
			}
			if outcome == "rollback" && stored.ModelNotice != soloModelSwitchFailed || outcome == "newer-choice" && stored.ModelNotice != "" {
				t.Fatalf("rollback clobbered a newer setting or lost notice: %+v", stored)
			}
		})
	}
}
