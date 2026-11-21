package provider

import (
	"encoding/json"
	"log"
)

// SendFrame hands frame to send as JSON; a nil send drops it.
func SendFrame(send func(json.RawMessage), frame map[string]any) {
	if send == nil {
		return
	}
	b, err := json.Marshal(frame)
	if err != nil {
		log.Printf("provider: 事件序列化失败: %v", err)
		return
	}
	send(b)
}

// UpdateFrame wraps one ACP-shaped session update.
func UpdateFrame(session string, update map[string]any) map[string]any {
	return map[string]any{"type": "session_update", "sessionId": session, "update": update}
}

// ChunkUpdate streams text as kind (agent_message_chunk, agent_thought_chunk).
func ChunkUpdate(kind, text string) map[string]any {
	return map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": text}}
}

// ErrorFrame carries a diagnostic the transcript shows before prompt_done.
func ErrorFrame(text string) map[string]any {
	return map[string]any{"op": "raw", "type": "error_text", "text": text}
}

// PromptDoneFrame ends a turn. Usage is included only when reported; partial
// marks usage of a turn that did not finish.
func PromptDoneFrame(session, stopReason string, usage map[string]TokenUsage, partial bool) map[string]any {
	frame := map[string]any{"type": "prompt_done", "sessionId": session, "stopReason": stopReason}
	if len(usage) > 0 {
		frame["usage"] = usage
		if partial {
			frame["usagePartial"] = true
		}
	}
	return frame
}

// WithAttachments writes images to files and lists their paths after text.
func WithAttachments(text string, images []PromptImage) (string, error) {
	if len(images) == 0 {
		return text, nil
	}
	files, err := MaterializeAttachments(images)
	if err != nil {
		return "", err
	}
	return AppendAttachmentRefs(text, files), nil
}
