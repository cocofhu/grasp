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
