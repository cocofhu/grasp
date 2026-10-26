package service

import (
	"encoding/json"
	"errors"
	"testing"

	"backend/internal/provider"
)

func TestProjectConfiguredModelsKeepsOrderAndMarksMissing(t *testing.T) {
	spec := []configuredModelSpec{{ID: "", Vendor: "cursor"}, {ID: "aux", Vendor: "cursor"}, {ID: "gone", Vendor: "xai"}}
	catalog := []AgentModel{{ID: "aux", Name: "Auxiliary", Vendor: "xai"}, {ID: "other", Name: "Other"}}
	got := projectConfiguredModels(spec, catalog, nil)
	if len(got) != 3 || got[0].ID != "" || got[0].Name != "自动" || got[0].Unavailable {
		t.Fatalf("primary auto: %+v", got)
	}
	if got[1].ID != "aux" || got[1].Name != "Auxiliary" || got[1].Vendor != "xai" || got[1].Unavailable {
		t.Fatalf("known auxiliary: %+v", got[1])
	}
	if got[2].ID != "gone" || !got[2].Unavailable || got[2].Vendor != "xai" {
		t.Fatalf("missing model: %+v", got[2])
	}
	unknown := projectConfiguredModels([]configuredModelSpec{{ID: "only"}}, nil, errCatalog)
	if len(unknown) != 1 || unknown[0].Unavailable {
		t.Fatalf("catalog failure should not mark the saved model unavailable: %+v", unknown)
	}
}

func TestConfiguredReasoningEffortsAreExactAndAdapterBacked(t *testing.T) {
	t.Setenv("GRASP_CONFIGURED_MODELS", `[{"id":"pinned-model","reasoningEfforts":["low","ultra"]}]`)
	for _, backend := range []string{"codex", "deepseek_harness"} {
		t.Setenv("AGENT_PROVIDER", backend)
		if !ReasoningEffortSelectable("pinned-model", "ultra") || !ReasoningEffortSelectable("pinned-model", "") || ReasoningEffortSelectable("other", "ultra") || ReasoningEffortSelectable("pinned-model", "max") {
			t.Fatalf("%s accepted an unadvertised model/effort or rejected an exact value", backend)
		}
	}
	t.Setenv("AGENT_PROVIDER", "cursor")
	if ReasoningEffortSelectable("pinned-model", "ultra") {
		t.Fatal("a model name/metadata must not invent a separate-effort provider adapter")
	}
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv("GRASP_CONFIGURED_MODELS", "{bad")
	if ReasoningEffortSelectable("pinned-model", "low") {
		t.Fatal("invalid capability metadata must reject explicit efforts")
	}

	rows := projectConfiguredModels([]configuredModelSpec{{ID: "pinned-model", ReasoningEfforts: []string{"ultra"}}}, nil, nil)
	if len(rows) != 1 || rows[0].Unavailable || len(rows[0].ReasoningEfforts) != 1 || rows[0].ReasoningEfforts[0] != "ultra" {
		t.Fatalf("a provider without a catalog must keep saved model capabilities: %+v", rows)
	}
}

func TestModelAndEffortRestartIsAtomicAndChatScoped(t *testing.T) {
	t.Setenv("ACP_BRIDGE_REASONING_EFFORT", "low")
	manager := NewChatManager()
	bridge, err := manager.Create("", "original")
	if err != nil {
		t.Fatal(err)
	}
	bridge.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		if bridge.Model() != "next" || bridge.ReasoningEffort() != "ultra" {
			t.Fatalf("restart observed a split model/effort pair: %q %q", bridge.Model(), bridge.ReasoningEffort())
		}
		return nil, errors.New("fixture restart rejected")
	}
	if _, err := bridge.SwitchModelSettings("next", "ultra"); err == nil {
		t.Fatal("expected restart failure")
	}
	if bridge.Model() != "original" || bridge.ReasoningEffort() != "low" {
		t.Fatalf("rollback lost original pair: %q %q", bridge.Model(), bridge.ReasoningEffort())
	}
	bridge.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		return &stubSess{id: "next"}, nil
	}
	if _, err := bridge.SwitchModelSettings("next", "ultra"); err != nil {
		t.Fatal(err)
	}
	if manager.Default().ReasoningEffort() != "low" || bridge.ReasoningEffort() != "ultra" {
		t.Fatal("effort change leaked to another chat")
	}
}

type reasoningSession struct {
	*stubSess
	model   string
	current string
	values  []string
}

func (s *reasoningSession) Info() provider.AgentInfo {
	return provider.AgentInfo{Name: "dsh", ModelID: s.model}
}
func (s *reasoningSession) ReasoningOptions() (string, []string) { return s.current, s.values }

func TestLiveReasoningOptionsAreScopedToActualModelAndHonorExplicitDisable(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "deepseek_harness")
	t.Setenv("GRASP_CONFIGURED_MODELS", `[{"id":"kimi"},{"id":"other","reasoningEfforts":["low"]}]`)
	manager := NewChatManager()
	manager.SetDefaultModel("kimi")
	bridge := manager.Default()
	attach(bridge, &reasoningSession{stubSess: &stubSess{id: "live"}, model: "kimi", current: "high", values: []string{"low", "high"}})
	current, values := bridge.ReasoningOptions("kimi")
	if current != "high" || len(values) != 2 || !bridge.ReasoningEffortSelectable("kimi", "high") {
		t.Fatalf("actual advertisement missing without stored metadata: %q %v", current, values)
	}
	values[0] = "modified"
	_, values = bridge.ReasoningOptions("kimi")
	if values[0] != "low" {
		t.Fatal("caller mutated session capability snapshot")
	}
	_, values = bridge.ReasoningOptions("other")
	if len(values) != 1 || values[0] != "low" || bridge.ReasoningEffortSelectable("other", "high") {
		t.Fatalf("live options leaked to another model: %v", values)
	}
	t.Setenv("GRASP_CONFIGURED_MODELS", `[{"id":"kimi","reasoningDisabled":true}]`)
	_, values = bridge.ReasoningOptions("kimi")
	if len(values) != 0 || bridge.ReasoningEffortSelectable("kimi", "high") {
		t.Fatal("live advertisement overrode explicitly disabled reasoning")
	}
}

var errCatalog = errString("catalog failed")

type errString string

func (e errString) Error() string { return string(e) }
