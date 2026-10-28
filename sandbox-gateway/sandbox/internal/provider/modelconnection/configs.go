package modelconnection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"backend/internal/backend/common"
)

func prepareClaude(p Prepared, root string) (Prepared, error) {
	c := p.Connection
	dir, cleanup, err := isolatedConfig(root)
	if err != nil {
		return p, err
	}
	p.Cleanup = cleanup
	p.Env = common.UpsertEnv(p.Env, "CLAUDE_CONFIG_DIR", dir)
	p.Env = common.UpsertEnv(p.Env, "ANTHROPIC_BASE_URL", nativeBaseURL(c))
	if c.Auth.Scheme == "bearer" {
		p.Env = common.UpsertEnv(p.Env, "ANTHROPIC_AUTH_TOKEN", common.GetEnvValue(p.Env, EnvAPIKey))
	} else {
		p.Env = common.UpsertEnv(p.Env, "ANTHROPIC_API_KEY", common.GetEnvValue(p.Env, EnvAPIKey))
	}
	for variable, model := range map[string]string{
		"ANTHROPIC_MODEL":                          p.Model,
		"ANTHROPIC_DEFAULT_MODEL":                  p.Model,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             c.role("opus"),
		"ANTHROPIC_DEFAULT_FABLE_MODEL":            p.Model,
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           c.role("sonnet"),
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            c.role("haiku"),
		"ANTHROPIC_SMALL_FAST_MODEL":               c.role("fast"),
		"CLAUDE_CODE_SUBAGENT_MODEL":               c.role("subagent"),
		"CLAUDE_CODE_SUBAGENT_MODEL_FORCE":         "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	} {
		p.Env = common.UpsertEnv(p.Env, variable, model)
	}
	// Restrict native aliases and project settings to the selected connection.
	ids := modelIDs(c.Models)
	if err := writeJSON(filepath.Join(dir, "settings.json"), map[string]any{"model": p.Model, "availableModels": ids, "fallbackModel": []string{}}); err != nil {
		return p, err
	}
	p.Args = []string{"--setting-sources", "user"}
	return p, nil
}

func prepareCodeBuddy(p Prepared, root string) (Prepared, error) {
	c := p.Connection
	dir, cleanup, err := isolatedConfig(root)
	if err != nil {
		return p, err
	}
	p.Cleanup = cleanup
	p.Env = common.UpsertEnv(p.Env, "CODEBUDDY_CONFIG_DIR", dir)
	p.Env = common.UpsertEnv(p.Env, "CODEBUDDY_API_KEY", common.GetEnvValue(p.Env, EnvAPIKey))
	p.Env = common.UpsertEnv(p.Env, "CODEBUDDY_BASE_URL", c.BaseURL)
	for variable, model := range map[string]string{
		"CODEBUDDY_MODEL":               p.Model,
		"CODEBUDDY_SMALL_FAST_MODEL":    c.role("fast"),
		"CODEBUDDY_BIG_SLOW_MODEL":      c.role("reasoning"),
		"CODEBUDDY_CODE_SUBAGENT_MODEL": c.role("subagent"),
	} {
		p.Env = common.UpsertEnv(p.Env, variable, model)
	}
	ids := modelIDs(c.Models)
	settings := map[string]any{"model": p.Model, "availableModels": ids,
		"variantModels": map[string]string{"lite": c.role("fast"), "reasoning": c.role("reasoning")}}
	if err := writeJSON(filepath.Join(dir, "settings.json"), settings); err != nil {
		return p, err
	}
	models := make([]map[string]any, 0, len(c.Models))
	for _, model := range c.Models {
		m := map[string]any{"id": model.ID, "name": model.Name, "vendor": c.ProviderID,
			"apiKey": "${" + EnvAPIKey + "}", "url": strings.TrimRight(c.BaseURL, "/") + "/chat/completions",
			"relatedModels": map[string]string{"lite": c.role("fast"), "reasoning": c.role("reasoning")}}
		if model.Name == "" {
			m["name"] = model.ID
		}
		if model.ContextWindow > 0 {
			m["maxInputTokens"] = model.ContextWindow
		}
		if model.MaxTokens > 0 {
			m["maxOutputTokens"] = model.MaxTokens
		}
		if model.Capabilities.ToolCalls != nil {
			m["supportsToolCall"] = *model.Capabilities.ToolCalls
		}
		if _, images := modelInput(model); images != nil {
			m["supportsImages"] = *images
		}
		if model.Capabilities.Reasoning != nil {
			m["supportsReasoning"] = *model.Capabilities.Reasoning
		}
		if len(model.ReasoningEfforts) > 0 {
			m["reasoning"] = map[string]any{"supportedEfforts": model.ReasoningEfforts}
		}
		models = append(models, m)
	}
	if err := writeJSON(filepath.Join(dir, "models.json"), map[string]any{"models": models, "availableModels": ids}); err != nil {
		return p, err
	}
	p.Args = []string{"--setting-sources", "user"}
	return p, nil
}

