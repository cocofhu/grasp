package models

import (
	"fmt"
	"strings"
)

// Run trigger codes (stable English storage values).
const (
	TriggerManual = "manual"
	TriggerAPI    = "api"
)

// AllowedTriggers is the authoritative whitelist for new Run.Trigger values.
var AllowedTriggers = []string{TriggerManual, TriggerAPI}

// ParseTrigger validates an exact lowercase trigger code.
// Empty / whitespace-only input returns ("", nil) meaning "not provided"
// so callers can apply a source-specific default. Case variants, Chinese
// display strings, and free-form values are rejected (no silent normalize).
func ParseTrigger(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", nil
	}
	switch trimmed {
	case TriggerManual, TriggerAPI:
		return trimmed, nil
	default:
		return "", fmt.Errorf("invalid trigger %q (want manual|api)", s)
	}
}

// ResolveTrigger applies ParseTrigger then fills sourceDefault when unset.
// sourceDefault must itself be a whitelist code.
func ResolveTrigger(raw, sourceDefault string) (string, error) {
	parsed, err := ParseTrigger(raw)
	if err != nil {
		return "", err
	}
	if parsed == "" {
		return sourceDefault, nil
	}
	return parsed, nil
}
