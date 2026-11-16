package models

// validTrigger reports whether s is one of the allowed codes.
// Test-only helper (production parsing uses ParseTrigger / NormalizeTrigger).
func validTrigger(s string) bool {
	switch s {
	case TriggerManual, TriggerAPI:
		return true
	default:
		return false
	}
}
