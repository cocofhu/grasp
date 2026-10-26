package modelrouting

import (
	"fmt"
	"strings"
)

// NormalizeAuxiliaryModels trims the primary model and the auxiliary list.
// An empty auxiliary entry or any repeated id is rejected. An empty list is
// stored as nil so older payloads stay unchanged.
func NormalizeAuxiliaryModels(binding *Binding) error {
	if binding == nil {
		return fmt.Errorf("binding is required")
	}
	binding.ModelID = strings.TrimSpace(binding.ModelID)
	seen := map[string]struct{}{}
	if binding.ModelID != "" {
		seen[binding.ModelID] = struct{}{}
	}
	out := make([]string, 0, len(binding.AuxiliaryModelIDs))
	for _, raw := range binding.AuxiliaryModelIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return fmt.Errorf("auxiliary model cannot be empty")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate model %s", id)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		binding.AuxiliaryModelIDs = nil
	} else {
		binding.AuxiliaryModelIDs = out
	}
	return nil
}

// AcceptsConfiguredModel reports whether modelID may be used for one chat or
// Solo start. Empty means follow the primary model.
func AcceptsConfiguredModel(binding Binding, modelID string) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || modelID == strings.TrimSpace(binding.ModelID) {
		return true
	}
	for _, id := range binding.AuxiliaryModelIDs {
		if strings.TrimSpace(id) == modelID {
			return true
		}
	}
	return false
}

// ModelInCatalog reports whether id is present on a connection's model list.
func ModelInCatalog(connection Connection, modelID string) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false
	}
	for _, model := range connection.Models {
		if model.ID == modelID {
			return true
		}
	}
	return false
}
