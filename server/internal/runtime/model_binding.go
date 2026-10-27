package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
)

const (
	EnvACPBridgeModel    = "ACP_BRIDGE_MODEL"
	EnvModelConnection   = "GRASP_MODEL_CONNECTION"
	EnvModelAPIKey       = "GRASP_MODEL_API_KEY"
	EnvModelCredentialID = "GRASP_MODEL_CREDENTIAL_ID"
	// EnvConfiguredModels is the ordered allow-list for interactive switching.
	// Index 0 is the primary model and may be empty when the tool should choose.
	EnvConfiguredModels = "GRASP_CONFIGURED_MODELS"
	// EnvInitialChatModel pins only the first sandbox chat. Empty follows the primary.
	EnvInitialChatModel = "GRASP_INITIAL_CHAT_MODEL"
)

// ModelBindingResolver resolves only selected, active references. The secret is
// never persisted in an Agent, connection DTO, run snapshot or config document.
type ModelBindingResolver func(string, *modelrouting.Binding) (modelrouting.Resolved, error)

// ApplyModelBinding replaces every legacy coding-provider selection as a unit.
// Git/SSH credentials remain independent. A missing or revoked reference fails
// before a sandbox is created instead of falling back to a workspace secret.
func ApplyModelBinding(env map[string]string, backend string, binding *modelrouting.Binding, resolve ModelBindingResolver) error {
	if err := modelrouting.ValidateConfigurationReference(binding); err != nil {
		return err
	}
	if resolve == nil {
		return fmt.Errorf("model binding resolver unavailable")
	}
	resolved, err := resolve(backend, binding)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resolved.Secret) == "" {
		return fmt.Errorf("selected model credential is unavailable")
	}
	ClearModelRoutingEnv(env)
	env[EnvModelCredentialID] = resolved.CredentialID
	switch resolved.Binding.Kind {
	case "platform":
		key := CredentialEnvKeyForBackend(backend)
		if key == "" {
			return fmt.Errorf("%s has no platform credential mode", backend)
		}
		env[key] = resolved.Secret
		env[EnvACPBridgeModel] = resolved.Binding.ModelID
		if backend == string(BackendOpenCode) {
			env["GRASP_OPENCODE_PROVIDER"] = "opencode"
		}
	case "connection":
		if resolved.Connection == nil {
			return fmt.Errorf("selected model connection is unavailable")
		}
		c := resolved.Connection
		config := struct {
			ConnectionID    string                    `json:"connectionId"`
			ProviderID      string                    `json:"providerId"`
			Protocol        string                    `json:"protocol"`
			BaseURL         string                    `json:"baseUrl"`
			Auth            modelrouting.Auth         `json:"auth"`
			Models          []modelrouting.Model      `json:"models"`
			SelectedModelID string                    `json:"selectedModelId"`
			ToolSettings    modelrouting.ToolSettings `json:"toolSettings,omitempty"`
		}{c.ID, c.ProviderID, c.Protocol, c.BaseURL, c.Auth, c.Models, resolved.Binding.ModelID, resolved.Binding.ToolSettings}
		data, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("encode model connection: %w", err)
		}
		env[EnvModelConnection] = string(data)
		env[EnvModelAPIKey] = resolved.Secret
		env[EnvACPBridgeModel] = resolved.Binding.ModelID
	default:
		return fmt.Errorf("model binding did not resolve to a concrete source")
	}
	// Automation reads only the primary model. An empty primary stays empty
	// even when auxiliary models exist; the allow-list is a separate env value.
	configured, err := configuredModelsJSON(resolved)
	if err != nil {
		return err
	}
	env[EnvConfiguredModels] = configured
	delete(env, EnvACPBridgeReasoningEffort)
	if effort := primaryOpenCodeEffort(backend, resolved); effort != "" {
		env[EnvACPBridgeReasoningEffort] = effort
	}
	return nil
}

func primaryOpenCodeEffort(backend string, resolved modelrouting.Resolved) string {
	if backend != "opencode" {
		return ""
	}
	id := strings.TrimSpace(resolved.Binding.ModelID)
	effort := strings.TrimSpace(resolved.Binding.ToolSettings.ModelEfforts[id])
	if !modelrouting.ValidEffortToken(effort) {
		return ""
	}
	return effort
}

