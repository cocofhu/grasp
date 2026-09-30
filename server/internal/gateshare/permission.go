package gateshare

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// Public write / preview action kinds checked by Allow.
const (
	ActionReply  = "reply"
	ActionCancel = "cancel"
	ActionDecide = "decide"
	// ActionLive is generate / accept / discard / steer (not a plain comment).
	ActionLive = "live"
)

// ParsePermissionPreset validates a create-time preset. Empty → full (default).
func ParsePermissionPreset(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return models.SharePermissionFull, true
	}
	switch p {
	case models.SharePermissionFull, models.SharePermissionReactOnly:
		return p, true
	default:
		return "", false
	}
}

// NormalizePermissionPreset maps stored / empty values for read paths.
// Unknown legacy values fall back to full so existing links stay usable.
func NormalizePermissionPreset(raw string) string {
	p, ok := ParsePermissionPreset(raw)
	if !ok {
		return models.SharePermissionFull
	}
	return p
}

// Allow reports whether preset permits the given public action.
// full: reply + cancel + decide + live.
// react_only: reply + cancel only; decide and Live writes are denied.
func Allow(preset, action string) bool {
	preset = NormalizePermissionPreset(preset)
	switch strings.TrimSpace(action) {
	case ActionReply, ActionCancel:
		return true
	case ActionDecide, ActionLive:
		return preset == models.SharePermissionFull
	default:
		return false
	}
}

// FilterActionsByPreset removes decide keys when the preset forbids them.
// Hot/cold session rules must already have populated actions; this only overlays the preset.
func FilterActionsByPreset(actions map[string]string, preset string) map[string]string {
	if len(actions) == 0 || Allow(preset, ActionDecide) {
		return actions
	}
	out := make(map[string]string, len(actions))
	for k, v := range actions {
		switch k {
		case "approve", "confirm", "reject":
			continue
		default:
			out[k] = v
		}
	}
	return out
}
