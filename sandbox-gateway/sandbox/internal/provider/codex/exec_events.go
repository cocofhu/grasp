package codex

import (
	"encoding/json"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

// Current codex exec --json uses thread/turn/item events. Keep the legacy
// parser for older CLI installations, but never silently discard current items.
func parseExecEvent(line []byte) (oneshot.ParseResult, bool) {
	var e struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
		Message  string `json:"message"`
		Error    struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage tokenCnt `json:"usage"`
		Item  struct {
			ID      string          `json:"id"`
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Command string          `json:"command"`
			Output  string          `json:"aggregated_output"`
			Status  string          `json:"status"`
			Tool    string          `json:"tool"`
			Result  json.RawMessage `json:"result"`
			Changes json.RawMessage `json:"changes"`
		} `json:"item"`
	}
	if json.Unmarshal(line, &e) != nil {
		return oneshot.ParseResult{}, false
	}
	r := oneshot.ParseResult{}
	switch e.Type {
	case "thread.started":
		r.SessionID = e.ThreadID
	case "turn.started":
	case "turn.completed":
		r.StopReason = "end_turn"
		r.Usage = map[string]provider.TokenUsage{"default": {InputTokens: e.Usage.InputTokens, OutputTokens: e.Usage.OutputTokens, CacheReadTokens: e.Usage.CachedInputTokens}}
	case "turn.failed", "error":
		r.StopReason = "failed"
		message := e.Error.Message
		if message == "" {
			message = e.Message
		}
		r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: message})
	case "item.started", "item.updated", "item.completed":
		i := e.Item
		switch i.Type {
		case "agent_message", "reasoning":
			if e.Type == "item.completed" && i.Text != "" {
				kind := oneshot.KindText
				if i.Type == "reasoning" {
					kind = oneshot.KindThinking
				}
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: kind, Text: i.Text})
			}
		case "command_execution", "file_change", "mcp_tool_call", "web_search":
			title := i.Command
			if title == "" {
				title = i.Tool
			}
			if title == "" {
				title = i.Type
			}
			if e.Type == "item.started" {
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindToolUse, ToolCallID: i.ID, ToolTitle: title})
			}
			if e.Type == "item.completed" {
				output := i.Output
				if i.Type == "file_change" {
					output = string(i.Changes)
				}
				if i.Type == "mcp_tool_call" {
					output = string(i.Result)
				}
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindToolResult, ToolCallID: i.ID, ToolTitle: title, ToolStatus: i.Status, Text: output})
			}
		case "error":
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: i.Text})
		}
	default:
		return r, false
	}
	return r, true
}
