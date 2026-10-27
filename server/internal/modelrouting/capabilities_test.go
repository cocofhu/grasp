package modelrouting

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCapabilityBoundaries(t *testing.T) {
	for _, capability := range Capabilities() {
		if capability.ToolID == "" || len(capability.Evidence) == 0 || capability.Verification != "not-run" {
			t.Fatalf("incomplete capability: %+v", capability)
		}
		if _, ok := CapabilityFor(capability.ToolID); !ok {
			t.Fatal("missing tool")
		}
	}
	if _, ok := CapabilityFor("unknown"); ok {
		t.Fatal("unknown tool was supported")
	}
	connection := Connection{ID: "c", ProviderID: "deepseek", Protocol: ProtocolAnthropic, BaseURL: "https://api.deepseek.com/anthropic/", Auth: Auth{Scheme: "x-api-key"}}
	binding := Binding{Kind: "connection", ConnectionID: "c", ModelID: "vendor/model"}
	for _, tool := range []string{"claude_code", "opencode", "deepseek_harness"} {
		if err := ValidateBinding(tool, binding, &connection); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	for _, tool := range []string{"cursor", "trae", "codebuddy", "codex", "unknown"} {
		if err := ValidateBinding(tool, binding, &connection); err == nil {
			t.Fatalf("unsupported %s accepted", tool)
		}
	}
	binding.ToolSettings.RouteProfile = "deepseek-native"
	if err := ValidateBinding("deepseek_harness", binding, &connection); err != nil {
		t.Fatal(err)
	}
	connection.ProviderID = "custom"
	if err := ValidateBinding("deepseek_harness", binding, &connection); err == nil {
		t.Fatal("native route accepted a third party")
	}
	connection.ProviderID, binding.ToolSettings.RouteProfile = "deepseek", ""
	connection.Auth.Scheme = "bearer"
	if err := ValidateBinding("claude_code", binding, &connection); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBinding("codebuddy", binding, &connection); err == nil {
		t.Fatal("unsupported bearer messages accepted")
	}
	connection.Protocol = ProtocolResponses
	if err := ValidateBinding("codex", binding, &connection); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBinding("codebuddy", binding, &connection); err == nil {
		t.Fatal("CodeBuddy must not advertise an unimplemented Responses transport")
	}
	connection.Auth.Scheme = "x-api-key"
	if err := ValidateBinding("codex", binding, &connection); err == nil {
		t.Fatal("unsupported x-api-key responses accepted")
	}
}

func TestBindingsAreExclusiveAndResolvedJSONHidesSecrets(t *testing.T) {
	if err := ValidateBinding("cursor", Binding{Kind: "platform", CredentialID: "key", ToolSettings: ToolSettings{ModelRoles: map[string]string{"main": "m"}}}, nil); err == nil {
		t.Fatal("platform role mapping was silently ignored")
	}
	for _, binding := range []Binding{{Kind: "inherit"}, {Kind: "platform"}, {Kind: "platform", CredentialID: "key", ConnectionID: "c"}, {Kind: "platform", CredentialID: "key", ToolSettings: ToolSettings{RouteProfile: "native"}}, {Kind: "connection", ConnectionID: "c"}} {
		if err := ValidateBinding("claude_code", binding, nil); err == nil {
			t.Fatalf("invalid binding accepted: %+v", binding)
		}
	}
	connection := Connection{Protocol: ProtocolAnthropic, Auth: Auth{Scheme: "x-api-key"}}
	for _, role := range []string{"unsupported", "main"} {
		binding := Binding{Kind: "connection", ConnectionID: "c", ModelID: "a", ToolSettings: ToolSettings{ModelRoles: map[string]string{role: "b"}}}
		if err := ValidateBinding("claude_code", binding, &connection); err == nil {
			t.Fatal("invalid role accepted")
		}
	}
	connection.Auth.HeaderName = "X-Custom"
	if err := ValidateBinding("claude_code", Binding{Kind: "connection", ConnectionID: "c", ModelID: "m"}, &connection); err == nil {
		t.Fatal("custom auth accepted")
	}
	if err := ValidateBinding("cursor", Binding{Kind: "platform", CredentialID: "key"}, nil); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(Resolved{Secret: "never-expose-this"})
	if err != nil || strings.Contains(string(encoded), "never-expose-this") || strings.Contains(string(encoded), `"secret"`) {
		t.Fatal("resolved secret serialized")
	}
}

func TestOfficialToolPlatformBindings(t *testing.T) {
	for _, tool := range []string{"opencode", "deepseek_harness"} {
		if err := ValidateBinding(tool, Binding{Kind: "platform", CredentialID: "official-key"}, nil); err != nil {
			t.Fatalf("%s default: %v", tool, err)
		}
	}
	for _, id := range []string{"opencode/claude-sonnet-5", "opencode/kimi-k3"} {
		if err := ValidateBinding("opencode", Binding{Kind: "platform", CredentialID: "zen", ModelID: id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"anthropic/claude-sonnet-5", "opencode-go/kimi-k3", "custom/model", "opencode/", "opencode/vendor/model", "claude-sonnet-5"} {
		for _, auxiliary := range []bool{false, true} {
			binding := Binding{Kind: "platform", CredentialID: "zen", ModelID: id}
			if auxiliary {
				binding.ModelID, binding.AuxiliaryModelIDs = "opencode/kimi-k3", []string{id}
			}
			if err := ValidateBinding("opencode", binding, nil); err == nil {
				t.Fatalf("Zen key accepted another provider model %q (auxiliary=%v)", id, auxiliary)
			}
		}
	}
	connection := Connection{Protocol: ProtocolChat, Auth: Auth{Scheme: "bearer"}}
	if err := ValidateBinding("opencode", Binding{Kind: "connection", ConnectionID: "generic", ModelID: "vendor/raw/model"}, &connection); err != nil {
		t.Fatalf("generic connection model IDs changed: %v", err)
	}
}

func TestOpenCodeEffortBinding(t *testing.T) {
	platform := Binding{Kind: "platform", CredentialID: "zen", ModelID: "opencode/gpt-5.5", AuxiliaryModelIDs: []string{"opencode/kimi-k2.7-code"}, ToolSettings: ToolSettings{ModelEfforts: map[string]string{"opencode/gpt-5.5": "high"}}}
	if err := ValidateBinding("opencode", platform, nil); err != nil {
		t.Fatal(err)
	}
	platform.ToolSettings.ModelEfforts = map[string]string{"opencode/gpt-5.5": "turbo"}
	if err := ValidateBinding("opencode", platform, nil); err != nil {
		t.Fatal(err)
	}
	platform.ToolSettings.ModelEfforts = nil
	if err := ValidateBinding("opencode", platform, nil); err != nil {
		t.Fatal(err)
	}
	for _, effort := range []string{" ", "high#max", "bad/value", "low medium"} {
		platform.ToolSettings.ModelEfforts = map[string]string{"opencode/gpt-5.5": effort}
		if err := ValidateBinding("opencode", platform, nil); err == nil {
			t.Fatalf("accepted invalid effort %q", effort)
		}
	}
	platform.ToolSettings.ModelEfforts = map[string]string{"opencode/gpt-5.6-sol": "max"}
	if err := ValidateBinding("opencode", platform, nil); err == nil {
		t.Fatal("accepted an effort for a model that is not selected")
	}
	if err := ValidateBinding("cursor", Binding{Kind: "platform", CredentialID: "key", ToolSettings: ToolSettings{ModelEfforts: map[string]string{"gpt-5.5": "high"}}}, nil); err == nil {
		t.Fatal("cursor accepted an OpenCode effort")
	}
	connection := Connection{Protocol: ProtocolChat, Auth: Auth{Scheme: "bearer"}, ProviderID: "tokenhub"}
	saved := Binding{Kind: "connection", ConnectionID: "hub", ModelID: "deepseek-v4-flash", ToolSettings: ToolSettings{ModelEfforts: map[string]string{"deepseek-v4-flash": "low"}}}
	if err := ValidateBinding("opencode", saved, &connection); err != nil {
		t.Fatal(err)
	}
	saved.ToolSettings.ModelEfforts = map[string]string{"deepseek-v4-pro": "low"}
	saved.ModelID = "deepseek-v4-pro"
	if err := ValidateBinding("opencode", saved, &connection); err != nil {
		t.Fatal(err)
	}
	saved.ModelID = "vendor/unlisted"
	saved.ToolSettings.ModelEfforts = map[string]string{"vendor/unlisted": "custom-1"}
	if err := ValidateBinding("opencode", saved, &connection); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBinding("codex", saved, &connection); err == nil {
		t.Fatal("codex accepted a model effort")
	}
}
