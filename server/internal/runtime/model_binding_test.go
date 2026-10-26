package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestConfiguredModelsCarryExactEffortsWithoutExpandingModels(t *testing.T) {
	no := false
	resolved := modelrouting.Resolved{Backend: "deepseek_harness", Binding: modelrouting.Binding{ModelID: "kimi", AuxiliaryModelIDs: []string{"fixed"}}, Connection: &modelrouting.Connection{ProviderID: "moonshot", Models: []modelrouting.Model{{ID: "kimi", ReasoningEfforts: []string{"quiet", "deep"}}, {ID: "fixed", Capabilities: modelrouting.ModelCapabilities{Reasoning: &no}}, {ID: "not-selected", ReasoningEfforts: []string{"ultra"}}}}}
	raw, err := configuredModelsJSON(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var items []struct {
		ID                string   `json:"id"`
		ReasoningEfforts  []string `json:"reasoningEfforts"`
		ReasoningDisabled bool     `json:"reasoningDisabled"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "kimi" || strings.Join(items[0].ReasoningEfforts, ",") != "quiet,deep" || items[1].ID != "fixed" || len(items[1].ReasoningEfforts) != 0 || !items[1].ReasoningDisabled {
		t.Fatalf("wrong model capabilities: %s", raw)
	}
}

func TestApplyModelBindingKeepsSecretSeparateAndClearsPreviousSource(t *testing.T) {
	env := map[string]string{"ANTHROPIC_API_KEY": "old", "OPENAI_BASE_URL": "old", "GITHUB_TOKEN": "git", "MCP_URL": "mcp", "CODEBUDDY_BIG_SLOW_MODEL": "old-model", "CLAUDE_CODE_USE_VERTEX": "1"}
	binding := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "daily"}
	resolve := func(backend string, got *modelrouting.Binding) (modelrouting.Resolved, error) {
		if backend != "codex" || got != binding {
			t.Fatal("incorrect binding resolution")
		}
		return modelrouting.Resolved{Binding: modelrouting.Binding{Kind: "connection", ConnectionID: "connection-a", ModelID: "vendor/raw/model"}, CredentialID: "key-a", Secret: "private-key-value", Connection: &modelrouting.Connection{
			ID: "connection-a", Protocol: modelrouting.ProtocolResponses, BaseURL: "https://models.example/v1", Auth: modelrouting.Auth{Scheme: "bearer"}, Models: []modelrouting.Model{{ID: "vendor/raw/model"}},
		}}, nil
	}
	if err := ApplyModelBinding(env, "codex", binding, resolve); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env[EnvModelConnection], "private-key-value") || strings.Contains(env[EnvModelConnection], "key-a") {
		t.Fatal("credential leaked into connection metadata")
	}
	if env[EnvModelAPIKey] != "private-key-value" || env[EnvACPBridgeModel] != "vendor/raw/model" {
		t.Fatal("selected secret or raw model was lost")
	}
	if env["ANTHROPIC_API_KEY"] != "" || env["OPENAI_BASE_URL"] != "" || env["CODEBUDDY_BIG_SLOW_MODEL"] != "" || env["CLAUDE_CODE_USE_VERTEX"] != "" || env["GITHUB_TOKEN"] != "git" || env["MCP_URL"] != "mcp" {
		t.Fatalf("incorrect environment isolation: %v", env)
	}
	if _, err := PrepareBoundAuth(BackendCodex, env, ""); err != nil {
		t.Fatalf("connection mode required a platform login: %v", err)
	}
	if id := ModelCredentialIDForWriteBack(env); id != "key-a" {
		t.Fatalf("credential owner = %s", id)
	}
}

func TestApplyModelBindingFailureDoesNotFallback(t *testing.T) {
	binding := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "revoked"}
	env := map[string]string{"OPENAI_API_KEY": "unrelated"}
	if err := ApplyModelBinding(env, "codex", binding, nil); err == nil {
		t.Fatal("missing resolver accepted")
	}
	if err := ApplyModelBinding(env, "codex", binding, func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{}, errors.New("credential revoked")
	}); err == nil {
		t.Fatal("revoked credential accepted")
	}
	if _, err := PrepareBoundAuth(BackendCodex, map[string]string{EnvModelConnection: "{}"}, ""); err == nil {
		t.Fatal("connection without a key accepted")
	}
}

func TestApplyOfficialToolBindingReplacesConnectionAndOtherToolCredentials(t *testing.T) {
	for _, tool := range []string{"opencode", "deepseek_harness"} {
		t.Run(tool, func(t *testing.T) {
			env := map[string]string{
				EnvModelConnection: "old-connection", EnvModelAPIKey: "old-generic-key",
				envauth.EnvOpenCodeAPIKey: "old-zen-key", envauth.EnvDeepSeekAPIKey: "old-deepseek-key",
				"OPENCODE_API_KEY": "old-zen-key", "DEEPSEEK_API_KEY": "old-deepseek-key",
				"GRASP_OPENCODE_PROVIDER": "third-party", "GRASP_OPENCODE_BASE_URL": "https://example.test/v1", "GITHUB_TOKEN": "git-test-key",
			}
			ref := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "official"}
			if err := ApplyModelBinding(env, tool, ref, func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
				return modelrouting.Resolved{Backend: tool, CredentialID: "selected", Secret: "selected-test-key", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "selected"}}, nil
			}); err != nil {
				t.Fatal(err)
			}
			key := CredentialEnvKeyForBackend(tool)
			if key == "" || env[key] != "selected-test-key" || env[EnvModelCredentialID] != "selected" || env[EnvModelConnection] != "" || env[EnvModelAPIKey] != "" || env["GRASP_OPENCODE_BASE_URL"] != "" || env["GITHUB_TOKEN"] != "git-test-key" {
				t.Fatal("official selection did not replace the previous model route")
			}
			prepared, err := PrepareBoundAuth(AcpBackend(tool), env, "")
			if err != nil {
				t.Fatal(err)
			}
			if tool == "opencode" {
				if prepared["OPENCODE_API_KEY"] != "selected-test-key" || prepared["GRASP_OPENCODE_PROVIDER"] != "opencode" || prepared["DEEPSEEK_API_KEY"] != "" || prepared[envauth.EnvDeepSeekAPIKey] != "" {
					t.Fatal("Zen selection retained another provider")
				}
			} else if prepared["DEEPSEEK_API_KEY"] != "selected-test-key" || prepared["OPENCODE_API_KEY"] != "" || prepared["GRASP_OPENCODE_PROVIDER"] != "" || prepared[envauth.EnvOpenCodeAPIKey] != "" {
				t.Fatal("DeepSeek selection retained another provider")
			}
		})
	}
}

func TestApplyModelBindingUsesPrimaryAndListsAuxiliaryModels(t *testing.T) {
	env := map[string]string{EnvInitialChatModel: "forged", EnvConfiguredModels: "forged"}
	binding := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "daily"}
	resolve := func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{
			Backend: "cursor", CredentialID: "cursor-key", Secret: "cursor-secret",
			Binding: modelrouting.Binding{Kind: "platform", ModelID: "", AuxiliaryModelIDs: []string{"grok-4.7", "composer-2.5"}},
		}, nil
	}
	if err := ApplyModelBinding(env, "cursor", binding, resolve); err != nil {
		t.Fatal(err)
	}
	if env[EnvACPBridgeModel] != "" {
		t.Fatalf("empty primary fell back to an auxiliary model: %q", env[EnvACPBridgeModel])
	}
	if env[EnvInitialChatModel] != "" {
		t.Fatal("automation injected a chat selection")
	}
	if !strings.Contains(env[EnvConfiguredModels], `"id":""`) || !strings.Contains(env[EnvConfiguredModels], "grok-4.7") || !strings.Contains(env[EnvConfiguredModels], "composer-2.5") {
		t.Fatalf("allow-list: %s", env[EnvConfiguredModels])
	}
	if strings.Index(env[EnvConfiguredModels], "grok-4.7") > strings.Index(env[EnvConfiguredModels], "composer-2.5") {
		t.Fatal("auxiliary order was not preserved")
	}

	env = map[string]string{}
	resolve = func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{
			Backend: "opencode", CredentialID: "key", Secret: "secret",
			Binding:    modelrouting.Binding{Kind: "connection", ConnectionID: "c", ModelID: "primary", AuxiliaryModelIDs: []string{"aux"}},
			Connection: &modelrouting.Connection{ID: "c", ProviderID: "deepseek", Protocol: modelrouting.ProtocolChat, Models: []modelrouting.Model{{ID: "primary"}, {ID: "aux"}, {ID: "unused"}}},
		}, nil
	}
	if err := ApplyModelBinding(env, "opencode", binding, resolve); err != nil {
		t.Fatal(err)
	}
	if env[EnvACPBridgeModel] != "primary" || !strings.Contains(env[EnvModelConnection], `"selectedModelId":"primary"`) || strings.Contains(env[EnvModelConnection], `"selectedModelId":"aux"`) {
		t.Fatalf("connection automation used a non-primary model: ACP=%q connection=%s", env[EnvACPBridgeModel], env[EnvModelConnection])
	}
	if !strings.Contains(env[EnvConfiguredModels], "aux") || !strings.Contains(env[EnvConfiguredModels], "deepseek") {
		t.Fatalf("connection allow-list: %s", env[EnvConfiguredModels])
	}
}

func TestApplyModelBindingPlatformConfiguration(t *testing.T) {
	env := map[string]string{EnvModelAPIKey: "old", EnvModelConnection: "old", "CURSOR_API_KEY": "other"}
	binding := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "cursor-daily"}
	if err := ApplyModelBinding(env, "cursor", binding, func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{Binding: modelrouting.Binding{Kind: "platform", CredentialID: "cursor-key", ModelID: "auto"}, CredentialID: "cursor-key", Secret: "cursor-secret"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if env[CredentialEnvKeyForBackend("cursor")] != "cursor-secret" || env[EnvModelAPIKey] != "" || env["CURSOR_API_KEY"] != "" {
		t.Fatal("previous source survived platform selection")
	}
	if err := ApplyModelBinding(env, "cursor", nil, nil); err == nil {
		t.Fatal("unconfigured Agent accepted")
	}
	if _, err := PrepareBoundAuth(BackendCodex, map[string]string{}, ""); !errors.Is(err, ErrCodexLoginFileMissing) {
		t.Fatalf("platform login gate was lost: %v", err)
	}
}

func TestCodexConnectionAuthErrorDoesNotRequestPlatformLogin(t *testing.T) {
	c := &acpProvider{backend: BackendCodex}
	req := NodeReq{RunID: "connection-auth-error", NodeID: "node"}
	key := c.backendKey(codexLoginKey(req))
	codexConnectionRuns.Store(key, true)
	t.Cleanup(func() { codexConnectionRuns.Delete(key) })
	result := &sandbox.ChatResult{Failed: true, ErrorText: "401 Unauthorized at https://api.openai.com/v1/responses"}
	if err := c.codexTurnLoginErr(result, req); err != nil {
		t.Fatal("connection error was rewritten as a platform login error")
	}
	upstream := errors.New(result.ErrorText)
	if err := c.settleCodexLogin(req, nil, upstream, nil); err != upstream {
		t.Fatalf("connection error changed: %v", err)
	}
	if _, exists := codexConnectionRuns.Load(key); exists {
		t.Fatal("attempt mode not cleared")
	}
}

func TestSpecResolvesBindingAfterAllUserEnvironmentSources(t *testing.T) {
	root := writeAgent(t, "bound", `{"acpBackend":"codex","modelBinding":{"kind":"configuration","configurationId":"selected"},"env":{"GRASP_MODEL_CONNECTION":"forged-agent"}}`)
	p := newBaseACPProvider(mcp.NewHost(newMemStore()), Options{
		ProfilesRoot: root,
		Env:          map[string]string{EnvModelConnection: "forged-platform"},
		RunSandboxEnvForRun: func(string) []models.EnvEntry {
			return []models.EnvEntry{{Key: EnvModelConnection, Value: "forged-run"}, {Key: EnvModelAPIKey, Value: "forged-run-key"}}
		},
		ModelBinding: func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
			return modelrouting.Resolved{Binding: modelrouting.Binding{Kind: "connection", ConnectionID: "selected", ModelID: "vendor/raw/model"}, CredentialID: "selected-key", Secret: "selected-secret", Connection: &modelrouting.Connection{ID: "selected", Protocol: modelrouting.ProtocolResponses, BaseURL: "https://selected.example/v1", Auth: modelrouting.Auth{Scheme: "bearer"}, Models: []modelrouting.Model{{ID: "vendor/raw/model"}}}}, nil
		},
	}, BackendCodex).(*acpProvider)
	if got := p.agentConfig("bound"); got.ModelBinding == nil {
		t.Fatalf("agent config not read: %+v (root %s)", got, root)
	}
	req := NodeReq{RunID: "binding-spec", NodeID: "n", WorkflowID: "wf", Token: "token", NodeType: "agent", Caps: testPlainCaps, Config: map[string]any{"agent_profile": "bound"}}
	spec, err := p.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeHome(spec.ConfigHome); codexConnectionRuns.Delete(p.backendKey(codexLoginKey(req))) })
	if spec.Env[EnvModelAPIKey] != "selected-secret" || !strings.Contains(spec.Env[EnvModelConnection], "https://selected.example/v1") {
		t.Fatal("selected connection was overwritten")
	}
	if spec.Env[envauth.EnvCodexAuthFile] != "" {
		t.Fatal("unselected platform login survived")
	}
	p.opts.ModelBinding = func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{}, errors.New("selected key revoked")
	}
	if _, err := p.spec(req); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatal("execution did not revalidate the selected key")
	}
}

func TestSpecRequiresExplicitNamedConfiguration(t *testing.T) {
	for _, raw := range []string{
		`{"acpBackend":"opencode","modelBinding":null}`,
		`{"acpBackend":"cursor","modelBinding":{"kind":"platform","credentialId":"direct"}}`,
		`{"acpBackend":"opencode","modelBinding":{"kind":"connection","connectionId":"direct","modelId":"m"}}`,
		`{"acpBackend":"cursor","modelBinding":{"kind":"inherit"}}`,
	} {
		root := writeAgent(t, "unconfigured", raw)
		called := false
		p := newBaseACPProvider(mcp.NewHost(newMemStore()), Options{ProfilesRoot: root, ModelBinding: func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
			called = true
			return modelrouting.Resolved{}, nil
		}}, BackendOpenCode).(*acpProvider)
		_, err := p.spec(NodeReq{Config: map[string]any{"agent_profile": "unconfigured"}})
		if err == nil || !strings.Contains(err.Error(), "model configuration") || called {
			t.Fatalf("invalid Agent binding reached resolver: called=%v err=%v", called, err)
		}
	}
}

func TestSpecNamedConfigurationResolvesCurrentModelAndRejectsMissingConfiguration(t *testing.T) {
	root := writeAgent(t, "named", `{"acpBackend":"codex","modelBinding":{"kind":"configuration","configurationId":"daily"}}`)
	selected := "vendor/main"
	p := newBaseACPProvider(mcp.NewHost(newMemStore()), Options{
		ProfilesRoot: root,
		ModelBinding: func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
			if backend != "codex" || binding.Kind != "configuration" || binding.ConfigurationID != "daily" {
				t.Fatal("named model reference lost before resolution")
			}
			if selected == "" {
				return modelrouting.Resolved{}, errors.New("model configuration not found")
			}
			return modelrouting.Resolved{Binding: modelrouting.Binding{Kind: "connection", ConnectionID: "service", ModelID: selected}, CredentialID: "key", Secret: "selected-key", Connection: &modelrouting.Connection{ID: "service", Protocol: modelrouting.ProtocolResponses, BaseURL: "https://models.example/v1", Auth: modelrouting.Auth{Scheme: "bearer"}, Models: []modelrouting.Model{{ID: selected}}}}, nil
		},
	}, BackendCodex).(*acpProvider)
	req := NodeReq{RunID: "named-config", NodeID: "n", Caps: testPlainCaps, Config: map[string]any{"agent_profile": "named"}}
	for _, model := range []string{"vendor/main", "vendor/updated"} {
		selected = model
		spec, err := p.spec(req)
		if err != nil {
			t.Fatal(err)
		}
		if spec.Env[EnvACPBridgeModel] != model || spec.Env[EnvModelAPIKey] != "selected-key" {
			t.Fatal("named configuration did not use its current model and credential")
		}
		removeHome(spec.ConfigHome)
	}
	t.Cleanup(func() { codexConnectionRuns.Delete(p.backendKey(codexLoginKey(req))) })
	selected = ""
	if _, err := p.spec(req); err == nil || !strings.Contains(err.Error(), "configuration not found") {
		t.Fatal("missing named configuration fell back")
	}
}
