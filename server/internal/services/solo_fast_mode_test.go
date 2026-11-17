package services

import (
	"context"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func fastModeFixture(t *testing.T) *SoloService {
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

func TestSoloFastModePreservesEffortAndAppliesOnlyAtNextTurn(t *testing.T) {
	solo := fastModeFixture(t)
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
	on := true
	view, err := solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &on)
	if err != nil || !view.CurrentFastMode || !view.FastModeSupported || view.AppliedFastMode || view.CurrentReasoningEffort != "high" || view.CurrentModelID != "" {
		t.Fatalf("Fast-only selection changed another setting or applied early: %+v %v", view, err)
	}
	event := <-viewer
	if event.Event != SoloEventModel || event.Payload["fastMode"] != true || event.Payload["fastModeSupported"] != true || event.Payload["pendingChange"] != "fast" {
		t.Fatalf("Fast change notification incomplete: %+v", event)
	}
	view, err = solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", effortPointer("gpt-6.2-astra"), effortPointer("medium"))
	if err != nil || !view.CurrentFastMode || view.CurrentReasoningEffort != "medium" {
		t.Fatalf("compatible model/effort change lost Fast: %+v %v", view, err)
	}
	view, err = solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", effortPointer("custom"), nil)
	if err != nil || view.CurrentFastMode || view.FastModeSupported {
		t.Fatalf("unsupported model inherited Fast: %+v %v", view, err)
	}
	_, err = solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &on)
	var inputErr *SoloError
	if !errors.As(err, &inputErr) || inputErr.Code != "fast_mode_unavailable" {
		t.Fatalf("unsupported Fast accepted: %v", err)
	}
	stored, _ := solo.Require(row.ID, "alice")
	if stored.CurrentModelID != "custom" || stored.CurrentFastMode {
		t.Fatalf("rejected Fast wrote the selection: %+v", stored)
	}
}

type fastSettingsAligner struct {
	before func()
	err    error
	fast   bool
}

func (*fastSettingsAligner) AlignChatModel(context.Context, uint, string) (bool, error) {
	return false, errors.New("model-only align must not handle Fast")
}

func (a *fastSettingsAligner) AlignChatSettings(_ context.Context, _ uint, _, _ string, fastMode ...bool) (bool, error) {
	a.fast = len(fastMode) > 0 && fastMode[0]
	if a.before != nil {
		a.before()
	}
	return true, a.err
}

func TestSoloFastModeAppliesAndRollsBackWithoutClobberingANewerChoice(t *testing.T) {
	for _, outcome := range []string{"success", "rollback", "newer-choice"} {
		t.Run(outcome, func(t *testing.T) {
			solo := fastModeFixture(t)
			row := models.SoloSession{ID: "fast-apply", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning,
				CurrentConfigurationID: "sandbox-fixture", AppliedConfigurationID: "sandbox-fixture", CurrentModelSet: true, AppliedModelSet: true,
				CurrentReasoningEffort: "high", AppliedReasoningEffort: "high", CurrentFastMode: true}
			if err := solo.db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			aligner := &fastSettingsAligner{}
			if outcome != "success" {
				aligner.err = errors.New("CLI rejected speed tier")
			}
			if outcome == "newer-choice" {
				aligner.before = func() {
					off := false
					if _, err := solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", nil, nil, &off); err != nil {
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
			if !aligner.fast || (err != nil) != (outcome != "success") {
				t.Fatalf("Fast not applied independently: changed=%t requested=%t err=%v", changed, aligner.fast, err)
			}
			if outcome == "rollback" {
				select {
				case event := <-viewer:
					if event.Event != SoloEventModel || event.Payload["fastModeSupported"] != true || event.Payload["fastMode"] != false {
						t.Fatalf("rollback did not restore capability: %+v", event)
					}
				default:
					t.Fatal("missing rollback notification")
				}
			}
			stored, _ := solo.Require(row.ID, "alice")
			if stored.CurrentReasoningEffort != "high" || stored.AppliedReasoningEffort != "high" {
				t.Fatalf("Fast changed reasoning: %+v", stored)
			}
			if outcome == "success" && (!changed || !stored.AppliedFastMode || !stored.CurrentFastMode) {
				t.Fatalf("successful Fast not remembered: %+v", stored)
			}
			if outcome != "success" && (stored.CurrentFastMode || stored.AppliedFastMode) {
				t.Fatalf("failed Fast was not rolled back: %+v", stored)
			}
			if outcome == "rollback" && stored.ModelNotice != soloModelSwitchFailed || outcome == "newer-choice" && stored.ModelNotice != "" {
				t.Fatalf("rollback clobbered a newer setting or lost notice: %+v", stored)
			}
		})
	}
}
