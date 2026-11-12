package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/services"
)

func TestAgentHTTPHasNoGitAuthorization(t *testing.T) {
	h := newHarness(t)
	selection := map[string]any{"items": []any{map[string]any{"credentialId": "gh"}}}
	w := h.do("POST", "/api/agents", map[string]any{"name": "git-http", "inheritModel": false, "acpBackend": "cursor", "gitBindings": selection, "inheritGit": false})
	if w.Code != 201 {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"gitBindings", "inheritGit", "repoIds"} {
		if _, exists := got[key]; exists {
			t.Fatalf("Agent response still exposes %s", key)
		}
	}
}

func TestSharedConfigurationExposesExplicitModelDefaults(t *testing.T) {
	h := newHarness(t)
	setSecretsKey(t)
	seedOnboardingModel(t, h)
	resolve := func(binding *modelrouting.Binding) (string, error) {
		cfg, err := services.NewModelConfigurationService(h.db).Get(binding.ConfigurationID)
		if err != nil {
			return "", err
		}
		_, err = services.NewModelConnectionService(h.db).Resolve(cfg.Backend, *binding)
		return cfg.Backend, err
	}
	h.h.SharedAgent.SetModelResolver(resolve)
	h.h.Agents.SetSharedAgent(h.h.SharedAgent)
	w := h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"env": map[string]string{"FEATURE": "keep", "ACP_BRIDGE_MODEL": "ignored"}})
	if w.Code != 200 {
		t.Fatalf("put = %d %s", w.Code, w.Body.String())
	}
	w = h.do("GET", "/api/workspace/shared-agent-config", nil)
	if w.Code != 200 {
		t.Fatalf("get = %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"repoIds", "aiCredentialId", "openCodeCredentialId", "gitBindings"} {
		if _, exists := got[key]; exists {
			t.Fatalf("shared config still exposed %s", key)
		}
	}
	env := got["env"].(map[string]any)
	if env["FEATURE"] != "keep" || env["ACP_BRIDGE_MODEL"] != nil {
		t.Fatal("shared config did not restrict environment to non-model settings")
	}
	w = h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"acpBackend": "codex", "modelBinding": map[string]any{"kind": "configuration", "configurationId": "model-onboarding"}})
	if w.Code != 200 {
		t.Fatalf("save defaults = %d %s", w.Code, w.Body.String())
	}
	var saved services.SharedAgentConfig
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.AcpBackend != "cursor" || saved.ModelBinding == nil || saved.ModelBinding.ConfigurationID != "model-onboarding" {
		t.Fatal("defaults lost references or accepted an independent CLI")
	}
	w = h.do("PUT", "/api/workspace/shared-agent-config", map[string]any{"modelBinding": map[string]any{"kind": "configuration", "configurationId": "missing"}})
	if w.Code != 400 || h.h.SharedAgent.Get().ModelBinding.ConfigurationID != "model-onboarding" {
		t.Fatal("invalid default model did not return 400 before overwriting defaults")
	}
	w = h.do("POST", "/api/agents", map[string]any{"name": "inherit-defaults"})
	if w.Code != 201 {
		t.Fatalf("inherited Agent = %d %s", w.Code, w.Body.String())
	}
	agent, _ := h.h.Agents.Get("inherit-defaults")
	if !agent.InheritModel || agent.ModelBinding != nil {
		t.Fatal("missing switches did not use defaults or copied the default model")
	}
	effective := h.h.Agents.Effective(agent)
	if effective.AcpBackend != "cursor" || effective.ModelBinding.ConfigurationID != "model-onboarding" {
		t.Fatal("inherited Agent did not resolve defaults")
	}
	w = h.do("PUT", "/api/agents/inherit-defaults", map[string]any{"inheritModel": false, "acpBackend": "cursor", "modelBinding": map[string]any{"kind": "configuration", "configurationId": "model-onboarding"}})
	if w.Code != 200 {
		t.Fatalf("save local choice = %d %s", w.Code, w.Body.String())
	}
	agent, _ = h.h.Agents.Get("inherit-defaults")
	if agent.InheritModel {
		t.Fatal("explicit false was not persisted independently of defaults")
	}
	w = h.do("PUT", "/api/agents/inherit-defaults", map[string]any{"acpBackend": "cursor"})
	if w.Code != 200 {
		t.Fatalf("reset to omitted defaults = %d %s", w.Code, w.Body.String())
	}
	agent, _ = h.h.Agents.Get("inherit-defaults")
	if !agent.InheritModel || h.h.Agents.Effective(agent).ModelBinding.ConfigurationID != "model-onboarding" {
		t.Fatal("save with omitted switches did not use current defaults")
	}
}

func TestAgentMissingSwitchesRejectAbsentWorkspaceDefaultModel(t *testing.T) {
	h := newHarness(t)
	w := h.do("POST", "/api/agents", map[string]any{"name": "default-agent", "acpBackend": "cursor", "modelBinding": map[string]any{"kind": "configuration", "configurationId": "local-model"}})
	if w.Code != 400 || h.h.Agents.Exists("default-agent") {
		t.Fatalf("missing default used a local reference or wrote partial state: %d", w.Code)
	}
}
