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
	if binding.CredentialID != "" || binding.ConnectionID != "" || binding.ModelID != "" || len(binding.ToolSettings.ModelRoles) != 0 || binding.ToolSettings.RouteProfile != "" {
		return fmt.Errorf("Agent model binding requires only configurationId")
	}
	return nil
}
