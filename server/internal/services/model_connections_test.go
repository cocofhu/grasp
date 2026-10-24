package services

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
)

func newModelKey(t *testing.T, s *CredentialService, name string) CredentialView {
	t.Helper()
	credential, err := s.Create(CredentialInput{Purpose: "model_api", Issuer: "deepseek", Name: name, Value: "private-model-key"})
	if err != nil {
		t.Fatal(err)
	}
	return credential
}

func connectionInput(key string) modelrouting.Connection {
	return modelrouting.Connection{Name: "DeepSeek team", ProviderID: "deepseek", Protocol: modelrouting.ProtocolChat, BaseURL: "https://api.deepseek.com/v1/", CredentialID: key, Models: []modelrouting.Model{{ID: "vendor/model-with/slashes", Input: []string{"text"}, ContextWindow: 128000, MaxTokens: 8192}}}
}

func TestModelConnectionCRUDAndSharedCredential(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	credentials, service := NewCredentialService(db), NewModelConnectionService(db)
	key := newModelKey(t, credentials, "Model key")
	if key.Purpose != "model_api" || key.ToolID != "" || key.EnvKey != "" || key.SecretRevision != 1 {
		t.Fatalf("bad domain credential: %+v", key)
	}
	if env := credentials.ResolveEnv(); len(env) != 0 {
		t.Fatalf("model key globally injected: %v", env)
	}
	first, err := service.Create(connectionInput(key.ID))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(connectionInput(key.ID))
	if err != nil || first.ID == second.ID {
		t.Fatalf("same key cannot have separate connections: %v", err)
	}
	if first.BaseURL != "https://api.deepseek.com/v1" || first.Auth.Scheme != "bearer" || first.Models[0].ID != "vendor/model-with/slashes" {
		t.Fatalf("lost connection information: %+v", first)
	}
	items, err := service.List()
	if err != nil || len(items) != 2 {
		t.Fatalf("list: %+v %v", items, err)
	}
	first.Name, first.State = "Edited", "disabled"
	updated, err := service.Update(first.ID, first)
	if err != nil || updated.Revision != 2 || updated.Name != "Edited" {
		t.Fatalf("update: %+v %v", updated, err)
	}
	stored, err := service.Get(first.ID)
	if err != nil || stored.State != "disabled" {
		t.Fatalf("get: %+v %v", stored, err)
	}
	serialized, _ := json.Marshal(items)
	if strings.Contains(string(serialized), "private-model-key") || strings.Contains(string(serialized), "valueEnc") {
		t.Fatal("connection API contains secret")
	}
	if err := service.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(first.ID); !errors.Is(err, ErrModelConnectionNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
	if _, err := service.Get(first.ID); !errors.Is(err, ErrModelConnectionNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if _, err := service.Update(first.ID, updated); !errors.Is(err, ErrModelConnectionNotFound) {
		t.Fatalf("update missing: %v", err)
	}
}

func TestModelConnectionResolveIsExplicitAndFailsClosed(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	credentials, service := NewCredentialService(db), NewModelConnectionService(db)
	key := newModelKey(t, credentials, "First")
	_ = newModelKey(t, credentials, "Second")
	connection, err := service.Create(connectionInput(key.ID))
	if err != nil {
		t.Fatal(err)
	}
	binding := modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: connection.Models[0].ID}
	resolved, err := service.Resolve("opencode", binding)
	if err != nil || resolved.Secret != "private-model-key" || resolved.CredentialID != key.ID || resolved.SecretRevision != 1 {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	encoded, _ := json.Marshal(resolved)
	if strings.Contains(string(encoded), "private-model-key") {
		t.Fatal("resolved secret escaped")
	}
	rotated, err := credentials.Update(key.ID, CredentialInput{Value: "rotation"})
	if err != nil || rotated.SecretRevision != 2 {
		t.Fatalf("rotate: %+v %v", rotated, err)
	}
	resolved, err = service.Resolve("opencode", binding)
	if err != nil || resolved.Secret != "rotation" || resolved.SecretRevision != 2 {
		t.Fatal("rotation was not resolved")
	}
	for _, test := range []struct {
		tool    string
		binding modelrouting.Binding
	}{
		{"cursor", binding}, {"trae", binding}, {"claude_code", binding},
		{"opencode", modelrouting.Binding{Kind: "inherit"}},
		{"opencode", modelrouting.Binding{Kind: "connection", ConnectionID: "gone", ModelID: "m"}},
		{"opencode", modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: "other"}},
		{"opencode", modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: binding.ModelID, ToolSettings: modelrouting.ToolSettings{ModelRoles: map[string]string{"fast": "other"}}}},
	} {
		if _, err := service.Resolve(test.tool, test.binding); err == nil {
			t.Fatalf("invalid binding executed: %+v", test)
		}
	}
	if err := credentials.Clear(key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve("opencode", binding); err == nil {
		t.Fatal("clear substituted another key")
	}
	if _, err := credentials.Update(key.ID, CredentialInput{Value: "restored"}); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Revoke(key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve("opencode", binding); err == nil {
		t.Fatal("revoked key resolved")
	}
}

func TestModelConnectionPlatformAndRoleValidation(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	credentials, service := NewCredentialService(db), NewModelConnectionService(db)
	platform, err := credentials.Create(CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Cursor account", Value: "cursor-key"})
	if err != nil {
		t.Fatal(err)
	}
	binding := modelrouting.Binding{Kind: "platform", CredentialID: platform.ID}
	if _, err := service.Resolve("cursor", binding); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve("trae", binding); err == nil {
		t.Fatal("platform credential crossed tools")
	}
	if _, err := service.Resolve("cursor", modelrouting.Binding{Kind: "platform", CredentialID: "missing"}); err == nil {
		t.Fatal("missing credential accepted")
	}
	input := connectionInput(platform.ID)
	if _, err := service.Create(input); err == nil {
		t.Fatal("tool authorization used as a model key")
	}
	key := newModelKey(t, credentials, "DSH key")
	input = connectionInput(key.ID)
	input.Protocol, input.BaseURL = modelrouting.ProtocolAnthropic, "https://api.deepseek.com/anthropic/"
	connection, err := service.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	binding = modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: input.Models[0].ID}
	if _, err := service.Resolve("deepseek_harness", binding); err != nil {
		t.Fatal(err)
	}
	binding.ToolSettings.RouteProfile = "deepseek-native"
	if _, err := service.Resolve("deepseek_harness", binding); err != nil {
		t.Fatal(err)
	}
	binding.ToolSettings = modelrouting.ToolSettings{ModelRoles: map[string]string{"haiku": input.Models[0].ID}}
	if _, err := service.Resolve("claude_code", binding); err != nil {
		t.Fatal(err)
	}
	connection.Models[0].ContextWindow = 0
	connection, err = service.Update(connection.ID, connection)
	if err != nil {
		t.Fatal(err)
	}
	binding.ToolSettings = modelrouting.ToolSettings{}
	if _, err := service.Resolve("deepseek_harness", binding); err != nil {
		t.Fatal("optional model catalog metadata should use adapter defaults", err)
	}
}

func TestModelConnectionInputValidation(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	key := newModelKey(t, NewCredentialService(db), "Key")
	service := NewModelConnectionService(db)
	cases := []func(*modelrouting.Connection){
		func(c *modelrouting.Connection) { c.Name = "" }, func(c *modelrouting.Connection) { c.ProviderID = "" },
		func(c *modelrouting.Connection) { c.Protocol = "unknown" }, func(c *modelrouting.Connection) { c.BaseURL = "https://user:secret@example.com/v1" },
		func(c *modelrouting.Connection) { c.BaseURL = "https://example.com?api_key=secret" }, func(c *modelrouting.Connection) { c.BaseURL = "file:///tmp/x" },
		func(c *modelrouting.Connection) { c.Auth.Scheme = "custom-header" }, func(c *modelrouting.Connection) { c.Auth.HeaderName = "Authorization" },
		func(c *modelrouting.Connection) { c.State = "unknown" }, func(c *modelrouting.Connection) { c.CredentialID = "missing" },
		func(c *modelrouting.Connection) { c.Models[0].ID = "" }, func(c *modelrouting.Connection) { c.Models = append(c.Models, c.Models[0]) },
		func(c *modelrouting.Connection) { c.Models[0].Source = "unknown" }, func(c *modelrouting.Connection) { c.Models[0].ContextWindow = -1 },
		func(c *modelrouting.Connection) { c.Models[0].Input = []string{"video"} },
	}
	for i, mutate := range cases {
		input := connectionInput(key.ID)
		mutate(&input)
		if _, err := service.Create(input); !errors.Is(err, ErrModelConnectionInvalid) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	input := connectionInput(key.ID)
	input.Models = nil
	created, err := service.Create(input)
	if err != nil || created.Models == nil || len(created.Models) != 0 {
		t.Fatalf("empty model catalog should be saveable: %+v %v", created, err)
	}
}

func TestLegacyDSHBindingUsesNativeModelService(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	credentials, service := NewCredentialService(db), NewModelConnectionService(db)
	key, err := credentials.Create(CredentialInput{Type: "ai", Provider: "deepseek_harness", Name: "Legacy DeepSeek", Value: "deepseek-key"})
	if err != nil || key.Purpose != "model_api" {
		t.Fatalf("DSH key incorrectly treated as platform account: %+v %v", key, err)
	}
	for _, model := range []string{"", "auto", "deepseek-official/deepseek-v4-pro", `["deepseek-official","deepseek-v4-pro"]`} {
		binding, err := service.LegacyBinding("deepseek_harness", key.ID, "", map[string]string{"ACP_BRIDGE_MODEL": model})
		if err != nil || binding.ToolSettings.RouteProfile != "deepseek-native" {
			t.Fatalf("native binding: %+v %v", binding, err)
		}
		if _, err := service.Resolve("deepseek_harness", *binding); err != nil {
			t.Fatal(err)
		}
	}
	if binding, err := service.LegacyBinding("cursor", "", "", nil); binding != nil || err != nil {
		t.Fatal("empty selection guessed a credential")
	}
	if binding, err := service.LegacyBinding("cursor", "account", "", map[string]string{"ACP_BRIDGE_MODEL": "gpt"}); err != nil || binding.CredentialID != "account" || binding.ModelID != "gpt" {
		t.Fatal("legacy platform mapping failed")
	}
}

func TestUnusedBuiltinCredentialDoesNotCreateModelConnection(t *testing.T) {
	service := NewCredentialService(newTestDB(t))
	if _, err := service.List(); err != nil {
		t.Fatal(err)
	}
	connections, err := NewModelConnectionService(service.db).List()
	if err != nil || len(connections) != 0 {
		t.Fatalf("empty slots became connections: %+v %v", connections, err)
	}
}

func TestLegacyOpenCodeKeepsItsNativeProviderWithoutInferredConnection(t *testing.T) {
	for _, provider := range []string{"google", "azure", "xai", "alibaba", "openai", "anthropic", "opencode", "custom"} {
		t.Run(provider, func(t *testing.T) {
			setCredentialKey(t)
			db := newTestDB(t)
			credentials, service := NewCredentialService(db), NewModelConnectionService(db)
			metadata := map[string]any{"provider": provider, "model": provider + "/vendor/raw-model", "vision": true}
			if provider == "custom" {
				metadata["baseUrl"] = "https://gateway.example/prefix/v1"
			}
			key, err := credentials.Create(CredentialInput{Type: "ai", Provider: "opencode", Name: "Original native provider", Value: "original-selected-key", Metadata: metadata})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := service.MigrateLegacyConnections(); err != nil {
					t.Fatal(err)
				}
				binding, err := service.LegacyBinding("opencode", "", key.ID, nil)
				if err != nil || binding != nil {
					t.Fatalf("native OpenCode selection was changed: %+v %v", binding, err)
				}
			}
			items, err := service.List()
			if err != nil || len(items) != 0 {
				t.Fatalf("provider protocol/endpoint was guessed: %+v %v", items, err)
			}
			original, err := credentials.Get(key.ID)
			if err != nil || original.Metadata["model"] != metadata["model"] || !original.Configured {
				t.Fatal("legacy metadata or key was changed")
			}
			agents := NewAgentService(t.TempDir())
			agents.SetModelBindingResolver(func(string, *modelrouting.Binding) error {
				t.Fatal("native provider required a new binding")
				return nil
			}, func(backend, ai, oc string, env map[string]string) *modelrouting.Binding {
				binding, _ := service.LegacyBinding(backend, ai, oc, env)
				return binding
			})
			if err := agents.Save(Agent{Name: "native", AcpBackend: "opencode", OpenCodeCredentialID: key.ID}); err != nil {
				t.Fatal(err)
			}
			got, _ := agents.Get("native")
			if got.ModelBinding != nil || got.OpenCodeCredentialID != key.ID {
				t.Fatal("legacy Agent selection was lost")
			}
			if err := credentials.Revoke(key.ID); err != nil {
				t.Fatal(err)
			}
			agents.ClearOpenCodeCredentialSelection(key.ID)
			got, _ = agents.Get("native")
			if got.OpenCodeCredentialID != key.ID || credentials.ResolveOpenCodeCredential(got.OpenCodeCredentialID) != nil {
				t.Fatal("revocation replaced the explicit native credential")
			}
		})
	}
}
