package service

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"

	"backend/internal/agents"
	"backend/internal/provider"
	"backend/internal/provider/kimicode"
)

const envConfiguredModels = "GRASP_CONFIGURED_MODELS"

type configuredModelSpec struct {
	ID                string   `json:"id"`
	Vendor            string   `json:"vendor,omitempty"`
	ReasoningEfforts  []string `json:"reasoningEfforts,omitempty"`
	ReasoningDisabled bool     `json:"reasoningDisabled,omitempty"`
}

var (
	chatModelsMu         sync.Mutex
	chatModelsCache      []AgentModel
	chatModelsConfigured bool
	chatModelsRaw        string
	chatModelsLoaded     bool
)

// ChatModelList returns the models a chat may show. When the sandbox was given
// a configured allow-list, the result is that list in order and nothing else.
func ChatModelList() (models []AgentModel, configured bool, err error) {
	chatModelsMu.Lock()
	defer chatModelsMu.Unlock()
	raw := strings.TrimSpace(os.Getenv(envConfiguredModels))
	if chatModelsLoaded && chatModelsRaw == raw {
		return append([]AgentModel(nil), chatModelsCache...), chatModelsConfigured, nil
	}
	spec, ok, parseErr := parseConfiguredModels()
	if parseErr != nil {
		return nil, true, parseErr
	}
	if !ok {
		models, err = ListAgentModels()
		if err != nil {
			return nil, false, err
		}
		chatModelsCache = append([]AgentModel(nil), models...)
		chatModelsConfigured = false
		chatModelsRaw = raw
		chatModelsLoaded = true
		return models, false, nil
	}
	catalog, catalogErr := ListAgentModels()
	if catalogErr != nil {
		log.Printf("acp-bridge: 配置模型目录读取失败，仍按标识展示: %v", catalogErr)
	}
	models = projectConfiguredModels(spec, catalog, catalogErr)
	chatModelsCache = append([]AgentModel(nil), models...)
	chatModelsConfigured = true
	chatModelsRaw = raw
	chatModelsLoaded = true
	return models, true, nil
}

// ModelSelectable reports whether a chat may switch to id. Empty follows the primary model.
// With no allow-list, any id is accepted: chat creation must not depend on the provider
// catalog being readable. An allow-list that fails to parse rejects every explicit id.
func ModelSelectable(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return true
	}
	_, configured, err := parseConfiguredModels()
	if err != nil || !configured {
		return err == nil
	}
	models, _, err := ChatModelList()
	if err != nil {
		return false
	}
	for _, model := range models {
		if model.ID == id && !model.Unavailable {
			return true
		}
	}
	return false
}

// ReasoningEffortSelectable accepts only capabilities supplied for the exact
// configured model and handled by this runtime's concrete provider adapter.
func ReasoningEffortSelectable(model, effort string) bool {
	if effort == "" {
		return true
	}
	for _, value := range configuredReasoningOptions(model) {
		if effort == value {
			return true
		}
	}
	return false
}

func configuredReasoningOptions(model string) []string {
	backend := agents.Current().Name()
	if backend != provider.Codex && backend != provider.DeepSeekHarness && backend != provider.OpenCode && backend != provider.KimiCode {
		return nil
	}
	spec, configured, err := parseConfiguredModels()
	if err != nil || !configured {
		return nil
	}
	for _, item := range spec {
		if strings.TrimSpace(item.ID) != strings.TrimSpace(model) {
			continue
		}
		if item.ReasoningDisabled {
			return nil
		}
		if backend == provider.KimiCode {
			// Kimi Code accepts only the exact native strengths for these
			// models, even if stale server metadata declares more values.
			var efforts []string
			for _, supported := range kimicode.ReasoningEfforts(model) {
				for _, declared := range item.ReasoningEfforts {
					if declared == supported {
						efforts = append(efforts, supported)
						break
					}
				}
			}
			return efforts
		}
		return append([]string(nil), item.ReasoningEfforts...)
	}
	return nil
}

func modelReasoningDisabled(model string) bool {
	spec, _, err := parseConfiguredModels()
	if err != nil {
		return true
	}
	for _, item := range spec {
		if strings.TrimSpace(item.ID) == strings.TrimSpace(model) {
			return item.ReasoningDisabled
		}
	}
	return false
}

func parseConfiguredModels() ([]configuredModelSpec, bool, error) {
	raw := strings.TrimSpace(os.Getenv(envConfiguredModels))
	if raw == "" {
		return nil, false, nil
	}
	var items []configuredModelSpec
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, true, err
	}
	if len(items) == 0 {
		return nil, false, nil
	}
	return items, true, nil
}

func projectConfiguredModels(spec []configuredModelSpec, catalog []AgentModel, catalogErr error) []AgentModel {
	byID := map[string]AgentModel{}
	for _, model := range catalog {
		if model.ID != "" {
			byID[model.ID] = model
		}
	}
	out := make([]AgentModel, 0, len(spec))
	for _, item := range spec {
		id := strings.TrimSpace(item.ID)
		vendor := strings.TrimSpace(item.Vendor)
		if item.ReasoningDisabled {
			item.ReasoningEfforts = nil
		}
		if id == "" {
			out = append(out, AgentModel{Name: "自动", Vendor: vendor, ReasoningEfforts: append([]string(nil), item.ReasoningEfforts...)})
			continue
		}
		if known, ok := byID[id]; ok {
			name := strings.TrimSpace(known.Name)
			if name == "" {
				name = id
			}
			if strings.TrimSpace(known.Vendor) != "" {
				vendor = known.Vendor
			}
			out = append(out, AgentModel{ID: id, Name: name, Vendor: vendor, ReasoningEfforts: append([]string(nil), item.ReasoningEfforts...)})
			continue
		}
		out = append(out, AgentModel{ID: id, Name: id, Vendor: vendor, Unavailable: catalogErr == nil && len(catalog) > 0, ReasoningEfforts: append([]string(nil), item.ReasoningEfforts...)})
	}
	return out
}
