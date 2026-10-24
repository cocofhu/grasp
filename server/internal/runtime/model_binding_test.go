package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestApplyModelBindingKeepsSecretSeparateAndClearsPreviousSource(t *testing.T) {
	env := map[string]string{"ANTHROPIC_API_KEY": "old", "OPENAI_BASE_URL": "old", "GITHUB_TOKEN": "git", "MCP_URL": "mcp", "CODEBUDDY_BIG_SLOW_MODEL": "old-model", "CLAUDE_CODE_USE_VERTEX": "1"}
	binding := &modelrouting.Binding{Kind: "connection", ConnectionID: "connection-a", ModelID: "vendor/raw/model"}
	resolve := func(backend string, got *modelrouting.Binding) (modelrouting.Resolved, error) {
		if backend != "codex" || got != binding {
			t.Fatal("incorrect binding resolution")
		}
		return modelrouting.Resolved{Binding: *got, CredentialID: "key-a", Secret: "private-key-value", Connection: &modelrouting.Connection{
			ID: got.ConnectionID, Protocol: modelrouting.ProtocolResponses, BaseURL: "https://models.example/v1", Auth: modelrouting.Auth{Scheme: "bearer"}, Models: []modelrouting.Model{{ID: got.ModelID}},
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
	if id := ModelCredentialIDForWriteBack(env, "old-key"); id != "key-a" {
		t.Fatalf("credential owner = %s", id)
	}
}

func TestApplyModelBindingFailureDoesNotFallback(t *testing.T) {
	binding := &modelrouting.Binding{Kind: "connection", ConnectionID: "revoked"}
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

func TestApplyModelBindingPlatformAndLegacyModes(t *testing.T) {
	env := map[string]string{EnvModelAPIKey: "old", EnvModelConnection: "old", "CURSOR_API_KEY": "other"}
	binding := &modelrouting.Binding{Kind: "platform", CredentialID: "cursor-key", ModelID: "auto"}
	if err := ApplyModelBinding(env, "cursor", binding, func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{Binding: *binding, CredentialID: binding.CredentialID, Secret: "cursor-secret"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if env[CredentialEnvKeyForBackend("cursor")] != "cursor-secret" || env[EnvModelAPIKey] != "" || env["CURSOR_API_KEY"] != "" {
		t.Fatal("previous source survived platform selection")
	}
	if err := ApplyModelBinding(env, "cursor", nil, nil); err != nil {
		t.Fatal(err)
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
	root := writeAgent(t, "bound", `{"acpBackend":"codex","modelBinding":{"kind":"connection","connectionId":"selected","modelId":"vendor/raw/model"},"env":{"GRASP_MODEL_CONNECTION":"forged-agent"}}`)
	p := newBaseACPProvider(mcp.NewHost(newMemStore()), Options{
		ProfilesRoot: root,
		Env:          map[string]string{EnvModelConnection: "forged-platform"},
		Credentials:  func() map[string]string { return map[string]string{envauth.EnvCodexAuthFile: chatgptLogin} },
		RunSandboxEnvForRun: func(string) []models.EnvEntry {
			return []models.EnvEntry{{Key: EnvModelConnection, Value: "forged-run"}, {Key: EnvModelAPIKey, Value: "forged-run-key"}}
		},
		ModelBinding: func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
			return modelrouting.Resolved{Binding: *binding, CredentialID: "selected-key", Secret: "selected-secret", Connection: &modelrouting.Connection{ID: binding.ConnectionID, Protocol: modelrouting.ProtocolResponses, BaseURL: "https://selected.example/v1", Auth: modelrouting.Auth{Scheme: "bearer"}, Models: []modelrouting.Model{{ID: binding.ModelID}}}}, nil
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

func TestSpecLegacySelectionDoesNotBypassDeletedConnection(t *testing.T) {
	root := writeAgent(t, "legacy", `{"acpBackend":"opencode","openCodeCredentialId":"old-key","env":{"ACP_BRIDGE_MODEL":"vendor/raw"}}`)
	mapped := false
	p := newBaseACPProvider(mcp.NewHost(newMemStore()), Options{
		ProfilesRoot: root,
		LegacyModelBinding: func(backend, _, id string, env map[string]string) *modelrouting.Binding {
			mapped = backend == "opencode" && id == "old-key" && env[EnvACPBridgeModel] == "vendor/raw"
			return &modelrouting.Binding{Kind: "connection", ConnectionID: "conn-legacy-old-key", ModelID: "raw"}
		},
		ModelBinding: func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
			return modelrouting.Resolved{}, errors.New("model connection not found")
		},
	}, BackendOpenCode).(*acpProvider)
	_, err := p.spec(NodeReq{RunID: "legacy-deleted", NodeID: "n", Config: map[string]any{"agent_profile": "legacy"}})
	if !mapped || err == nil || !strings.Contains(err.Error(), "connection not found") {
		t.Fatalf("legacy selection bypassed connection validation: mapped=%v err=%v", mapped, err)
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
