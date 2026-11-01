package models

import (
	"fmt"
	"strings"
)

// CompositeText is a paragraph variable value with optional image attachments.
// Stored as JSON {text, images[]} or as a plain string when no images are present.
type CompositeText struct {
	Text   string        `json:"text"`
	Images []PromptImage `json:"images,omitempty"`
	// References and ReferenceUser are set only on Run.FirstMessage, after
	// the launcher's @ picks were validated; ReferenceUser is the sender.
	References    []MessageReference `json:"references,omitempty"`
	ReferenceUser string             `json:"referenceUser,omitempty"`
}

// IsCompositeText reports whether v is a {text, images} map shape.
func IsCompositeText(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, hasText := m["text"]
	_, hasImages := m["images"]
	return hasText || hasImages
}

// VarDisplayText returns the human-readable text for interpolation and display.
// Plain strings pass through; composite values return text; other types fmt.Sprint.
func VarDisplayText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if ct := AsCompositeText(v); ct != nil {
		return ct.Text
	}
	return fmt.Sprint(v)
}

// AsCompositeText coerces v into CompositeText when it carries the composite shape.
func AsCompositeText(v any) *CompositeText {
	switch x := v.(type) {
	case CompositeText:
		return &x
	case *CompositeText:
		return x
	case map[string]any:
		ct := &CompositeText{}
		if t, ok := x["text"].(string); ok {
			ct.Text = t
		}
		if imgs, ok := x["images"].([]any); ok {
			for _, im := range imgs {
				if m, ok := im.(map[string]any); ok {
					pi := PromptImage{}
					if d, ok := m["data"].(string); ok {
						pi.Data = d
					}
					if r, ok := m["ref"].(string); ok {
						pi.Ref = r
					}
					if mt, ok := m["mimeType"].(string); ok {
						pi.MimeType = mt
					}
					if n, ok := m["name"].(string); ok {
						pi.Name = n
					}
					switch sb := m["sizeBytes"].(type) {
					case float64:
						pi.SizeBytes = int64(sb)
					case int64:
						pi.SizeBytes = sb
					case int:
						pi.SizeBytes = int64(sb)
					}
					if pi.Data != "" || pi.Ref != "" {
						ct.Images = append(ct.Images, pi)
					}
				}
			}
		}
		return ct
	}
	return nil
}

// ExtractImages returns image attachments from a composite value, or nil.
func ExtractImages(v any) []PromptImage {
	if ct := AsCompositeText(v); ct != nil && len(ct.Images) > 0 {
		out := make([]PromptImage, len(ct.Images))
		copy(out, ct.Images)
		return out
	}
	return nil
}

// IsBlankVar reports whether a variable value is empty for validation purposes.
// Composite values are non-blank when text is non-empty or images are present.
func IsBlankVar(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	if ct := AsCompositeText(v); ct != nil {
		if strings.TrimSpace(ct.Text) != "" {
			return false
		}
		return len(ct.Images) == 0
	}
	switch v.(type) {
	case bool, int, int64, float32, float64:
		return false
	}
	return false
}
