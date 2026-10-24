package modelrouting

import "testing"

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
