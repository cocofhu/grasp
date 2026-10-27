package streamjson

import (
	"encoding/json"
	"log"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

// Gemini's stream-json is a separate dialect from the Claude-family stream:
// text and tool calls are top-level events, and result.status carries failure.
type geminiEvent struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"session_id"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolName   string          `json:"tool_name"`
	ToolID     string          `json:"tool_id"`
	Parameters json.RawMessage `json:"parameters"`
	Status     string          `json:"status"`
	Output     string          `json:"output"`
	Severity   string          `json:"severity"`
	Message    string          `json:"message"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
	Stats *struct {
		geminiUsage
		Models map[string]geminiUsage `json:"models"`
	} `json:"stats"`
}

type geminiUsage struct {
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Cached       int64  `json:"cached"`
	Input        *int64 `json:"input"`
}

func (u geminiUsage) tokens() provider.TokenUsage {
	// Gemini input_tokens includes cached tokens; the native input field is
	// the uncached portion. Our token buckets are disjoint and Total adds them.
	input := max(u.InputTokens-u.Cached, 0)
	if u.Input != nil {
		input = *u.Input
	}
	return provider.TokenUsage{InputTokens: input, OutputTokens: u.OutputTokens, CacheReadTokens: u.Cached}
}

func parseGeminiLine(line []byte) oneshot.ParseResult {
	var ev geminiEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		log.Printf("gemini: skip non-json line: %v (snippet=%q)", err, truncateForLog(line, 120))
		return oneshot.ParseResult{}
	}
	res := oneshot.ParseResult{SessionID: ev.SessionID}
	switch ev.Type {
	case "message":
		if ev.Role == "assistant" && ev.Content != "" {
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: ev.Content})
		}
	case "tool_use":
		res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindToolUse, ToolCallID: ev.ToolID, ToolTitle: ev.ToolName, RawInput: ev.Parameters})
	case "tool_result":
		status, text := "completed", ev.Output
		if ev.Status == "error" {
			status = "failed"
			if ev.Error != nil && ev.Error.Message != "" {
				text = ev.Error.Message
			}
		}
		res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindToolResult, ToolCallID: ev.ToolID, ToolStatus: status, Text: text})
	case "error":
		kind := oneshot.KindError
		if ev.Severity == "warning" {
			kind = oneshot.KindLog
		} else {
			res.StopReason = "failed"
		}
		if ev.Message != "" {
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: kind, Text: ev.Message})
		}
	case "result":
		res.StopReason = "end_turn"
		if ev.Status == "error" {
			res.StopReason = "failed"
			text := "Gemini request failed"
			if ev.Error != nil && ev.Error.Message != "" {
				text = ev.Error.Message
			}
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: text})
		}
		if ev.Stats != nil {
			res.Usage = make(map[string]provider.TokenUsage, len(ev.Stats.Models)+1)
			for model, usage := range ev.Stats.Models {
				res.Usage[model] = usage.tokens()
			}
			if len(res.Usage) == 0 {
				res.Usage["default"] = ev.Stats.geminiUsage.tokens()
			}
		}
	}
	return res
}
