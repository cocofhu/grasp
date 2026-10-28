// Package modelconnection translates a selected model connection into a CLI's
// native configuration. Connection JSON never contains a credential: every
// generated file refers to the separately injected API-key environment variable.
package modelconnection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/backend/common"
	"backend/internal/provider"
)

const (
	EnvConfig = "GRASP_MODEL_CONNECTION"
	EnvAPIKey = "GRASP_MODEL_API_KEY"
	Chat      = "openai-chat-completions"
	Responses = "openai-responses"
	Messages  = "anthropic-messages"
)

type Auth struct {
	Scheme     string `json:"scheme"`
	HeaderName string `json:"headerName,omitempty"`
}

type Capabilities struct {
	ToolCalls *bool `json:"toolCalls,omitempty"`
	Vision    *bool `json:"vision,omitempty"`
	Reasoning *bool `json:"reasoning,omitempty"`
}

type Model struct {
	ID               string       `json:"id"`
	Name             string       `json:"name,omitempty"`
	Source           string       `json:"source,omitempty"`
	Input            []string     `json:"input,omitempty"`
	ContextWindow    int          `json:"contextWindow,omitempty"`
	MaxTokens        int          `json:"maxTokens,omitempty"`
	ReasoningEfforts []string     `json:"reasoningEfforts,omitempty"`
	Capabilities     Capabilities `json:"capabilities,omitempty"`
}

type ToolSettings struct {
	ModelRoles   map[string]string `json:"modelRoles,omitempty"`
	RouteProfile string            `json:"routeProfile,omitempty"`
	ModelEfforts map[string]string `json:"modelEfforts,omitempty"`
}

type Connection struct {
	ConnectionID    string       `json:"connectionId"`
	ProviderID      string       `json:"providerId"`
	Protocol        string       `json:"protocol"`
	BaseURL         string       `json:"baseUrl"`
	Auth            Auth         `json:"auth"`
	Models          []Model      `json:"models"`
	SelectedModelID string       `json:"selectedModelId"`
	ToolSettings    ToolSettings `json:"toolSettings,omitempty"`
}

// Prepared owns any temporary configuration for one provider session.
type Prepared struct {
	Env        []string
	Model      string
	Args       []string
	Connection *Connection
	RouteID    string
	DSHPatch   []byte
	Platform   bool
	Cleanup    func()
}

// Catalog exposes upstream IDs to the UI, rather than a CLI-specific route.
func Catalog(name provider.Name, env []string) ([]provider.Model, bool, error) {
	raw := strings.TrimSpace(common.GetEnvValue(env, EnvConfig))
	if raw == "" {
		return nil, false, nil
	}
	var c Connection
	if json.Unmarshal([]byte(raw), &c) != nil {
		return nil, true, errors.New("model connection: invalid configuration JSON")
	}
	if err := c.validate(name); err != nil {
		return nil, true, err
	}
	models := make([]provider.Model, 0, len(c.Models))
	for _, model := range c.Models {
		label := model.Name
		if label == "" {
			label = model.ID
		}
		models = append(models, provider.Model{ID: model.ID, Name: label, IsDefault: model.ID == c.SelectedModelID})
	}
	return models, true, nil
}

