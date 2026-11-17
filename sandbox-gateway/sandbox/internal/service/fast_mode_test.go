package service

import (
	"encoding/json"
	"errors"
	"testing"

	"backend/internal/provider"
)

func TestFastModeRequiresExactDeclarationAndCodexAdapter(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv(envConfiguredModels, `[{"id":"supported","fastModeSupported":true},{"id":"standard"}]`)
	if !FastModeSupported(" supported ") || FastModeSupported("supported-extra") || FastModeSupported("standard") || FastModeSupported("") {
		t.Fatal("Fast mode did not use exact declared capabilities")
	}
	rows, _, err := ChatModelList()
	if err != nil || len(rows) != 2 || !rows[0].FastModeSupported || rows[1].FastModeSupported {
		t.Fatalf("capability projection = %+v, %v", rows, err)
	}
	t.Setenv("AGENT_PROVIDER", "cursor")
	if FastModeSupported("supported") {
		t.Fatal("Fast mode leaked into an adapter without a service tier")
	}
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv(envConfiguredModels, "{invalid")
	if FastModeSupported("supported") {
		t.Fatal("invalid metadata enabled Fast mode")
	}
}

func TestFastModeRestartRollbackAndChatIsolation(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv("ACP_BRIDGE_FAST_MODE", "false")
	t.Setenv(envConfiguredModels, `[{"id":"original","fastModeSupported":true},{"id":"next","fastModeSupported":true},{"id":"standard"}]`)
	manager := NewChatManager()
	manager.SetDefaultModel("original")
	bridge, err := manager.Create("", "original")
	if err != nil {
		t.Fatal(err)
	}
	bridge.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		if bridge.Model() != "next" || bridge.ReasoningEffort() != "ultra" || !bridge.FastMode() {
			t.Fatal("restart did not observe the complete model, effort and speed choice")
		}
		return nil, errors.New("fixture restart rejected")
	}
	if _, err := bridge.SwitchModelSettings("next", "ultra", true); err == nil {
		t.Fatal("expected restart failure")
	}
	if bridge.Model() != "original" || bridge.FastMode() {
		t.Fatal("restart failure did not restore the original speed and model")
	}
	bridge.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		return &stubSess{id: "next"}, nil
	}
	if _, err := bridge.SwitchModelSettings("next", "ultra", true); err != nil {
		t.Fatal(err)
	}
	if !bridge.FastMode() || manager.Default().FastMode() {
		t.Fatal("speed selection leaked between chats")
	}
	if _, err := bridge.SwitchModelSettings("standard", "ultra"); err != nil || bridge.FastMode() {
		t.Fatalf("unsupported target retained Fast mode: %v", err)
	}
	if _, err := bridge.SwitchModelSettings("standard", "ultra", true); !errors.Is(err, ErrFastModeUnavailable) || bridge.FastMode() {
		t.Fatalf("unsupported explicit Fast was accepted: %v", err)
	}
	t.Setenv("ACP_BRIDGE_FAST_MODE", "true")
	booted := NewBridge()
	booted.SetModel("original")
	if !booted.FastMode() {
		t.Fatal("restored Fast environment was ignored")
	}
}
