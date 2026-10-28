package services

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestSoloRouteSettingsNotifyViewersWithoutApplyingTheNextTurn(t *testing.T) {
	base, _, _, db := newSoloFixture(t, 0, 3)
	insertSoloModelConfig(t, db)
	connection := models.ModelConnection{ID: "notification-account", State: "active", Models: []modelrouting.Model{
		{ID: "primary", ReasoningEfforts: []string{"low", "high"}},
		{ID: "aux", ReasoningEfforts: []string{"low", "high"}},
	}}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	other := models.ModelConfiguration{ID: "notification-group", Backend: "codex", State: "active", Binding: modelrouting.Binding{
		Kind: "connection", ConnectionID: connection.ID, ModelID: "primary", AuxiliaryModelIDs: []string{"aux"},
	}}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	agent, _ := base.agents.Get("agentA")
	agent.ModelBinding.ConfigurationIDs = []string{"sandbox-fixture", other.ID}
	base.agents.SetModelConfigurationResolver(func(binding *modelrouting.Binding) (string, error) {
		if binding.ConfigurationID == other.ID {
			return other.Backend, nil
		}
		return "cursor", nil
	})
	if err := base.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	solo := NewSoloService(db, nil, base.agents, NewProjectService(db))
	aligner := &recordAligner{}
	solo.SetModelAlignerForTest(aligner)
	row := models.SoloSession{
		ID: "notified-settings", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning,
		InitialConfigurationID: "sandbox-fixture", CurrentConfigurationID: "sandbox-fixture", AppliedConfigurationID: "sandbox-fixture",
		CurrentModelID: "aux", CurrentModelSet: true, AppliedModelID: "aux", AppliedModelSet: true,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	var viewers []<-chan chatsession.Event
	for range 2 {
		viewer, stop, err := solo.Subscribe(row.ID, "alice", -1)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		<-viewer // Initial queue_state; subsequent events must come from settings saves.
		viewers = append(viewers, viewer)
	}
	expectSelection := func(group, model, backend, effort, pendingChange string, choices []string) {
		t.Helper()
		for _, viewer := range viewers {
			select {
			case event := <-viewer:
				if event.Event != SoloEventModel || event.Payload["currentConfigurationId"] != group || event.Payload["currentModelId"] != model || event.Payload["currentModelSet"] != true || event.Payload["backend"] != backend || event.Payload["reasoningEffort"] != effort || event.Payload["modelNotice"] != "" || event.Payload["pendingChange"] != pendingChange || !reflect.DeepEqual(event.Payload["reasoningEfforts"], choices) {
					t.Fatalf("incomplete settings notification: %#v", event)
				}
			default:
				t.Fatal("successful settings save did not notify every subscribed viewer")
			}
		}
	}
	expectNoEvents := func() {
		t.Helper()
		for _, viewer := range viewers {
			select {
			case event := <-viewer:
				t.Fatalf("rejected settings change emitted an event: %#v", event)
			default:
			}
		}
	}
	ctx := context.Background()
	aux, high := "aux", "high"
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", &other.ID, &aux, &high); err != nil {
		t.Fatal(err)
	}
	expectSelection(other.ID, aux, "codex", high, "model", []string{"low", "high"})
	current, _ := solo.Require(row.ID, "alice")
	if current.AppliedConfigurationID != row.AppliedConfigurationID || current.AppliedModelID != row.AppliedModelID || current.AppliedReasoningEffort != "" || aligner.count() != 0 {
		t.Fatal("announcing the selection changed the applied route before the next turn")
	}
	if err := db.Model(&row).Updates(map[string]any{"applied_configuration_id": other.ID, "applied_model_id": aux}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", nil, nil, &high); err != nil {
		t.Fatal(err)
	}
	expectSelection(other.ID, aux, "codex", high, "effort", []string{"low", "high"})
	if err := db.Model(&row).Update("applied_reasoning_effort", high).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", nil, nil, &high); err != nil {
		t.Fatal(err)
	}
	expectSelection(other.ID, aux, "codex", high, "", []string{"low", "high"})
	primary := "primary"
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", nil, &primary, nil); err != nil {
		t.Fatal(err)
	}
	expectSelection(other.ID, "", "codex", high, "model", []string{"low", "high"})
	defaultGroup := ""
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", &defaultGroup, &primary, nil); err != nil {
		t.Fatal(err)
	}
	expectSelection("sandbox-fixture", "", "cursor", "", "model", []string{})
	invalid := "missing-model"
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", nil, &invalid, nil); err == nil {
		t.Fatal("invalid model was accepted")
	}
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "bob", &other.ID, &aux, nil); err == nil {
		t.Fatal("another user changed the selected model")
	}
	const callback = "fail-solo-settings-notification-write"
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "solo_sessions" {
			tx.AddError(errors.New("settings save unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", &other.ID, &aux, nil); err == nil {
		t.Fatal("failed settings write was reported as successful")
	}
	expectNoEvents()
}

func TestSoloGroupQualifiedSelectionKeepsDefaultAndChoosesItsTool(t *testing.T) {
	base, _, _, db := newSoloFixture(t, 0, 3)
	insertSoloModelConfig(t, db)
	other := models.ModelConfiguration{
		ID: "review-group", Name: "Review", Backend: "claude_code", State: "active",
		Binding: modelrouting.Binding{Kind: "platform", CredentialID: "different-account", ModelID: "primary", AuxiliaryModelIDs: []string{"aux"}},
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	agent, _ := base.agents.Get("agentA")
	agent.ModelBinding.ConfigurationIDs = []string{"sandbox-fixture", other.ID}
	base.agents.SetModelConfigurationResolver(func(binding *modelrouting.Binding) (string, error) {
		if binding.ConfigurationID == other.ID {
			return other.Backend, nil
		}
		return "cursor", nil
	})
	if err := base.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	// No sandbox is needed to exercise API selection and persisted routing.
	solo := NewSoloService(db, nil, base.agents, NewProjectService(db))
	row := models.SoloSession{
		ID: "qualified-route", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning,
		InitialConfigurationID: "sandbox-fixture", CurrentConfigurationID: "sandbox-fixture", AppliedConfigurationID: "sandbox-fixture",
		CurrentModelSet: true, AppliedModelSet: true,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	primary := "primary"
	view, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", &other.ID, &primary, nil)
	if err != nil || view.CurrentConfigurationID != other.ID || view.CurrentModelID != "" || view.Backend != "claude_code" || view.InitialConfigurationID != "sandbox-fixture" {
		t.Fatalf("qualified same primary: %+v, %v", view, err)
	}
	stored, _ := base.agents.Get("agentA")
	if stored.ModelBinding.ConfigurationID != "sandbox-fixture" || stored.AcpBackend != "cursor" {
		t.Fatalf("session switch changed Agent default: %+v", stored)
	}
	// Omitted configuration stays on the selected group, rather than returning
	// to the Agent default when only the model or effort changes.
	view, err = solo.SetCurrentModel(ctx, row.ID, "alice", "aux")
	if err != nil || view.CurrentConfigurationID != other.ID || view.CurrentModelID != "aux" {
		t.Fatalf("model-only selection lost group: %+v %v", view, err)
	}
	aligner := &recordAligner{}
	solo.SetModelAlignerForTest(aligner)
	current, _ := solo.Require(row.ID, "alice")
	changed, err := solo.alignSessionModel(ctx, solo.newChat(row.ID), 0, current)
	if err != nil || !changed || aligner.count() != 1 {
		t.Fatalf("group-qualified alignment: changed=%v calls=%d err=%v", changed, aligner.count(), err)
	}
	current, _ = solo.Require(row.ID, "alice")
	if current.AppliedConfigurationID != other.ID || current.AppliedModelID != "aux" || !current.ModelContextPending {
		t.Fatalf("applied group/history flag not persisted: %+v", current)
	}
	if err := db.Model(&other).Update("state", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SetCurrentModel(ctx, row.ID, "alice", ""); err == nil {
		t.Fatal("disabled selected group was accepted")
	}
	current, _ = solo.Require(row.ID, "alice")
	if current.CurrentConfigurationID != other.ID {
		t.Fatal("disabled group silently fell back to another account")
	}
	view, err = solo.Get(ctx, row.ID, "alice")
	if err != nil || view.Backend != "" || len(view.ReasoningEfforts) != 0 {
		t.Fatalf("invalid group borrowed default tool/settings: %+v %v", view, err)
	}
	missing := "not-bound"
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", &missing, &primary, nil); err == nil || !strings.Contains(err.Error(), "未绑定") {
		t.Fatalf("unbound group: %v", err)
	}
	if _, err := solo.SetCurrentModelRouteSettings(ctx, row.ID, "bob", &missing, &primary, nil); err == nil {
		t.Fatal("another user changed this session")
	}
	defaultGroup := ""
	view, err = solo.SetCurrentModelRouteSettings(ctx, row.ID, "alice", &defaultGroup, &primary, nil)
	if err != nil || view.CurrentConfigurationID != "sandbox-fixture" || view.Backend != "cursor" {
		t.Fatalf("explicit default: %+v %v", view, err)
	}
}

func TestSoloHistoryReadFailureBlocksFreshModelUntilContextCanBeLoaded(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	ctx := context.Background()
	view, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready for context handoff", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	gate := newGateChat(false)
	solo.SetChatterForTest(gate)
	aligner := &recordAligner{}
	solo.SetModelAlignerForTest(aligner)
	if _, err := solo.SetCurrentModel(ctx, view.ID, "alice", "aux"); err != nil {
		t.Fatal(err)
	}
	const callback = "fail-history-handoff-query"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "solo_messages" {
			tx.AddError(errors.New("history temporarily unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	if _, err := solo.Send(view.ID, "alice", "continue after switching", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "handoff failure finishes turn", func() bool {
		chat := solo.lookup(view.ID)
		return aligner.count() == 1 && chat != nil && chat.sess.Ready()
	})
	if gate.count() != 0 {
		t.Fatal("fresh model ran without its existing context")
	}
	row, _ := solo.Require(view.ID, "alice")
	if !row.ModelContextPending {
		t.Fatal("failed handoff dropped the context retry marker")
	}
	if err := db.Callback().Query().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Send(view.ID, "alice", "retry with context", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "handoff retry uses saved history", func() bool {
		chat := solo.lookup(view.ID)
		return gate.count() == 1 && chat != nil && chat.sess.Ready()
	})
	if prompt := gate.snapshot()[0]; !strings.Contains(prompt, "continue after switching") || !strings.Contains(prompt, "用户本轮消息：\nretry with context") {
		t.Fatalf("retry lost existing dialogue: %s", prompt)
	}
	row, _ = solo.Require(view.ID, "alice")
	if row.ModelContextPending {
		t.Fatal("successful context retry kept a stale marker")
	}
}

func TestSoloStartupPinsLegacyRouteBeforeAgentDefaultChanges(t *testing.T) {
	base, _, _, db := newSoloFixture(t, 0, 3)
	old := models.SoloSession{ID: "old-before-groups", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, CurrentModelSet: true, AppliedModelSet: true}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	explicit := models.SoloSession{ID: "already-qualified", UserID: "alice", AgentName: "agentA", InitialConfigurationID: "explicit-opening", CurrentConfigurationID: "explicit-current", AppliedConfigurationID: "explicit-applied"}
	if err := db.Create(&explicit).Error; err != nil {
		t.Fatal(err)
	}
	solo := NewSoloService(db, nil, base.agents, NewProjectService(db))
	pinned, err := solo.Require(old.ID, "alice")
	if err != nil || pinned.InitialConfigurationID != "sandbox-fixture" || pinned.CurrentConfigurationID != "sandbox-fixture" || pinned.AppliedConfigurationID != "sandbox-fixture" {
		t.Fatalf("legacy route was not pinned: %+v %v", pinned, err)
	}
	qualified, _ := solo.Require(explicit.ID, "alice")
	if qualified.InitialConfigurationID != explicit.InitialConfigurationID || qualified.CurrentConfigurationID != explicit.CurrentConfigurationID || qualified.AppliedConfigurationID != explicit.AppliedConfigurationID {
		t.Fatalf("explicit group references were rewritten: %+v", qualified)
	}
	newGroup := models.ModelConfiguration{ID: "new-default", Backend: "claude_code", State: "active", Binding: modelrouting.Binding{Kind: "platform", ModelID: "another-model"}}
	if err := db.Create(&newGroup).Error; err != nil {
		t.Fatal(err)
	}
	base.agents.SetModelConfigurationResolver(func(binding *modelrouting.Binding) (string, error) {
		if binding.ConfigurationID == newGroup.ID {
			return "claude_code", nil
		}
		return "cursor", nil
	})
	agent, _ := base.agents.Get("agentA")
	agent.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: newGroup.ID, ConfigurationIDs: []string{newGroup.ID, "sandbox-fixture"}}
	if err := base.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	view, err := solo.Get(context.Background(), old.ID, "alice")
	if err != nil || view.CurrentConfigurationID != "sandbox-fixture" || view.Backend != "cursor" {
		t.Fatalf("changed default moved existing session to another account: %+v %v", view, err)
	}
}

type ambiguousSoloModelAligner struct {
	mu    sync.Mutex
	calls int
}

func (a *ambiguousSoloModelAligner) AlignChatModel(context.Context, uint, string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if a.calls == 1 {
		// The CLI accepted the new model but its response was lost.
		return false, ErrChatModelUnknown
	}
	// The retry observes that the desired model is already selected.
	return false, nil
}

func (a *ambiguousSoloModelAligner) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func TestSoloAmbiguousModelSwitchStillSeedsHistoryWhenRetryIsNoop(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	gate := newGateChat(false)
	solo.SetChatterForTest(gate)
	aligner := &ambiguousSoloModelAligner{}
	solo.SetModelAlignerForTest(aligner)
	if _, err := solo.SetCurrentModel(context.Background(), view.ID, "alice", "aux"); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Send(view.ID, "alice", "remember my task", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "uncertain switch finishes", func() bool {
		chat := solo.lookup(view.ID)
		return aligner.count() == 1 && chat != nil && chat.sess.Ready()
	})
	row, _ := solo.Require(view.ID, "alice")
	if !row.ModelContextPending || row.CurrentModelID != "aux" || gate.count() != 0 {
		t.Fatalf("uncertain switch lost pending handoff: %+v", row)
	}
	if _, err := solo.Send(view.ID, "alice", "continue", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "no-op retry supplies context", func() bool {
		chat := solo.lookup(view.ID)
		return aligner.count() == 2 && gate.count() == 1 && chat != nil && chat.sess.Ready()
	})
	if prompt := gate.snapshot()[0]; !strings.Contains(prompt, "remember my task") || !strings.Contains(prompt, "编码模型已更换") {
		t.Fatalf("already-applied model started from zero: %s", prompt)
	}
}

func TestSoloConfigurationSelectionKeepsCustomLayoutAndRejectsInvalidGroups(t *testing.T) {
	_, _, _, db := newSoloFixture(t, 0, 3)
	configuration := models.ModelConfiguration{ID: "other-cli", Backend: "codex", State: "active", Binding: modelrouting.Binding{Kind: "platform"}}
	if err := db.Create(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	agent := Agent{AcpBackend: "cursor", Layout: AgentLayout{ConfigRoot: DefaultConfigRootForBackend("cursor")}, ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "default", ConfigurationIDs: []string{"default", configuration.ID}}}
	selected, err := selectSoloConfiguration(db, agent, configuration.ID)
	if err != nil || selected.AcpBackend != "codex" || selected.Layout.ConfigRoot != DefaultConfigRootForBackend("codex") || len(selected.ModelBinding.ConfigurationIDs) != 0 {
		t.Fatalf("execution route: %+v %v", selected, err)
	}
	if agent.AcpBackend != "cursor" || agent.ModelBinding.ConfigurationID != "default" {
		t.Fatal("selection mutated source Agent")
	}
	agent.Layout.ConfigRoot = "/custom/config"
	selected, err = selectSoloConfiguration(db, agent, configuration.ID)
	if err != nil || selected.Layout.ConfigRoot != "/custom/config" {
		t.Fatalf("custom config root lost: %+v %v", selected, err)
	}
	if _, err := selectSoloConfiguration(db, Agent{}, configuration.ID); err == nil {
		t.Fatal("unconfigured Agent accepted")
	}
	if err := db.Delete(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := selectSoloConfiguration(db, agent, configuration.ID); err == nil {
		t.Fatal("deleted model group accepted")
	}
	var se *SoloError
	if _, err := selectSoloConfiguration(db, agent, "not-bound"); !errors.As(err, &se) {
		t.Fatalf("selection lacks actionable error: %v", err)
	}
}

func TestSoloLocalModeAllowsSwitchingBetweenAllLocalConfigurations(t *testing.T) {
	t.Setenv("GRASP_EXECUTION_MODE", "local")
	_, _, _, db := newSoloFixture(t, 0, 3)
	for _, cfg := range []models.ModelConfiguration{
		{ID: "model-local-deepseek-harness", Backend: "deepseek_harness", State: "active", Binding: modelrouting.Binding{Kind: "local"}},
		{ID: "model-local-claude-code", Backend: "claude_code", State: "active", Binding: modelrouting.Binding{Kind: "local"}},
		{ID: "model-local-opencode", Backend: "opencode", State: "active", Binding: modelrouting.Binding{Kind: "local"}},
	} {
		if err := db.Create(&cfg).Error; err != nil {
			t.Fatal(err)
		}
	}
	agent := Agent{
		AcpBackend:   "deepseek_harness",
		Layout:       AgentLayout{ConfigRoot: DefaultConfigRootForBackend("deepseek_harness")},
		ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "model-local-deepseek-harness"},
	}
	for _, target := range []struct {
		id      string
		backend string
	}{
		{"model-local-claude-code", "claude_code"},
		{"model-local-opencode", "opencode"},
		{"model-local-deepseek-harness", "deepseek_harness"},
	} {
		selected, err := selectSoloConfiguration(db, agent, target.id)
		if err != nil || selected.AcpBackend != target.backend || selected.ModelBinding.ConfigurationID != target.id {
			t.Fatalf("local mode switch to %s failed: %+v %v", target.id, selected, err)
		}
	}
}