// Prepare preserves platform mode when no connection is selected. It must run
// after legacy AuthEnv normalization so old aliases cannot restore an old key.
func Prepare(name provider.Name, env []string, model, configRoot string) (Prepared, error) {
	p := Prepared{Env: env, Cleanup: func() {}}
	raw := strings.TrimSpace(common.GetEnvValue(env, EnvConfig))
	if raw == "" {
		// No catalog: the request model stays on the platform path and is not
		// written into a connection command line.
		p.Model = model
		if name == provider.OpenCode && common.GetEnvValue(env, "GRASP_OPENCODE_PROVIDER") == "opencode" {
			return prepareOpenCodePlatform(p, configRoot)
		}
		localAuth := strings.TrimSpace(common.GetEnvValue(env, "GRASP_LOCAL_AUTH")) == "1"
		if localAuth {
			if root := strings.TrimSpace(common.GetEnvValue(env, "CONFIG_ROOT")); root != "" {
				configRoot = root
			}
			if strings.TrimSpace(configRoot) != "" {
				switch name {
				case provider.OpenCode:
					p.Env = common.UpsertEnv(p.Env, "OPENCODE_CONFIG_DIR", configRoot)
					p = qualifyLocalOpenCodeModel(p, configRoot)
				case provider.ClaudeCode, provider.ClaudeCodeACP:
					p.Env = common.UpsertEnv(p.Env, "CLAUDE_CONFIG_DIR", configRoot)
				case provider.CodeBuddy, provider.CodeBuddyACP:
					p.Env = common.UpsertEnv(p.Env, "CODEBUDDY_CONFIG_DIR", configRoot)
				}
			}
		}
		if name == provider.DeepSeekHarness {
			key := common.GetEnvValue(env, "DEEPSEEK_API_KEY")
			baseURL := common.GetEnvValue(env, "DEEPSEEK_BASE_URL")
			if !localAuth && (strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00")) {
				return p, errors.New("DeepSeek Harness official authorization: selected API key is missing or invalid")
			}
			p.Env = cleanEnv(env)
			if strings.TrimSpace(key) != "" {
				p.Env = common.UpsertEnv(p.Env, "DEEPSEEK_API_KEY", key)
			}
			if localAuth && strings.TrimSpace(baseURL) != "" {
				p.Env = common.UpsertEnv(p.Env, "DEEPSEEK_BASE_URL", baseURL)
			}
		}
		return p, nil
	}
	var c Connection
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return p, errors.New("model connection: invalid configuration JSON")
	}
	if err := c.validate(name); err != nil {
		return p, err
	}
	key := common.GetEnvValue(env, EnvAPIKey)
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00") {
		return p, errors.New("model connection: selected API key is missing or invalid")
	}
	p.Connection = &c
	p.RouteID = c.RouteID()
	// A chat may select any model on this connection. Anything else, including a
	// stale platform model id, stays on the configuration primary.
	p.Model = sessionModelID(c, model)
	p.Env = cleanEnv(env)
	p.Env = common.UpsertEnv(p.Env, EnvAPIKey, key)
	if root := strings.TrimSpace(common.GetEnvValue(env, "CONFIG_ROOT")); root != "" {
		configRoot = root
	}
	var err error
	switch name {
	case provider.ClaudeCode, provider.ClaudeCodeACP:
		p, err = prepareClaude(p, configRoot)
	case provider.CodeBuddy, provider.CodeBuddyACP:
		p, err = prepareCodeBuddy(p, configRoot)
	case provider.OpenCode:
		p, err = prepareOpenCode(p, configRoot)
	case provider.Codex:
		p = prepareCodex(p)
	case provider.DeepSeekHarness:
		p.DSHPatch, err = c.dshPatch(p.RouteID, p.Model)
		if c.ToolSettings.RouteProfile == "deepseek-native" {
			p.RouteID = "deepseek-official"
		}
	}
	if err != nil {
		p.Cleanup()
		return Prepared{}, err
	}
	return p, nil
}

// sessionModelID is the catalog id for this launch. A request is accepted only
// when it equals one catalog entry; the result is that entry's own ID, never
// the request string. Empty, auto, and unknown ids stay on the primary so a
// stale platform model cannot leave the connection.
func sessionModelID(c Connection, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" && requested != "auto" {
		for i := range c.Models {
			if c.Models[i].ID == requested {
				return c.Models[i].ID
			}
		}
	}
	return c.SelectedModelID
}

// ChatModelID is the catalog id prepared for this connection. OpenCode stores
// route/id in Model; every other tool stores the raw id.
func (p Prepared) ChatModelID() string {
	if prefix := p.RouteID + "/"; p.RouteID != "" && strings.HasPrefix(p.Model, prefix) {
		return strings.TrimPrefix(p.Model, prefix)
	}
	if p.Connection == nil {
		return strings.TrimSpace(p.Model)
	}
	return p.Model
}

