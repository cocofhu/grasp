package services

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestSoloFixedModelUsesNegotiatedDSHEffortsWithoutMetadata(t *testing.T) {
	selected, effort := "", ""
	available := []string{"quiet", "deep"}
	posts := 0
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "fixture", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"selected": selected, "reasoningEffort": effort, "reasoningEfforts": available})
		case "/api/model":
			var input struct {
				Model  string `json:"model"`
				Effort string `json:"reasoningEffort"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			selected, effort = input.Model, input.Effort
			posts++
			_ = json.NewEncoder(w).Encode(map[string]any{"selected": selected, "reasoningEffort": effort})
		default:
			http.NotFound(w, request)
		}
	}))
	defer peer.Close()
	host, rawPort, err := net.SplitHostPort(strings.TrimPrefix(peer.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatal(err)
	}
	solo, _, _, _ := newSoloFixture(t, 0, 3)
	connection := models.ModelConnection{ID: "dsh-live", State: "active", Models: []modelrouting.Model{{ID: "kimi"}}}
	if err := solo.db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	configuration := models.ModelConfiguration{ID: "sandbox-fixture", State: "active", Backend: "deepseek_harness", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: "kimi"}}
	if err := solo.db.Create(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	agent, _ := solo.agents.Get("agentA")
	agent.AcpBackend = "deepseek_harness"
	if err := solo.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	sandboxRow := models.Sandbox{Name: "live-dsh", Host: host, ACPPort: port, Token: "fixture", Status: "running"}
	if err := solo.db.Create(&sandboxRow).Error; err != nil {
		t.Fatal(err)
	}
	row := models.SoloSession{ID: "live-reasoning", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, SandboxID: sandboxRow.ID, CurrentModelSet: true, AppliedModelSet: true}
	if err := solo.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	view, err := solo.Get(ctx, row.ID, "alice")
	if err != nil || !reflect.DeepEqual(view.ReasoningEfforts, []string{"quiet", "deep"}) {
		t.Fatalf("missing negotiated choices: %+v %v", view, err)
	}
	view, err = solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer("deep"))
	if err != nil || view.CurrentModelID != "" || view.CurrentReasoningEffort != "deep" || posts != 0 {
		t.Fatalf("effort-only changed model or applied too early: %+v posts=%d err=%v", view, posts, err)
	}
	current, _ := solo.Require(row.ID, "alice")
	if changed, err := solo.alignSessionModel(ctx, solo.newChat(row.ID), sandboxRow.ID, current); err != nil || !changed || posts != 1 || selected != "" || effort != "deep" {
		t.Fatalf("negotiated effort not applied: changed=%v posts=%d pair=%s/%s err=%v", changed, posts, selected, effort, err)
	}
	selected = "not-configured"
	if _, err := solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer("quiet")); err == nil {
		t.Fatal("live options from another model were accepted")
	}
	selected = ""
	connection.Models[0].ReasoningEfforts = []string{"invented", "quiet"}
	if err := solo.db.Save(&connection).Error; err != nil {
		t.Fatal(err)
	}
	view, err = solo.Get(ctx, row.ID, "alice")
	if err != nil || !reflect.DeepEqual(view.ReasoningEfforts, []string{"quiet", "deep"}) {
		t.Fatalf("stale metadata replaced live choices: %+v %v", view, err)
	}
	if _, err := solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer("invented")); err == nil {
		t.Fatal("stale metadata granted a non-advertised effort")
	}
	available = []string{}
	view, err = solo.Get(ctx, row.ID, "alice")
	if err != nil || view.ReasoningEfforts == nil || len(view.ReasoningEfforts) != 0 {
		t.Fatalf("empty live capability must override stale metadata: %+v %v", view, err)
	}
	if _, err := solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer("quiet")); err == nil {
		t.Fatal("empty live capabilities did not block effort")
	}
	available = []string{"quiet", "deep"}
	no := false
	connection.Models[0].Capabilities.Reasoning = &no
	if err := solo.db.Save(&connection).Error; err != nil {
		t.Fatal(err)
	}
	view, err = solo.Get(ctx, row.ID, "alice")
	if err != nil || len(view.ReasoningEfforts) != 0 {
		t.Fatalf("explicit non-reasoning capability ignored: %+v %v", view, err)
	}
	if _, err := solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer("quiet")); err == nil {
		t.Fatal("live options overrode explicit non-support")
	}
}

func prepareSoloEfforts(t *testing.T, solo *SoloService) {
	t.Helper()
	connection := modelrouting.Connection{ID: "effort-connection", State: "active", ProviderID: "fixture", Protocol: modelrouting.ProtocolResponses, Models: []modelrouting.Model{
		{ID: "primary", ReasoningEfforts: []string{"low", "high"}},
		{ID: "aux", ReasoningEfforts: []string{"high", "ultra"}},
		{ID: "fixed"},
	}}
	if err := solo.db.Create((*models.ModelConnection)(&connection)).Error; err != nil {
		t.Fatal(err)
	}
	configuration := models.ModelConfiguration{ID: "sandbox-fixture", State: "active", Backend: "codex", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: "primary", AuxiliaryModelIDs: []string{"aux", "fixed"}}}
	if err := solo.db.Create(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	agent, _ := solo.agents.Get("agentA")
	agent.AcpBackend = "codex"
	agent.Env[runtime.EnvACPBridgeReasoningEffort] = "legacy-value"
	if err := solo.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	solo.sbx.modelBinding = func(backend string, _ *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{Backend: backend, Binding: configuration.Binding, CredentialID: "fixture", Secret: "test-key", Connection: &connection}, nil
	}
}

func effortPointer(value string) *string { return &value }

func TestSoloReasoningChoicesValidateWithoutChangingAgent(t *testing.T) {
	solo, _, _, _ := newSoloFixture(t, 0, 3)
	prepareSoloEfforts(t, solo)
	before, _ := solo.agents.Get("agentA")
	row := &models.SoloSession{ID: "settings", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, CurrentModelID: "aux", CurrentModelSet: true, CurrentReasoningEffort: "high"}
	if err := solo.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	view, err := solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer("ultra"))
	if err != nil || view.CurrentModelID != "aux" || view.CurrentReasoningEffort != "ultra" || !reflect.DeepEqual(view.ReasoningEfforts, []string{"high", "ultra"}) {
		t.Fatalf("effort-only: view=%+v err=%v", view, err)
	}
	view, err = solo.SetCurrentModelSettings(ctx, row.ID, "alice", effortPointer("primary"), nil)
	if err != nil || view.CurrentModelID != "" || view.CurrentReasoningEffort != "" {
		t.Fatalf("incompatible effort must reset: view=%+v err=%v", view, err)
	}
	view, err = solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer("high"))
	if err != nil {
		t.Fatal(err)
	}
	view, err = solo.SetCurrentModelSettings(ctx, row.ID, "alice", effortPointer("aux"), nil)
	if err != nil || view.CurrentReasoningEffort != "high" {
		t.Fatalf("compatible effort lost: %+v %v", view, err)
	}
	_, err = solo.SetCurrentModelSettings(ctx, row.ID, "alice", effortPointer("fixed"), effortPointer("ultra"))
	var inputErr *SoloError
	if !errors.As(err, &inputErr) || inputErr.Code != "reasoning_effort_unavailable" {
		t.Fatalf("unadvertised effort accepted: %v", err)
	}
	got, _ := solo.Require(row.ID, "alice")
	if got.CurrentModelID != "aux" || got.CurrentReasoningEffort != "high" {
		t.Fatalf("invalid request wrote settings: %+v", got)
	}
	view, err = solo.SetCurrentModelSettings(ctx, row.ID, "alice", nil, effortPointer(""))
	if err != nil || view.CurrentModelID != "aux" || view.CurrentReasoningEffort != "" {
		t.Fatalf("explicit reset: %+v %v", view, err)
	}
	after, _ := solo.agents.Get("agentA")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("session settings wrote the Agent")
	}
}

func TestSoloCreateReasoningRejectsBeforeWritingAndRestoresOnRestart(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, gateway, _ := newSoloFixture(t, port, 3)
	prepareSoloEfforts(t, solo)
	ctx := context.Background()
	if _, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", ReasoningEffort: "ultra"}); err == nil {
		t.Fatal("invalid primary effort accepted")
	}
	var count int64
	solo.db.Model(&models.SoloSession{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid effort created a session")
	}
	view, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", InitialModelID: "aux", ReasoningEffort: "ultra"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "opening effort", func() bool {
		row, _ := solo.Require(view.ID, "alice")
		return row.Status == models.SoloStatusRunning && gateway.LastCreateEnv()[runtime.EnvACPBridgeReasoningEffort] == "ultra"
	})
	if _, err := solo.Close(ctx, view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Restart(ctx, view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restarted effort", func() bool {
		row, _ := solo.Require(view.ID, "alice")
		return row.Status == models.SoloStatusRunning && row.AppliedReasoningEffort == "ultra" && gateway.LastCreateEnv()[runtime.EnvACPBridgeReasoningEffort] == "ultra"
	})
	if _, err := solo.Close(ctx, view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	reset, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "default effort clears legacy env", func() bool {
		row, _ := solo.Require(reset.ID, "alice")
		return row.Status == models.SoloStatusRunning && gateway.LastCreateEnv()[runtime.EnvACPBridgeReasoningEffort] == ""
	})
}

type settingsAligner struct {
	mu      sync.Mutex
	choices [][2]string
	err     error
	before  func()
}

func (a *settingsAligner) AlignChatModel(context.Context, uint, string) (bool, error) {
	return false, errors.New("model-only aligner used for settings")
}
func (a *settingsAligner) AlignChatSettings(_ context.Context, _ uint, model, effort string) (bool, error) {
	a.mu.Lock()
	a.choices = append(a.choices, [2]string{model, effort})
	a.mu.Unlock()
	if a.before != nil {
		a.before()
	}
	return a.err == nil, a.err
}
func (a *settingsAligner) snapshot() [][2]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([][2]string(nil), a.choices...)
}

func TestSoloEffortWaitsForTurnBoundaryAndCarriesHistory(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, _ := newSoloFixture(t, port, 3)
	prepareSoloEfforts(t, solo)
	ctx := context.Background()
	view, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", InitialModelID: "aux", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	gate := newGateChat(true)
	defer gate.release()
	aligner := &settingsAligner{}
	solo.SetChatterForTest(gate)
	solo.SetModelAlignerForTest(aligner)
	if _, err := solo.Send(view.ID, "alice", "keep this context", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first turn", gate.started)
	if _, err := solo.SetCurrentModelSettings(ctx, view.ID, "alice", nil, effortPointer("ultra")); err != nil {
		t.Fatal(err)
	}
	if len(aligner.snapshot()) != 0 {
		t.Fatal("effort interrupted the generating turn")
	}
	if _, err := solo.Send(view.ID, "alice", "next", nil); err != nil {
		t.Fatal(err)
	}
	gate.release()
	waitFor(t, "next turn", func() bool {
		chat := solo.lookup(view.ID)
		return gate.count() >= 2 && chat != nil && chat.sess.Ready()
	})
	if got := aligner.snapshot(); !reflect.DeepEqual(got, [][2]string{{"aux", "ultra"}}) {
		t.Fatalf("applied settings: %v", got)
	}
	if text := gate.snapshot()[1]; !strings.Contains(text, "推理强度已调整") || !strings.Contains(text, "[用户] keep this context") {
		t.Fatalf("effort change lost dialogue: %q", text)
	}
	row, _ := solo.Require(view.ID, "alice")
	if row.CurrentModelID != "aux" || row.AppliedReasoningEffort != "ultra" || row.CurrentReasoningEffort != "ultra" {
		t.Fatalf("stored settings: %+v", row)
	}
}

func TestSoloFailedSettingsRollbackBothAndPreserveNewerChoice(t *testing.T) {
	for _, newer := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "newer-choice"}[newer], func(t *testing.T) {
			solo, _, _, _ := newSoloFixture(t, 0, 3)
			prepareSoloEfforts(t, solo)
			row := &models.SoloSession{ID: "rollback-settings", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, CurrentModelSet: true, CurrentModelID: "aux", CurrentReasoningEffort: "ultra", AppliedModelSet: true, AppliedReasoningEffort: "high"}
			if err := solo.db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
			aligner := &settingsAligner{err: errors.New("CLI rejected effort")}
			if newer {
				aligner.before = func() {
					if _, err := solo.SetCurrentModelSettings(context.Background(), row.ID, "alice", effortPointer("primary"), effortPointer("low")); err != nil {
						t.Fatal(err)
					}
				}
			}
			solo.SetModelAlignerForTest(aligner)
			if _, err := solo.alignSessionModel(context.Background(), solo.newChat(row.ID), 1, row); err == nil {
				t.Fatal("rejection ignored")
			}
			got, _ := solo.Require(row.ID, "alice")
			if got.CurrentModelID != "" {
				t.Fatalf("wrong model after rollback: %q", got.CurrentModelID)
			}
			if newer {
				if got.CurrentReasoningEffort != "low" || got.ModelNotice != "" {
					t.Fatalf("newer choice lost: %+v", got)
				}
			} else if got.CurrentReasoningEffort != "high" || got.ModelNotice != soloModelSwitchFailed {
				t.Fatalf("settings not rolled back together: %+v", got)
			}
		})
	}
}

func TestSoloLegacyNullEffortStillRollsBackModel(t *testing.T) {
	solo, _, _, _ := newSoloFixture(t, 0, 3)
	prepareSoloEfforts(t, solo)
	row := &models.SoloSession{ID: "legacy-null-effort", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, CurrentModelID: "aux", CurrentModelSet: true, AppliedModelSet: true}
	if err := solo.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if err := solo.db.Model(row).UpdateColumn("current_reasoning_effort", nil).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := solo.Require(row.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	solo.SetModelAlignerForTest(&settingsAligner{err: errors.New("CLI rejected model")})
	if _, err := solo.alignSessionModel(context.Background(), solo.newChat(row.ID), 1, loaded); err == nil {
		t.Fatal("rejection ignored")
	}
	after, _ := solo.Require(row.ID, "alice")
	if after.CurrentModelID != "" || after.ModelNotice != soloModelSwitchFailed {
		t.Fatalf("migrated session did not roll back: %+v", after)
	}
}

func TestOpenCodeSessionUsesTheSavedVariantWithoutWritingBack(t *testing.T) {
	solo, _, _, _ := newSoloFixture(t, 0, 3)
	configuration := models.ModelConfiguration{ID: "official-opencode", State: "active", Backend: "opencode", Binding: modelrouting.Binding{
		Kind: "platform", CredentialID: "zen", ModelID: "opencode/gpt-5.5",
		ToolSettings: modelrouting.ToolSettings{ModelEfforts: map[string]string{"opencode/gpt-5.5": "high"}},
	}}
	if err := solo.db.Create(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	agent, _ := solo.agents.Get("agentA")
	agent.AcpBackend = "opencode"
	agent.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}
	if err := solo.agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	if got := solo.configuredOpenCodeEffort(agent, ""); got != "high" {
		t.Fatalf("empty session request did not start from the saved variant: %q", got)
	}
	efforts := solo.reasoningEfforts(agent, "opencode/gpt-5.5")
	if !reflect.DeepEqual(efforts, []string{"none", "low", "medium", "high", "xhigh"}) {
		t.Fatalf("official menu: %v", efforts)
	}
	if err := solo.validateReasoningEffort(agent, "opencode/gpt-5.5", ""); err != nil {
		t.Fatal(err)
	}
	configuration.Binding.ToolSettings.ModelEfforts["opencode/gpt-5.5"] = "turbo"
	if err := solo.db.Save(&configuration).Error; err != nil {
		t.Fatal(err)
	}
	efforts = solo.reasoningEfforts(agent, "opencode/gpt-5.5")
	if len(efforts) != 0 {
		t.Fatalf("custom level should hide the chat menu: %v", efforts)
	}
	if err := solo.validateReasoningEffort(agent, "opencode/gpt-5.5", "turbo"); err != nil {
		t.Fatal(err)
	}
	if err := solo.validateReasoningEffort(agent, "opencode/gpt-5.5", "high"); err == nil {
		t.Fatal("chat selected a documented level while the model is fixed")
	}
	if err := solo.validateReasoningEffort(agent, "opencode/gpt-5.5", "not a variant"); err == nil {
		t.Fatal("an invented session variant was accepted")
	}
	agent.AcpBackend = "cursor"
	if solo.configuredOpenCodeEffort(agent, "") != "" || solo.reasoningEfforts(agent, "opencode/gpt-5.5") != nil {
		t.Fatal("another coding tool received OpenCode variants")
	}
}
