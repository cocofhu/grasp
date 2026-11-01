package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// ConversationHistoryRequest pages backward through one fixed history snapshot.
// Cursor is opaque and is always checked against the token's current session.
type ConversationHistoryRequest struct {
	View   string `json:"view"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// ConversationHistoryMessage deliberately excludes raw ACP events and inline
// attachment bytes. Tool and file activity remains in the original transcript.
type ConversationHistoryMessage struct {
	ID          string                          `json:"id"`
	Role        string                          `json:"role"`
	Content     string                          `json:"content"`
	Status      string                          `json:"status,omitempty"`
	Error       string                          `json:"error,omitempty"`
	Images      []models.PromptImage            `json:"attachments,omitempty"`
	CreatedAt   time.Time                       `json:"createdAt"`
	ToolResults []ConversationHistoryToolResult `json:"tool_results,omitempty"`
}

type ConversationHistoryToolResult struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
	Text   string `json:"text,omitempty"`
}

type ConversationHistoryPage struct {
	ConversationID string                       `json:"conversationId"`
	Ref            string                       `json:"ref,omitempty"`
	HistoryVersion string                       `json:"history_version"`
	Summary        string                       `json:"summary,omitempty"`
	Messages       []ConversationHistoryMessage `json:"messages"`
	NextCursor     string                       `json:"next_cursor,omitempty"`
	HasMore        bool                         `json:"has_more"`
}

type ConversationHistoryProvider interface {
	ReadConversationHistory(runID string, req ConversationHistoryRequest) (ConversationHistoryPage, error)
}

func (h *Host) SetConversationHistoryProvider(provider ConversationHistoryProvider) {
	h.mu.Lock()
	h.conversationHistory = provider
	h.mu.Unlock()
}

func (h *Host) getConversationHistory(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "get_history failed: " + ErrUnauthorized.Error(), true
	}
	h.mu.RLock()
	provider := h.conversationHistory
	h.mu.RUnlock()
	if provider == nil {
		return "get_history failed: 当前会话历史不可用", true
	}
	// Reject scope arguments rather than accidentally accepting a caller's
	// conversation ID now or after a future provider extension.
	for key := range args {
		if key != "view" && key != "cursor" && key != "limit" {
			return fmt.Sprintf("get_history failed: unsupported argument %q", key), true
		}
	}
	req := ConversationHistoryRequest{View: asString(args["view"]), Cursor: asString(args["cursor"]), Limit: asInt(args["limit"])}
	if req.View == "" {
		req.View = "summary"
	}
	if req.View != "summary" && req.View != "messages" {
		return "get_history failed: view must be summary or messages", true
	}
	if req.Limit == 0 {
		req.Limit = 20
	}
	if req.Limit < 1 || req.Limit > 50 {
		return "get_history failed: limit must be between 1 and 50", true
	}
	page, err := provider.ReadConversationHistory(runID, req)
	if err != nil {
		return "get_history failed: " + err.Error(), true
	}
	data, err := json.Marshal(page)
	if err != nil {
		return "get_history failed: " + err.Error(), true
	}
	return string(data), false
}

func conversationHistoryTool() map[string]any {
	return map[string]any{
		"name":        "get_history",
		"description": "读取当前 Solo 会话的历史，供模型、模型组或编码工具切换后接续任务。summary 返回覆盖会话的用户要求与更正概览及近期对话；messages 按页返回完整消息正文。每页内按时间正序，next_cursor 继续读取更早记录，history_version 固定本次分页快照。会话由当前授权确定。",
		"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		"inputSchema": map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"view":   map[string]any{"type": "string", "enum": []string{"summary", "messages"}, "default": "summary"},
				"cursor": map[string]any{"type": "string", "description": "可选：上一页返回的 next_cursor，用于向前回查同一快照"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 20},
			},
		},
	}
}