func qualifyLocalOpenCodeModel(p Prepared, root string) Prepared {
	data, err := os.ReadFile(filepath.Join(root, "opencode.json"))
	if err != nil {
		return p
	}
	p.Env = common.UpsertEnv(p.Env, "OPENCODE_CONFIG_CONTENT", string(data))
	m := strings.TrimSpace(p.Model)
	if m == "" || m == "auto" || strings.Contains(m, "/") {
		return p
	}
	var doc struct {
		Model            string         `json:"model"`
		EnabledProviders []string       `json:"enabled_providers"`
		Provider         map[string]any `json:"provider"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return p
	}
	prov, _, ok := strings.Cut(strings.TrimSpace(doc.Model), "/")
	if !ok || prov == "" {
		if len(doc.EnabledProviders) == 1 {
			prov = strings.TrimSpace(doc.EnabledProviders[0])
		} else if len(doc.Provider) == 1 {
			for k := range doc.Provider {
				prov = strings.TrimSpace(k)
			}
		}
	}
	if prov != "" {
		p.RouteID = prov
		p.Model = prov + "/" + m
	}
	return p
}

func (c Connection) RouteID() string {
	sum := sha256.Sum256([]byte(c.ConnectionID))
	return "grasp-" + hex.EncodeToString(sum[:12])
}

func (c Connection) role(role string) string {
	if value := c.ToolSettings.ModelRoles[role]; value != "" {
		return value
	}
	return c.SelectedModelID
}

func (c Connection) validate(name provider.Name) error {
	fail := func(message string) error { return fmt.Errorf("model connection: %s", message) }
	if c.ConnectionID == "" || c.ProviderID == "" || c.SelectedModelID == "" {
		return fail("connection, provider and selected model are required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fail("base URL must be an HTTP(S) API root without credentials, query or fragment")
	}
	if c.Protocol != Chat && c.Protocol != Responses && c.Protocol != Messages {
		return fail("unsupported protocol")
	}
	known := map[string]bool{}
	for _, m := range c.Models {
		if strings.TrimSpace(m.ID) == "" || known[m.ID] {
			return fail("model IDs must be nonempty and unique")
		}
		known[m.ID] = true
		if m.ContextWindow < 0 || m.MaxTokens < 0 {
			return fail("invalid model token capacity")
		}
		for _, input := range m.Input {
			if input != "text" && input != "image" {
				return fail("unsupported model input type")
			}
		}
	}
	if !known[c.SelectedModelID] {
		return fail("selected model is absent from this connection")
	}
	for role, value := range c.ToolSettings.ModelRoles {
		if !known[value] {
			return fail("role model is absent from this connection")
		}
		if role == "main" && value != c.SelectedModelID {
			return fail("main role disagrees with selected model")
		}
		switch name {
		case provider.ClaudeCode, provider.ClaudeCodeACP:
			if role != "main" && role != "fast" && role != "subagent" && role != "opus" && role != "sonnet" && role != "haiku" {
				return fail("unsupported Claude model role")
			}
		case provider.CodeBuddy, provider.CodeBuddyACP:
			if role != "main" && role != "fast" && role != "subagent" && role != "reasoning" {
				return fail("unsupported CodeBuddy model role")
			}
		case provider.OpenCode:
			if role != "main" && role != "fast" {
				return fail("unsupported OpenCode model role")
			}
		default:
			if role != "main" {
				return fail("tool does not support this model role")
			}
		}
	}
	profile := c.ToolSettings.RouteProfile
	if profile != "" && (name != provider.DeepSeekHarness || profile != "deepseek-native") {
		return fail("unsupported tool route profile")
	}
	if profile == "deepseek-native" && (c.ProviderID != "deepseek" || c.Protocol != Messages || u.Scheme != "https" || u.Host != "api.deepseek.com" || strings.TrimRight(u.Path, "/") != "/anthropic") {
		return fail("DeepSeek native profile requires the official DeepSeek Messages service")
	}
	switch name {
	case provider.ClaudeCode, provider.ClaudeCodeACP:
		if c.Protocol != Messages {
			return fail("Claude Code requires Anthropic Messages")
		}
	case provider.Codex:
		if c.Protocol != Responses {
			return fail("Codex requires OpenAI Responses")
		}
	case provider.CodeBuddy, provider.CodeBuddyACP:
		// The reviewed 2.162.0 CLI routes both its environment-based and
		// custom models.json requests through Chat Completions. An `api`
		// metadata field does not switch the HTTP transport.
		if c.Protocol != Chat {
			return fail("CodeBuddy model connections require OpenAI Chat Completions")
		}
	case provider.OpenCode, provider.DeepSeekHarness:
	default:
		return fail("this tool does not support model connections")
	}
	if c.Protocol == Messages {
		if c.Auth.Scheme != "x-api-key" && !(c.Auth.Scheme == "bearer" && (name == provider.ClaudeCode || name == provider.ClaudeCodeACP)) {
			return fail("this Messages adapter requires x-api-key authentication")
		}
	} else if c.Auth.Scheme != "bearer" {
		return fail("this adapter requires bearer authentication")
	}
	return nil
}

// Do not let a CLI's legacy account/provider env redirect a selected connection.
func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if key == EnvConfig || key == EnvAPIKey {
			out = append(out, item)
			continue
		}
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CODEBUDDY_") || strings.HasPrefix(key, "GRASP_OPENCODE_") || strings.HasPrefix(key, "ACP_DEEPSEEK") {
			continue
		}
		switch key {
		case "OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_API_BASE", "OPENCODE_API_KEY", "OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT", "OPENCODE_CONFIG_DIR", "OPENCODE_MODELS_PATH", "OPENCODE_MODELS_URL", "OPENCODE_CONSOLE_TOKEN", "CODEX_API_KEY", "ACP_CODEX_API_KEY", "DEEPSEEK_API_KEY", "DEEPSEEK_BASE_URL", "DEEPSEEK_SEARCH_BASE_URL", "GRASP_DEEPSEEK_API_KEY", "GRASP_CLAUDE_API_KEY", "GRASP_CODEBUDDY_API_KEY", "ACP_CLAUDE_API_KEY", "ACP_CODEBUDDY_API_KEY", "ACP_OPENCODE_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_USE_CCR", "CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE":
			continue
		case "GRASP_CURSOR_API_KEY", "GRASP_TRAE_API_KEY", "GRASP_CODEX_AUTH_JSON", "CURSOR_API_KEY", "ACP_CURSOR_API_KEY", "TRAECLI_PERSONAL_ACCESS_TOKEN", "ACP_TRAE_API_KEY", "OPENROUTER_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENERATIVE_AI_API_KEY", "GEMINI_API_KEY", "MOONSHOT_API_KEY", "DASHSCOPE_API_KEY", "XAI_API_KEY", "KIMI_CODE_API_KEY", "GRASP_KIMI_CODE_API_KEY", "ACP_KIMI_CODE_API_KEY":
			continue
		}
		out = append(out, item)
	}
	return out
}

// isolatedConfig keeps skills, rules and session history while omitting login,
// provider and settings files that could override the selected service.
func isolatedConfig(root string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "grasp-model-")
	if err != nil {
		return "", func() {}, errors.New("model connection: cannot create isolated config")
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for _, entry := range []string{"skills", "rules", "agents", "commands", "projects", "sessions", "CLAUDE.md", "CODEBUDDY.md", "AGENTS.md", "mcp.json", ".mcp.json"} {
		path := filepath.Join(root, entry)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Symlink(path, filepath.Join(dir, entry)); err != nil {
			cleanup()
			return "", func() {}, errors.New("model connection: cannot preserve agent config assets")
		}
	}
	return dir, cleanup, nil
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("model connection: cannot encode CLI config")
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return errors.New("model connection: cannot write CLI config")
	}
	return nil
}