func configuredModelsJSON(resolved modelrouting.Resolved) (string, error) {
	vendor := resolved.Backend
	if resolved.Connection != nil && strings.TrimSpace(resolved.Connection.ProviderID) != "" {
		vendor = resolved.Connection.ProviderID
	}
	type item struct {
		ID                string   `json:"id"`
		Vendor            string   `json:"vendor,omitempty"`
		ReasoningEfforts  []string `json:"reasoningEfforts,omitempty"`
		ReasoningDisabled bool     `json:"reasoningDisabled,omitempty"`
	}
	modelItem := func(id string) item {
		id = strings.TrimSpace(id)
		efforts := modelrouting.ReasoningEfforts(resolved.Backend, id, resolved.Connection)
		if resolved.Backend == "opencode" {
			efforts = modelrouting.WithSavedEffort(efforts, resolved.Binding.ToolSettings.ModelEfforts[id])
		}
		value := item{ID: id, Vendor: vendor, ReasoningEfforts: efforts}
		if resolved.Connection != nil && !modelrouting.FixedEffortBinding(resolved.Backend, resolved.Connection) {
			for _, model := range resolved.Connection.Models {
				if model.ID == id && model.Capabilities.Reasoning != nil && !*model.Capabilities.Reasoning {
					value.ReasoningDisabled = true
				}
			}
		}
		return value
	}
	items := []item{modelItem(resolved.Binding.ModelID)}
	for _, id := range resolved.Binding.AuxiliaryModelIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		items = append(items, modelItem(id))
	}
	data, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("encode configured models: %w", err)
	}
	return string(data), nil
}

// ClearModelRoutingEnv removes only coding authentication and derived model
// routing. It deliberately leaves repository, SSH, MCP and run coordinates.
func ClearModelRoutingEnv(env map[string]string) {
	for _, key := range []string{
		envauth.EnvCursorAPIKey, envauth.EnvClaudeAPIKey, envauth.EnvCodeBuddyAPIKey,
		envauth.EnvTraeAPIKey, envauth.EnvOpenCodeAPIKey, envauth.EnvCodexAuthFile, envauth.EnvDeepSeekAPIKey,
		"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CODEBUDDY_API_KEY",
		"TRAECLI_PERSONAL_ACCESS_TOKEN", "OPENCODE_API_KEY", "OPENAI_API_KEY", "CODEX_API_KEY", "DEEPSEEK_API_KEY",
		"ACP_CURSOR_API_KEY", "ACP_CLAUDE_API_KEY", "ACP_CODEBUDDY_API_KEY", "ACP_CODEX_API_KEY", "ACP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_HARNESS_API_KEY",
		EnvModelConnection, EnvModelAPIKey, EnvModelCredentialID, EnvACPBridgeModel, EnvConfiguredModels, EnvInitialChatModel, "GRASP_OPENCODE_PROVIDER", "GRASP_OPENCODE_BASE_URL", "GRASP_OPENCODE_MODEL_VISION",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL",
		"ANTHROPIC_DEFAULT_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL_FORCE", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_USE_CCR",
		"OPENAI_BASE_URL", "CODEBUDDY_BASE_URL", "CODEBUDDY_MODEL", "CODEBUDDY_SMALL_FAST_MODEL", "CODEBUDDY_BIG_SLOW_MODEL", "CODEBUDDY_CODE_SUBAGENT_MODEL", "CODEBUDDY_SUBAGENT_MODEL",
	} {
		delete(env, key)
	}
}

// PrepareBoundAuth keeps the existing platform login contract and delegates
// explicit model connections to the in-sandbox adapter, which supports their
// protocol-specific authentication without pretending they are platform keys.
func PrepareBoundAuth(backend AcpBackend, env map[string]string, workDir string, sharedWorkDir ...string) (map[string]string, error) {
	if env[EnvModelConnection] != "" {
		if strings.TrimSpace(env[EnvModelAPIKey]) == "" {
			return nil, fmt.Errorf("model connection credential is unavailable")
		}
		return env, nil
	}
	return PrepareAuthEnv(backend, env, workDir, sharedWorkDir...)
}

// InstallBoundCodexLoginFile installs platform login material only when that
// source was selected. A Responses connection authenticates with its own key.
func InstallBoundCodexLoginFile(backend AcpBackend, home, body string, env map[string]string) error {
	if env[EnvModelConnection] != "" {
		return nil
	}
	return InstallCodexLoginFile(backend, home, body)
}

// ModelCredentialIDForWriteBack keeps refreshed login material attached to the
// exact resolved credential, from the selected model configuration.
func ModelCredentialIDForWriteBack(env map[string]string) string {
	return strings.TrimSpace(env[EnvModelCredentialID])
}
