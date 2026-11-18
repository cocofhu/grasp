package modelrouting

import (
	"slices"
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
)

// ServiceTiers requires an exact model capability from the official Codex
// catalog. Compatible API accounts do not inherit platform speed tiers.
// Empty means only the standard tier is available.
func ServiceTiers(backend, modelID string, connection *Connection) []string {
	if strings.TrimSpace(backend) != "codex" || connection != nil {
		return nil
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || modelID == "auto" {
		return nil
	}
	for _, model := range clicatalog.Models("codex") {
		if model.ID == modelID {
			return slices.Clone(model.ServiceTiers)
		}
	}
	return nil
}
