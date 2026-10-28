package modelrouting

import (
	"reflect"
	"testing"
)

func TestAgentConfigurationReference(t *testing.T) {
	for _, binding := range []*Binding{
		nil, {}, {Kind: "inherit"}, {Kind: "platform", CredentialID: "key"}, {Kind: "connection", ConnectionID: "conn", ModelID: "m"},
		{Kind: "configuration"}, {Kind: "configuration", ConfigurationID: "  "},
		{Kind: "configuration", ConfigurationID: "model", ModelID: "override"},
		{Kind: "configuration", ConfigurationID: "model", CredentialID: "override"},
		{Kind: "configuration", ConfigurationID: "model", ConnectionID: "override"},
		{Kind: "configuration", ConfigurationID: "model", ToolSettings: ToolSettings{RouteProfile: "override"}},
		{Kind: "configuration", ConfigurationID: "model", ToolSettings: ToolSettings{ModelRoles: map[string]string{"fast": "override"}}},
	} {
		if err := ValidateConfigurationReference(binding); err == nil {
			t.Errorf("invalid Agent configuration reference accepted: %+v", binding)
		}
	}
	if err := ValidateConfigurationReference(&Binding{Kind: "configuration", ConfigurationID: "daily"}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentConfigurationSetHasExplicitDefaultAndStableOrder(t *testing.T) {
	ref := &Binding{Kind: "configuration", ConfigurationID: "codex", ConfigurationIDs: []string{"claude", "codex", "opencode"}}
	if err := ValidateConfigurationReference(ref); err != nil {
		t.Fatal(err)
	}
	ids := ConfigurationIDs(ref)
	if !reflect.DeepEqual(ids, []string{"claude", "codex", "opencode"}) || !HasConfiguration(ref, "opencode") || HasConfiguration(ref, "other") {
		t.Fatalf("configuration set: %v", ids)
	}
	ids[0] = "mutated"
	if ref.ConfigurationIDs[0] != "claude" {
		t.Fatal("returned IDs alias saved binding")
	}
	for _, ids := range [][]string{{"claude"}, {"codex", "codex"}, {"codex", ""}, {"codex", " claude"}} {
		bad := *ref
		bad.ConfigurationIDs = ids
		if err := ValidateConfigurationReference(&bad); err == nil {
			t.Fatalf("invalid list accepted: %v", ids)
		}
	}
	if got := ConfigurationIDs(ConfigurationReference("single")); !reflect.DeepEqual(got, []string{"single"}) {
		t.Fatalf("old single binding: %v", got)
	}
	if ConfigurationIDs(nil) != nil || HasConfiguration(nil, "any") {
		t.Fatal("empty binding has configurations")
	}
	if err := ValidateBinding("codex", Binding{Kind: "platform", ConfigurationIDs: []string{"nested"}}, nil); err == nil {
		t.Fatal("concrete group accepted nested configuration references")
	}
}
