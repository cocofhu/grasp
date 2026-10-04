package services

import (
	"encoding/json"
	"testing"
)

func TestExtractPmAgentTextNestedOpEvent(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"op": "event",
		"data": map[string]any{
			"type": "session_update",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "Hello"},
			},
		},
	})
	if got := extractPmAgentText(raw); got != "Hello" {
		t.Fatalf("got %q want Hello", got)
	}
}

func TestExtractPmAgentTextIgnoresThought(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"type": "session_update",
		"update": map[string]any{
			"sessionUpdate": "agent_thought_chunk",
			"content":       map[string]any{"text": "think"},
		},
	})
	if got := extractPmAgentText(raw); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractPmAgentTextParts(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"type": "session_update",
		"update": map[string]any{
			"sessionUpdate": "agentMessageChunk",
			"content": []any{
				map[string]any{"text": "A"},
				map[string]any{"parts": []any{map[string]any{"text": "B"}}},
			},
		},
	})
	if got := extractPmAgentText(raw); got != "AB" {
		t.Fatalf("got %q want AB", got)
	}
}

func TestNormalizePmKind(t *testing.T) {
	if got := normalizePmKind("agentMessageChunk"); got != "agent_message_chunk" {
		t.Fatalf("got %q", got)
	}
}
