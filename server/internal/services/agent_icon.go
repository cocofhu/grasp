package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// AgentIcon is one optional linear identity mark stored on an Agent.
// It records a built-in catalog id and one of eight colors. Emoji is not a field.
type AgentIcon struct {
	IconID string `json:"iconId"`
	Color  string `json:"color"`
}

// identityIconIDs is the built-in linear catalog. Keep it aligned with
// web/src/lib/agent/identityIcons.ts. There is no emoji catalog.
var identityIconIDs = []string{
	"folder", "dollar", "book", "graduation", "pencil", "pen", "code", "play",
	"music", "popcorn", "palette", "stethoscope", "asterisk", "lotus", "briefcase",
	"chart", "kettlebell", "dumbbell", "calendar", "scales", "compass", "plane",
	"globe", "wrench", "paw", "flask", "brain", "heart", "plant",
}

var identityColorIDs = []string{
	"black", "red", "orange", "yellow", "green", "blue", "purple", "pink",
}

var (
	identityIconIDSet  = map[string]struct{}{}
	identityColorIDSet = map[string]struct{}{}
	emojiFieldKeys     = map[string]struct{}{
		"emoji": {}, "emojiId": {}, "emojis": {}, "emojiKind": {},
	}
)

func init() {
	for _, id := range identityIconIDs {
		identityIconIDSet[id] = struct{}{}
	}
	for _, id := range identityColorIDs {
		identityColorIDSet[id] = struct{}{}
	}
}

// ValidateAgentIcon accepts a missing icon. A present icon must be a catalog
// id plus one of the eight colors. Callers wrap nothing: the error is already
// ErrInvalidAgent.
func ValidateAgentIcon(icon *AgentIcon) error {
	if icon == nil {
		return nil
	}
	if _, ok := identityIconIDSet[icon.IconID]; !ok {
		return fmt.Errorf("%w: unknown identity icon %q", ErrInvalidAgent, icon.IconID)
	}
	if _, ok := identityColorIDSet[icon.Color]; !ok {
		return fmt.Errorf("%w: unknown identity color %q", ErrInvalidAgent, icon.Color)
	}
	return nil
}

func cloneAgentIcon(icon *AgentIcon) *AgentIcon {
	if icon == nil {
		return nil
	}
	cp := *icon
	return &cp
}

// RejectEmojiIdentity fails the request when any object carries an emoji field
// or an emoji kind. Existing agents are left untouched because callers validate
// before the first write.
func RejectEmojiIdentity(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if emojiIdentityPresent(v) {
		return fmt.Errorf("%w: emoji identity is not supported", ErrInvalidAgent)
	}
	return nil
}

func emojiIdentityPresent(v any) bool {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if _, ok := emojiFieldKeys[k]; ok {
				return true
			}
			if k == "kind" {
				if s, ok := child.(string); ok && strings.EqualFold(strings.TrimSpace(s), "emoji") {
					return true
				}
			}
			if emojiIdentityPresent(child) {
				return true
			}
		}
	case []any:
		for _, child := range n {
			if emojiIdentityPresent(child) {
				return true
			}
		}
	}
	return false
}

// RejectUnknownIconFields allows only iconId and color on the icon object.
// SVG bodies, image URLs, and emoji keys are refused instead of stored.
func RejectUnknownIconFields(raw []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return err
	}
	iconRaw, ok := top["icon"]
	if !ok || bytes.Equal(bytes.TrimSpace(iconRaw), []byte("null")) {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(iconRaw, &obj); err != nil {
		return fmt.Errorf("%w: icon must be an object", ErrInvalidAgent)
	}
	for k := range obj {
		if k != "iconId" && k != "color" {
			return fmt.Errorf("%w: unsupported icon field %q", ErrInvalidAgent, k)
		}
	}
	return nil
}
