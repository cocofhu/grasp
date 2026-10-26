package modelrouting

import (
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
)

// ReasoningEfforts returns only declared choices for adapters that apply a
// separate effort. Model identifiers remain opaque; their spelling grants no
// capabilities. Connection metadata takes precedence over the CLI shortlist.
func ReasoningEfforts(backend, modelID string, connection *Connection) []string {
	if backend != "codex" && backend != "deepseek_harness" {
		return nil
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || modelID == "auto" {
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
