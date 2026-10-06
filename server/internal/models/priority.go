package models

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Run priority levels (stored as ordered integers for claim sorting).
const (
	PriorityLow    = 1
	PriorityNormal = 2
	PriorityHigh   = 3
)

// Priority string labels exposed by API / frontend.
const (
	PriorityLabelLow    = "low"
	PriorityLabelNormal = "normal"
	PriorityLabelHigh   = "high"
)

// ParsePriorityLabel maps a string label to the stored integer.
// Empty string defaults to normal. Unknown values return an error.
func ParsePriorityLabel(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", PriorityLabelNormal:
		return PriorityNormal, nil
	case PriorityLabelHigh:
		return PriorityHigh, nil
	case PriorityLabelLow:
		return PriorityLow, nil
	default:
		return 0, fmt.Errorf("invalid priority %q (want high|normal|low)", s)
	}
}

// PriorityLabel maps a stored integer to the API string label.
// Unknown values map to normal.
func PriorityLabel(p int) string {
	switch p {
	case PriorityHigh:
		return PriorityLabelHigh
	case PriorityLow:
		return PriorityLabelLow
	default:
		return PriorityLabelNormal
	}
}

// ValidPriorityInt reports whether p is one of the three defined levels.
func ValidPriorityInt(p int) bool {
	return p == PriorityLow || p == PriorityNormal || p == PriorityHigh
}

// BeforeCreate ensures Priority defaults to normal when unset (Go zero value).
func (r *Run) BeforeCreate(tx *gorm.DB) error {
	if r.Priority == 0 {
		r.Priority = PriorityNormal
	}
	return nil
}
