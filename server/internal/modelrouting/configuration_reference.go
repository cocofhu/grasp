package modelrouting

import (
	"fmt"
	"strings"
)

// ValidateConfigurationReference checks the only model source an Agent may use.
// Concrete platform/connection bindings live inside saved configurations.
func ValidateConfigurationReference(binding *Binding) error {
	if binding == nil {
		return fmt.Errorf("select a model configuration before running this Agent")
	}
	if binding.Kind != "configuration" || strings.TrimSpace(binding.ConfigurationID) == "" || binding.ConfigurationID != strings.TrimSpace(binding.ConfigurationID) {
		return fmt.Errorf("Agent model binding must reference a saved model configuration; select a model configuration")
	}
	if binding.CredentialID != "" || binding.ConnectionID != "" || binding.ModelID != "" || len(binding.ToolSettings.ModelRoles) != 0 || binding.ToolSettings.RouteProfile != "" || len(binding.ToolSettings.ModelEfforts) != 0 {
		return fmt.Errorf("Agent model binding requires only configurationId")
	}
	if binding.AllConfigurations && len(binding.ConfigurationIDs) != 0 {
		return fmt.Errorf("Agent allConfigurations cannot be combined with configurationIds")
	}
	seen := map[string]bool{}
	for _, id := range binding.ConfigurationIDs {
		if id == "" || id != strings.TrimSpace(id) || seen[id] {
			return fmt.Errorf("Agent model configuration IDs must be nonempty and unique")
		}
		seen[id] = true
	}
	if len(binding.ConfigurationIDs) > 0 && !seen[binding.ConfigurationID] {
		return fmt.Errorf("Agent default model configuration must be in configurationIds")
	}
	return nil
}

// ConfigurationIDs returns explicitly bound groups in display order, including
// the default. Dynamic alternatives from AllConfigurations require a live list
// of saved configurations and are not expanded here.
func ConfigurationIDs(binding *Binding) []string {
	if binding == nil {
		return nil
	}
	if len(binding.ConfigurationIDs) > 0 {
		return append([]string(nil), binding.ConfigurationIDs...)
	}
	if binding.ConfigurationID != "" {
		return []string{binding.ConfigurationID}
	}
	return nil
}

func HasConfiguration(binding *Binding, id string) bool {
	if binding != nil && binding.Kind == "configuration" && binding.AllConfigurations && strings.TrimSpace(id) != "" && id == strings.TrimSpace(id) {
		return true
	}
	for _, configured := range ConfigurationIDs(binding) {
		if configured == id {
			return true
		}
	}
	return false
}

// ConfigurationReference constructs a single execution route, without carrying
// the Agent's unrelated alternatives into an execution fingerprint.
func ConfigurationReference(id string) *Binding {
	return &Binding{Kind: "configuration", ConfigurationID: id}
}
