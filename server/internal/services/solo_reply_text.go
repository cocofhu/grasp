package services

import (
	"encoding/json"
	"strings"
)

// Native text chunks within one message must stay byte-for-byte contiguous.
// A tool or thought between messages is a paragraph boundary in the saved
// reply, even when neither adjacent message supplied whitespace of its own.
type soloReplyTextState struct {
	separated bool
	tools     map[string]bool
}

func appendSoloReplyText(text *strings.Builder, state *soloReplyTextState, raw json.RawMessage) {
	update, kind := agentSessionUpdate(raw)
	switch {
	case kind == "agent_message_chunk":
		delta := contentTextAny(update["content"])
		if delta == "" {
			return
		}
		if state.separated && text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(delta)
		state.separated = false
	case kind == "agent_thought_chunk" && contentTextAny(update["content"]) != "":
		state.separated = true
	case strings.Contains(kind, "tool_call"), strings.Contains(kind, "toolcall"):
		id := soloReplyToolID(update)
		if id != "" && state.tools[id] {
			return
		}
		state.separated = true
		if id != "" {
			if state.tools == nil {
				state.tools = map[string]bool{}
			}
			state.tools[id] = true
		}
	}
}

func soloReplyToolID(update map[string]any) string {
	for _, key := range []string{"toolCallId", "tool_call_id", "id", "callId"} {
		if id, _ := update[key].(string); id != "" {
			return id
		}
	}
	for _, key := range []string{"toolCall", "tool", "call"} {
		if tool, ok := update[key].(map[string]any); ok {
			for _, field := range []string{"toolCallId", "id", "callId"} {
				if id, _ := tool[field].(string); id != "" {
					return id
				}
			}
			break
		}
	}
	return ""
}
