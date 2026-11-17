package modelrouting

import (
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
)

// FastModeSupported requires an exact model capability from the official Codex
// catalog. Compatible API accounts do not inherit platform speed tiers.
func FastModeSupported(backend, modelID string, connection *Connection) bool {
	if strings.TrimSpace(backend) != "codex" || connection != nil {
		return false
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || modelID == "auto" {
		return false
	}
	for _, model := range clicatalog.Models("codex") {
		if model.ID == modelID {
			return model.FastModeSupported
		}
	}
	return false
}