func prepareOpenCode(p Prepared, root string) (Prepared, error) {
	c := p.Connection
	models := make(map[string]any, len(c.Models))
	for _, model := range c.Models {
		m := map[string]any{"id": model.ID, "name": model.ID}
		if model.Name != "" {
			m["name"] = model.Name
		}
		if model.ContextWindow > 0 || model.MaxTokens > 0 {
			limit := map[string]any{}
			if model.ContextWindow > 0 {
				limit["context"] = model.ContextWindow
			}
			if model.MaxTokens > 0 {
				limit["output"] = model.MaxTokens
			}
			m["limit"] = limit
		}
		input, images := modelInput(model)
		if len(input) > 0 {
			m["modalities"] = map[string]any{"input": input, "output": []string{"text"}}
		}
		if images != nil {
			m["attachment"] = *images
		}
		if model.Capabilities.Reasoning != nil {
			m["reasoning"] = *model.Capabilities.Reasoning
		}
		models[model.ID] = m
	}
	npm := "@ai-sdk/openai-compatible"
	if c.Protocol == Responses {
		npm = "@ai-sdk/openai"
	}
	if c.Protocol == Messages {
		npm = "@ai-sdk/anthropic"
	}
	p.Model = p.RouteID + "/" + p.Model
	config := map[string]any{"$schema": "https://opencode.ai/config.json", "model": p.Model,
		"small_model": p.RouteID + "/" + c.role("fast"), "enabled_providers": []string{p.RouteID},
		"provider": map[string]any{p.RouteID: map[string]any{"npm": npm, "name": c.ProviderID,
			"options": map[string]any{"baseURL": openCodeBaseURL(c), "apiKey": "{env:" + EnvAPIKey + "}"}, "models": models}}}
	preserveOpenCodeManagedConfig(config, root)
	data, err := json.Marshal(config)
	if err != nil {
		return p, err
	}
	p.Env = common.UpsertEnv(p.Env, "OPENCODE_CONFIG_CONTENT", string(data))
	return p, nil
}

// MCP endpoints and instruction paths are managed alongside the selected
// configuration. Keep them when replacing native model routing, without
// carrying provider overrides, plugins or saved account credentials.
func preserveOpenCodeManagedConfig(config map[string]any, root string) {
	data, err := os.ReadFile(filepath.Join(root, "opencode.json"))
	if err != nil {
		return
	}
	var existing map[string]json.RawMessage
	if json.Unmarshal(data, &existing) != nil {
		return
	}
	for _, key := range []string{"mcp", "instructions"} {
		if value, ok := existing[key]; ok {
			config[key] = value
		}
	}
}

// Anthropic's native SDK (also used by DSH) appends /v1/messages. Accept a
// versioned root saved by an older OpenCode credential without duplicating v1;
// remove only this terminal segment, preserving any gateway path prefix.
func nativeBaseURL(c *Connection) string {
	if c.Protocol == Messages {
		return strings.TrimSuffix(strings.TrimRight(c.BaseURL, "/"), "/v1")
	}
	return c.BaseURL
}

// The Vercel Anthropic SDK used by OpenCode appends /messages instead.
func openCodeBaseURL(c *Connection) string {
	base := strings.TrimRight(nativeBaseURL(c), "/")
	if c.Protocol == Messages {
		return base + "/v1"
	}
	return base
}

func prepareCodex(p Prepared) Prepared {
	c := p.Connection
	// Explicit command-line provider selection outranks old config.toml; the
	// custom provider never consults the platform auth.json (which is preserved).
	// Resolve the catalog entry again so -c model= cannot keep a request body
	// that merely compared equal to an entry.
	modelID := sessionModelID(*c, p.Model)
	prefix := "model_providers." + p.RouteID + "."
	for _, setting := range []string{
		"model=" + strconv.Quote(modelID),
		"model_provider=" + strconv.Quote(p.RouteID),
		prefix + "name=" + strconv.Quote(c.ProviderID),
		prefix + "base_url=" + strconv.Quote(c.BaseURL),
		prefix + "wire_api=\"responses\"",
		prefix + "env_key=\"" + EnvAPIKey + "\"",
		prefix + "requires_openai_auth=false",
	} {
		p.Args = append(p.Args, "-c", setting)
	}
	return p
}

func modelIDs(models []Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
