package services

import (
	"encoding/json"
	"strings"
)

// extractAgentText pulls agent_message_chunk text from a raw ACP frame.
func extractAgentText(raw json.RawMessage) string {
	var envelope map[string]any
	if json.Unmarshal(raw, &envelope) != nil {
		return ""
	}
	ev := envelope
	if op, _ := envelope["op"].(string); op == "event" {
		if data, ok := envelope["data"].(map[string]any); ok {
			ev = data
		}
	} else if data, ok := envelope["data"].(map[string]any); ok {
		if _, hasType := envelope["type"]; !hasType {
			ev = data
		}
	}
	typ, _ := ev["type"].(string)
	if typ != "session_update" {
		return ""
	}
	update, _ := ev["update"].(map[string]any)
	if update == nil {
		return ""
	}
	kind := stringifyKind(update["sessionUpdate"])
	if kind == "" {
		kind = stringifyKind(update["session_update"])
	}
	if kind == "" {
		kind = stringifyKind(update["type"])
	}
	if normalizeACPKind(kind) != "agent_message_chunk" {
		return ""
	}
	return contentTextAny(update["content"])
}

func stringifyKind(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func normalizeACPKind(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				prev := s[i-1]
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
					b.WriteByte('_')
				}
			}
			b.WriteByte(c - 'A' + 'a')
			continue
		}
		if c == '-' {
			b.WriteByte('_')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func contentTextAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var out string
		for _, e := range x {
			out += contentTextAny(e)
		}
		return out
	case map[string]any:
		if t, ok := x["text"].(string); ok {
			return t
		}
		if parts, ok := x["parts"].([]any); ok {
			return contentTextAny(parts)
		}
	}
	return ""
}
