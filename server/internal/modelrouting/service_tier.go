package modelrouting

import (
	"slices"
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
)

// ServiceTiers requires an exact model capability from the official Codex or
// Cursor catalog. Compatible API accounts do not inherit platform speed tiers.
// Empty means only the standard tier is available.
func ServiceTiers(backend, modelID string, connection *Connection) []string {
	backend = strings.TrimSpace(backend)
	if backend != "codex" && backend != "cursor" || connection != nil {
		return nil
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || modelID == "auto" {
		return nil
	}
	for _, model := range clicatalog.Models(backend) {
		if model.ID == modelID {
			return slices.Clone(model.ServiceTiers)
		}
	}
	return nil
}
