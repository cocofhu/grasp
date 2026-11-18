package service

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"backend/internal/provider"
)

func tierPtr(tier string) *string { return &tier }

func TestServiceTiersRequireExactDeclarationAndCodexAdapter(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv(envConfiguredModels, `[{"id":"supported","serviceTiers":["fast","turbo"]},{"id":"standard"}]`)
	if !slices.Equal(ServiceTiers(" supported "), []string{"fast", "turbo"}) || ServiceTiers("supported-extra") != nil || ServiceTiers("standard") != nil || ServiceTiers("") != nil {
		t.Fatal("service tiers did not use exact declared capabilities")
	}
	if !ServiceTierSelectable("supported", "turbo") || ServiceTierSelectable("supported", "ultra") || ServiceTierSelectable("standard", "fast") || !ServiceTierSelectable("standard", "") {
		t.Fatal("tier selection did not follow the declared list")
	}
	rows, _, err := ChatModelList()
	if err != nil || len(rows) != 2 || !slices.Equal(rows[0].ServiceTiers, []string{"fast", "turbo"}) || rows[1].ServiceTiers != nil {
		t.Fatalf("capability projection = %+v, %v", rows, err)
	}
	t.Setenv("AGENT_PROVIDER", "cursor")
	if ServiceTiers("supported") != nil {
		t.Fatal("service tiers leaked into an adapter without a speed tier")
	}
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv(envConfiguredModels, "{invalid")
	if ServiceTiers("supported") != nil {
		t.Fatal("invalid metadata enabled a service tier")
	}
}

func TestServiceTierRestartRollbackAndChatIsolation(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "codex")
	t.Setenv("ACP_BRIDGE_SERVICE_TIER", "default")
	t.Setenv(envConfiguredModels, `[{"id":"original","serviceTiers":["fast"]},{"id":"next","serviceTiers":["fast"]},{"id":"standard"}]`)
	manager := NewChatManager()
	manager.SetDefaultModel("original")
	bridge, err := manager.Create("", "original")
	if err != nil {
		t.Fatal(err)
	}
	bridge.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		if bridge.Model() != "next" || bridge.ReasoningEffort() != "ultra" || bridge.ServiceTier() != "fast" {
			t.Fatal("restart did not observe the complete model, effort and speed choice")
		}
		return nil, errors.New("fixture restart rejected")
	}
	if _, err := bridge.SwitchModelSettings("next", "ultra", tierPtr("fast")); err == nil {
		t.Fatal("expected restart failure")
	}
	if bridge.Model() != "original" || bridge.ServiceTier() != "" {
		t.Fatal("restart failure did not restore the original speed and model")
	}
	bridge.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		return &stubSess{id: "next"}, nil
	}
	if _, err := bridge.SwitchModelSettings("next", "ultra", tierPtr("fast")); err != nil {
		t.Fatal(err)
	}
	if bridge.ServiceTier() != "fast" || manager.Default().ServiceTier() != "" {
		t.Fatal("speed selection leaked between chats")
	}
	if _, err := bridge.SwitchModelSettings("next", "low", nil); err != nil || bridge.ServiceTier() != "fast" {
		t.Fatalf("omitted tier did not keep the current one: %v", err)
	}
	if _, err := bridge.SwitchModelSettings("next", "ultra", tierPtr("turbo")); !errors.Is(err, ErrServiceTierUnavailable) || bridge.ServiceTier() != "fast" {
		t.Fatalf("undeclared tier was accepted: %v", err)
	}
	if _, err := bridge.SwitchModelSettings("standard", "ultra", nil); err != nil || bridge.ServiceTier() != "" {
		t.Fatalf("unsupported target retained its service tier: %v", err)
	}
	if _, err := bridge.SwitchModelSettings("standard", "ultra", tierPtr("fast")); !errors.Is(err, ErrServiceTierUnavailable) || bridge.ServiceTier() != "" {
		t.Fatalf("unsupported explicit tier was accepted: %v", err)
	}
	t.Setenv("ACP_BRIDGE_SERVICE_TIER", "fast")
	booted := NewBridge()
	booted.SetModel("original")
	if booted.ServiceTier() != "fast" {
		t.Fatal("restored service tier environment was ignored")
	}
}
