package modelrouting

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
)

//go:embed opencode_variants.json
var openCodeVariantCatalogJSON []byte

type openCodeVariantModel struct {
	Spell    string   `json:"spell"`
	Variants []string `json:"variants"`
}

// Generated from OpenCode's Variant.supports and Variant.resolve over every
// models.dev opencode model. An empty variants list means the model has none.
var openCodeVariantCatalog = mustOpenCodeVariantCatalog()

func mustOpenCodeVariantCatalog() map[string]openCodeVariantModel {
	var out map[string]openCodeVariantModel
	if err := json.Unmarshal(openCodeVariantCatalogJSON, &out); err != nil {
		panic(err)
	}
	return out
}

var effortTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]{0,63}$`)

// OpenCodeCatalogID strips the platform provider prefix from a model id.
func OpenCodeCatalogID(modelID string) string {
	id := strings.TrimSpace(modelID)
	id = strings.TrimPrefix(id, "opencode/")
	if i := strings.LastIndex(id, "#"); i >= 0 {
		id = id[:i]
	}
	return id
}

// OpenCodePlatformVariants returns the catalog variants for an official model.
// Models without variants, and ids outside the catalog, return nil.
func OpenCodePlatformVariants(modelID string) []string {
	entry, ok := openCodeVariantCatalog[OpenCodeCatalogID(modelID)]
	if !ok || len(entry.Variants) == 0 {
		return nil
	}
	return append([]string{}, entry.Variants...)
}

// OpenCodeCatalogVariant reports whether effort is one of the model's catalog variants.
func OpenCodeCatalogVariant(modelID, effort string) bool {
	effort = strings.TrimSpace(effort)
	for _, item := range OpenCodePlatformVariants(modelID) {
		if item == effort {
			return true
		}
	}
	return false
}

// ValidEffortToken accepts one OpenCode variant token. Blank is the default and
// is not a token. Spaces, slashes and hashes are rejected.
func ValidEffortToken(effort string) bool {
	return effortTokenPattern.MatchString(strings.TrimSpace(effort))
}

// WithSavedEffort appends a saved custom variant when the catalog does not
// already list it, so a session can select the value stored on the model group.
func WithSavedEffort(efforts []string, saved string) []string {
	saved = strings.TrimSpace(saved)
	if !ValidEffortToken(saved) {
		return efforts
	}
	for _, item := range efforts {
		if item == saved {
			return efforts
		}
	}
	return append(append([]string{}, efforts...), saved)
}
