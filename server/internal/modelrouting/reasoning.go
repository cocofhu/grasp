package modelrouting

import (
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
)

// TokenHub reports the TokenHub service, including the built-in hunyuan entry
// and the international TokenHub host.
func TokenHub(connection *Connection) bool {
	entry := providerForConnection(connection)
	if entry == nil {
		return false
	}
	for _, id := range entry.IDs {
		if id == "tokenhub" || id == "hunyuan" {
			return true
		}
	}
	return false
}

// FixedEffortBinding is an OpenCode connection whose levels come from the
// documented provider catalog. Official OpenCode keys use the generated catalog.
func FixedEffortBinding(backend string, connection *Connection) bool {
	return connection != nil && backend == "opencode" && providerForConnection(connection) != nil
}

// ReasoningEfforts returns only declared choices for adapters that apply a
// separate effort. Model identifiers remain opaque; their spelling grants no
// capabilities. Connection metadata takes precedence over the CLI shortlist.
func ReasoningEfforts(backend, modelID string, connection *Connection) []string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || modelID == "auto" {
		return nil
	}
	if backend == "opencode" {
		if connection == nil {
			return OpenCodePlatformVariants(modelID)
		}
		return providerModelEfforts(connection, modelID)
	}
	if backend != "codex" && backend != "deepseek_harness" {
		return nil
	}
	if connection != nil {
		found := false
		for _, model := range connection.Models {
			if model.ID != modelID {
				continue
			}
			found = true
			if model.Capabilities.Reasoning != nil && !*model.Capabilities.Reasoning {
				return nil
			}
			if len(model.ReasoningEfforts) > 0 {
				return cleanEfforts(model.ReasoningEfforts)
			}
			break
		}
		if !found {
			return nil
		}
	}
	if backend == "codex" {
		for _, model := range clicatalog.Models(backend) {
			if model.ID == modelID {
				return cleanEfforts(model.Efforts)
			}
		}
	}
	return nil
}

func cleanEfforts(input []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
