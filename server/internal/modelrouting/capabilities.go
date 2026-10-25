package modelrouting

import (
	"fmt"
	"strings"
)

type Evidence struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type Capability struct {
	ToolID            string     `json:"toolId"`
	Modes             []string   `json:"modes"`
	Protocols         []string   `json:"protocols"`
	Transport         string     `json:"transport"`
	VersionConstraint string     `json:"versionConstraint"`
	AdapterState      string     `json:"adapterState"`
	Verification      string     `json:"verification"`
	RouteProfiles     []string   `json:"routeProfiles,omitempty"`
	RoleNames         []string   `json:"roleNames,omitempty"`
	Evidence          []Evidence `json:"evidence"`
}

// Capabilities is a conservative allowlist. Documentation, adapter support and
// verification remain separate facts; a catalog entry is not a live API test.
func Capabilities() []Capability {
	all := []string{ProtocolChat, ProtocolResponses, ProtocolAnthropic}
	capabilities := []Capability{
		{ToolID: "cursor", Modes: []string{"platform"}, Protocols: []string{}, Transport: "acp", VersionConstraint: "deployed CLI", AdapterState: "existing", Verification: "not-run", Evidence: []Evidence{{Kind: "tool-documentation", URL: "https://cursor.com/docs/cli/reference/authentication"}}},
		{ToolID: "trae", Modes: []string{"platform"}, Protocols: []string{}, Transport: "acp", VersionConstraint: "1.x", AdapterState: "existing", Verification: "not-run", Evidence: []Evidence{{Kind: "tool-documentation", URL: "https://docs.trae.cn/cli_agent-client-protocol"}}},
		{ToolID: "claude_code", Modes: []string{"platform", "connection"}, Protocols: []string{ProtocolAnthropic}, Transport: "stream-json", VersionConstraint: "deployed CLI", AdapterState: "existing", Verification: "not-run", Evidence: []Evidence{{Kind: "tool-documentation", URL: "https://code.claude.com/docs/en/llm-gateway-connect"}}},
		{ToolID: "codebuddy", Modes: []string{"platform", "connection"}, Protocols: []string{ProtocolChat}, Transport: "acp", VersionConstraint: "deployed CLI", AdapterState: "existing", Verification: "not-run", Evidence: []Evidence{{Kind: "tool-documentation", URL: "https://www.codebuddy.cn/docs/cli/models"}, {Kind: "tool-documentation", URL: "https://www.codebuddy.cn/docs/cli/env-vars"}, {Kind: "reviewed-client", URL: "https://www.npmjs.com/package/@tencent-ai/codebuddy-code/v/2.162.0"}}},
		{ToolID: "opencode", Modes: []string{"connection"}, Protocols: all, Transport: "exec-json", VersionConstraint: "deployed CLI", AdapterState: "existing", Verification: "not-run", Evidence: []Evidence{{Kind: "tool-documentation", URL: "https://opencode.ai/docs/providers/#custom-provider"}}},
		{ToolID: "codex", Modes: []string{"platform", "connection"}, Protocols: []string{ProtocolResponses}, Transport: "exec-json", VersionConstraint: "deployed CLI", AdapterState: "existing", Verification: "not-run", Evidence: []Evidence{{Kind: "tool-documentation", URL: "https://learn.chatgpt.com/docs/config-file/config-advanced"}}},
		{ToolID: "deepseek_harness", Modes: []string{"connection"}, Protocols: all, Transport: "acp", VersionConstraint: "0.2.1-rc.2", AdapterState: "existing", Verification: "not-run", RouteProfiles: []string{"deepseek-native"}, Evidence: []Evidence{{Kind: "official-source", URL: "https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-pi-ai/README.md"}}},
	}
	for i := range capabilities {
		if capabilities[i].ToolID == "cursor" || capabilities[i].ToolID == "codebuddy" {
			capabilities[i].Transport = "stream-json"
		}
		switch capabilities[i].ToolID {
		case "claude_code":
			capabilities[i].VersionConstraint = ">=2.1.257 (connection mode)"
			capabilities[i].RoleNames = []string{"main", "fast", "subagent", "opus", "sonnet", "haiku"}
		case "codebuddy":
			capabilities[i].RoleNames = []string{"main", "fast", "reasoning", "subagent"}
		case "opencode":
			capabilities[i].RoleNames = []string{"main", "fast"}
		case "codex", "deepseek_harness":
			capabilities[i].RoleNames = []string{"main"}
		}
	}
	return capabilities
}

func CapabilityFor(backend string) (Capability, bool) {
	for _, capability := range Capabilities() {
		if capability.ToolID == strings.TrimSpace(backend) {
			return capability, true
		}
	}
	return Capability{}, false
}

func ValidateBinding(backend string, binding Binding, connection *Connection) error {
	if err := NormalizeAuxiliaryModels(&binding); err != nil {
		return err
	}
	if binding.ConfigurationID != "" {
		return fmt.Errorf("concrete bindings cannot reference a model configuration")
	}
	capability, ok := CapabilityFor(backend)
	if !ok {
		return fmt.Errorf("unknown coding tool %q", backend)
	}
	allowed := false
	for _, mode := range capability.Modes {
		if mode == binding.Kind {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("%s does not support %s model bindings", backend, binding.Kind)
	}
	if binding.Kind == "platform" {
		if binding.CredentialID == "" || binding.ConnectionID != "" {
			return fmt.Errorf("platform binding requires only a credential reference")
		}
		if binding.ToolSettings.RouteProfile != "" {
			return fmt.Errorf("platform binding cannot specify a route profile")
		}
		if len(binding.ToolSettings.ModelRoles) != 0 {
			return fmt.Errorf("platform role mappings are not supported")
		}
		return nil
	}
	if binding.ConnectionID == "" || binding.CredentialID != "" || binding.ModelID == "" || connection == nil {
		return fmt.Errorf("connection binding requires connectionId and modelId")
	}
	allowed = false
	for _, protocol := range capability.Protocols {
		if protocol == connection.Protocol {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("%s does not support protocol %s", backend, connection.Protocol)
	}
	scheme := connection.Auth.Scheme
	if connection.Protocol == ProtocolAnthropic {
		if scheme != "x-api-key" && !(backend == "claude_code" && scheme == "bearer") {
			return fmt.Errorf("unsupported authentication for Anthropic Messages")
		}
	} else if scheme != "bearer" {
		return fmt.Errorf("unsupported authentication for OpenAI protocol")
	}
	if connection.Auth.HeaderName != "" {
		return fmt.Errorf("custom authentication headers are not supported")
	}
	profile := binding.ToolSettings.RouteProfile
	if profile != "" {
		if backend != "deepseek_harness" || profile != "deepseek-native" || connection.ProviderID != "deepseek" || connection.Protocol != ProtocolAnthropic || strings.TrimRight(connection.BaseURL, "/") != "https://api.deepseek.com/anthropic" || scheme != "x-api-key" {
			return fmt.Errorf("unsupported native model route")
		}
	}
	for role := range binding.ToolSettings.ModelRoles {
		roleAllowed := false
		for _, allowedRole := range capability.RoleNames {
			if role == allowedRole {
				roleAllowed = true
			}
		}
		if !roleAllowed {
			return fmt.Errorf("unsupported model role for %s", backend)
		}
	}
	if main := binding.ToolSettings.ModelRoles["main"]; main != "" && main != binding.ModelID {
		return fmt.Errorf("main role must match modelId")
	}
	return nil
}
