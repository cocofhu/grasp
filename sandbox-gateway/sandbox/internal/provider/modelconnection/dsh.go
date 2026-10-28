package modelconnection

import "gopkg.in/yaml.v3"

func (c Connection) dshPatch(route, sessionModel string) ([]byte, error) {
	models := make([]map[string]any, 0, len(c.Models))
	for _, model := range c.Models {
		m := map[string]any{"id": model.ID}
		if model.Name != "" {
			m["name"] = model.Name
		}
		if model.ContextWindow > 0 {
			m["contextWindow"] = model.ContextWindow
		}
		if model.MaxTokens > 0 {
			m["maxTokens"] = model.MaxTokens
		}
		input, _ := modelInput(model)
		if len(input) > 0 {
			if c.ToolSettings.RouteProfile == "deepseek-native" {
				m["inputModalities"] = input
			} else {
				m["input"] = input
			}
		}
		if len(model.ReasoningEfforts) > 0 && c.ToolSettings.RouteProfile != "deepseek-native" {
			efforts := map[string]any{}
			for _, effort := range model.ReasoningEfforts {
				if effort == "off" {
					efforts[effort] = nil
				} else {
					efforts[effort] = effort
				}
			}
			m["reasoningEfforts"] = efforts
		}
		models = append(models, m)
	}
	rows := []map[string]any{{"id": "credentials", "disabled": true}, {"id": "authorization", "disabled": true},
		{"id": "deepseek-account", "disabled": true}, {"id": "llm-deepseek-account", "disabled": true}}
	if c.ToolSettings.RouteProfile == "deepseek-native" {
		route = "deepseek-official"
		rows = append(rows, map[string]any{"id": "llm-pi-ai", "disabled": true},
			map[string]any{"id": "llm-deepseek", "config": map[string]any{"apiKeyEnv": EnvAPIKey, "baseURL": nativeBaseURL(&c), "models": models}})
	} else {
		api := c.Protocol
		if api == Chat {
			api = "openai-completions"
		}
		rows = append(rows, map[string]any{"id": "llm-deepseek", "disabled": true},
			map[string]any{"id": "llm-pi-ai", "config": map[string]any{"providers": map[string]any{
				route: map[string]any{"apiKeyEnv": EnvAPIKey, "api": api, "baseURL": nativeBaseURL(&c), "models": models},
			}}})
	}
	rows = append(rows, map[string]any{"id": "acp", "config": map[string]any{"provider": route, "model": sessionModel}})
	return yaml.Marshal(rows)
}
