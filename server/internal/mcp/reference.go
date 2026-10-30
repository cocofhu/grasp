package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ReferenceReader reads conversations the user explicitly attached to the
// current Solo session with @. The provider authorizes every read.
type ReferenceReader interface {
	ReadReference(runID, refID string, req ConversationHistoryRequest) (ConversationHistoryPage, error)
}

// ReferenceHintError answers a read_reference call for a reference the Agent
// reads itself (GitHub): the hint carries the commands to run instead.
type ReferenceHintError struct {
	Hint string
}

func (e *ReferenceHintError) Error() string { return e.Hint }

// referenceReader returns the configured provider when it also reads
// references; the conversation history service implements both.
func (h *Host) referenceReader() ReferenceReader {
	h.mu.RLock()
	provider := h.conversationHistory
	h.mu.RUnlock()
	reader, _ := provider.(ReferenceReader)
	return reader
}

func (h *Host) readReference(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "read_reference failed: " + ErrUnauthorized.Error(), true
	}
	reader := h.referenceReader()
	if reader == nil {
		return "read_reference failed: 引用读取不可用", true
	}
	for key := range args {
		if key != "ref" && key != "view" && key != "cursor" && key != "limit" {
			return fmt.Sprintf("read_reference failed: unsupported argument %q", key), true
		}
	}
	ref := asString(args["ref"])
	if ref == "" {
		return "read_reference failed: ref is required", true
	}
	req := ConversationHistoryRequest{View: asString(args["view"]), Cursor: asString(args["cursor"]), Limit: asInt(args["limit"])}
	if req.View == "" {
		req.View = "summary"
	}
	if req.View != "summary" && req.View != "messages" {
		return "read_reference failed: view must be summary or messages", true
	}
	if req.Limit == 0 {
		req.Limit = 20
	}
	if req.Limit < 1 || req.Limit > 50 {
		return "read_reference failed: limit must be between 1 and 50", true
	}
	page, err := reader.ReadReference(runID, ref, req)
	if err != nil {
		var hint *ReferenceHintError
		if errors.As(err, &hint) {
			return hint.Hint, false
		}
		return "read_reference failed: " + err.Error(), true
	}
	data, err := json.Marshal(page)
	if err != nil {
		return "read_reference failed: " + err.Error(), true
	}
	return string(data), false
}

func referenceTool() map[string]any {
	return map[string]any{
		"name":        "read_reference",
		"description": "读取用户在本会话中用 @ 显式引用的另一个会话。ref 只能取提示词「用户引用的上下文」里给出的 ref-xxx；本会话没有引用时调用会返回错误。summary 返回被引用会话的用户要求概览；messages 按页返回完整消息，next_cursor 继续读取更早记录。内容固定在引用时的快照。GitHub 引用请按提示词里的 gh 命令直接读取。",
		"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		"inputSchema": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"ref"},
			"properties": map[string]any{
				"ref":    map[string]any{"type": "string", "description": "提示词里给出的引用句柄，形如 ref-xxxxxxxx"},
				"view":   map[string]any{"type": "string", "enum": []string{"summary", "messages"}, "default": "summary"},
				"cursor": map[string]any{"type": "string", "description": "可选：上一页返回的 next_cursor，只能用于同一个 ref"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 20},
			},
		},
	}
}
