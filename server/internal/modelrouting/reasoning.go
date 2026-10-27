package modelrouting

import (
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
)

// openCodeEfforts is the verified TokenHub table. It is not the official
// OpenCode catalog: a model can have different levels on TokenHub than it does
// under an OpenCode API key. Keys match the model id, or its last path segment,
// ignoring case.
var openCodeEfforts = map[string][]string{
	"deepseek-v4.1-flash":      {"low", "high", "max"},
	"deepseek-v4.1-flash-free": {"low", "high", "max"},
	"deepseek-v4-flash":        {"low", "high", "max"},
	"deepseek-v4":              {"low", "high", "max"},
	"deepseek-v4-pro":          {"low", "high", "max"},
}

func OpenCodeModelEfforts(modelID string) []string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if efforts, ok := openCodeEfforts[id]; ok {
		return append([]string{}, efforts...)
	}
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		if efforts, ok := openCodeEfforts[id[slash+1:]]; ok {
			return append([]string{}, efforts...)
		}
	}
	return nil
}

// TokenHub reports the TokenHub service, including the built-in hunyuan entry.
func TokenHub(connection *Connection) bool {
	if connection == nil {
		return false
	}
	id := strings.ToLower(strings.TrimSpace(connection.ProviderID))
	if id == "tokenhub" || id == "hunyuan" {
		return true
	}
	return strings.Contains(strings.ToLower(connection.BaseURL), "tokenhub.tencentmaas.com")
}

// FixedEffortBinding is OpenCode on TokenHub. Those models use the verified
// table above. Official OpenCode models use the generated catalog instead.
func FixedEffortBinding(backend string, connection *Connection) bool {
	return connection != nil && backend == "opencode" && TokenHub(connection)
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
		if TokenHub(connection) {
			return OpenCodeModelEfforts(modelID)
		}
		if connection == nil {
			return OpenCodePlatformVariants(modelID)
		}
		return nil
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
