package cursorsdk

import (
	"encoding/json"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"

	"backend/internal/provider"
)

// event is one bridge event line.
type event struct {
	Type   string          `json:"type"`
	Update json.RawMessage `json:"update"`
	State  string          `json:"state"`
	runEnd
}

// update is one SDK interaction update.
type update struct {
	Type     string    `json:"type"`
	Text     string    `json:"text"`
	CallID   string    `json:"callId"`
	ToolCall *toolCall `json:"toolCall"`
	Usage    *sdkUsage `json:"usage"`
}

type toolCall struct {
	Type   string          `json:"type"`
	Args   json.RawMessage `json:"args"`
	Result *struct {
		Status string          `json:"status"`
		Value  json.RawMessage `json:"value"`
		Error  json.RawMessage `json:"error"`
	} `json:"result"`
}

// onEvent handles one bridge event line.
func (s *session) onEvent(raw json.RawMessage) {
	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil {
		log.Printf("cursor: 事件无法解析: %v", err)
		return
	}
	switch ev.Type {
	case "delta":
		var u update
		if json.Unmarshal(ev.Update, &u) == nil {
			s.onUpdate(u)
		}
	case "run_end":
		s.mu.Lock()
		if t := s.turn; t != nil && !t.ended {
			t.ended, t.end = true, ev.runEnd
			close(t.done)
		}
		s.mu.Unlock()
	case "connection":
		log.Printf("cursor: 连接状态 %s", ev.State)
	}
}

func (s *session) onUpdate(u update) {
	s.mu.Lock()
	t := s.turn
	if t == nil {
		s.mu.Unlock()
		return
	}
	var out map[string]any
	switch u.Type {
	case "text-delta":
		if u.Text != "" {
			out = provider.ChunkUpdate("agent_message_chunk", u.Text)
		}
	case "thinking-delta":
		if u.Text != "" {
			out = provider.ChunkUpdate("agent_thought_chunk", u.Text)
		}
	case "model-call-completed":
		if u.Usage != nil {
			t.calls = t.calls.Plus(u.Usage.tokens())
		}
	case "tool-call-started":
		if u.ToolCall != nil && u.CallID != "" {
			s.openTools[u.CallID] = true
			title, input := toolTitleInput(u.ToolCall)
			out = map[string]any{"sessionUpdate": "tool_call", "toolCallId": u.CallID, "title": title, "status": "in_progress"}
			if len(input) > 0 && string(input) != "null" && string(input) != "{}" {
				out["rawInput"] = input
			}
		}
	case "tool-call-completed":
		if u.ToolCall != nil && u.CallID != "" {
			delete(s.openTools, u.CallID)
			text, failed := toolResult(u.ToolCall)
			status := "completed"
			if failed {
				status = "failed"
			}
			out = map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": u.CallID, "status": status}
			if ms := toolDuration(u.ToolCall); ms > 0 {
				out["durationMs"] = ms
			}
			if text != "" {
				out["content"] = []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": text}}}
			}
		}
	}
	s.mu.Unlock()
	if out != nil {
		s.emitUpdate(out)
	}
}

// toolTitleInput names a tool call and the arguments worth showing; MCP
// calls show the server tool and its own arguments.
func toolTitleInput(c *toolCall) (string, json.RawMessage) {
	if c.Type == "mcp" {
		var a struct {
			ToolName string          `json:"toolName"`
			Args     json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(c.Args, &a)
		if a.ToolName == "" {
			a.ToolName = "mcp"
		}
		return a.ToolName, a.Args
	}
	return toolDisplayName(c.Type), c.Args
}

// toolDisplayName turns a camelCase tool type into a title: shell → Shell,
// readLints → Read lints.
func toolDisplayName(kind string) string {
	if kind == "" {
		return "Tool"
	}
	var b strings.Builder
	for i, r := range kind {
		switch {
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsUpper(r):
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maxToolResultText caps the tool output forwarded per call; the server
// redacts and truncates again before anything reaches a browser.
const maxToolResultText = 4000

// toolResult extracts readable output and whether the call failed.
func toolResult(c *toolCall) (string, bool) {
	r := c.Result
	if r == nil {
		return "", false
	}
	if r.Status != "success" {
		text := resultText(r.Error)
		if text == "" {
			text = resultText(r.Value)
		}
		if text == "" {
			text = r.Status
		}
		return clipText(text), true
	}
	var mcp struct {
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(r.Value, &mcp)
	return clipText(resultText(r.Value)), mcp.IsError
}

// toolDuration reads the shell's execution time.
func toolDuration(c *toolCall) int64 {
	if c.Result == nil {
		return 0
	}
	var v struct {
		ExecutionTime int64 `json:"executionTime"`
	}
	_ = json.Unmarshal(c.Result.Value, &v)
	return v.ExecutionTime
}

// resultText prefers the human-readable fields of a result payload, falling
// back to the compact JSON.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		var parts []string
		for _, k := range []string{"stdout", "stderr", "output", "content", "message", "markdown", "text", "error"} {
			if v, ok := m[k]; ok {
				if t := flatText(v); strings.TrimSpace(t) != "" {
					parts = append(parts, t)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return string(raw)
}

// flatText reads a string, or the text of MCP content blocks
// ([{"text":"…"}] or [{"text":{"text":"…"}}]).
func flatText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text json.RawMessage `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		var t string
		if json.Unmarshal(b.Text, &t) == nil {
			parts = append(parts, t)
			continue
		}
		var inner struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(b.Text, &inner) == nil && inner.Text != "" {
			parts = append(parts, inner.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func clipText(s string) string {
	if len(s) <= maxToolResultText {
		return s
	}
	cut := maxToolResultText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func (s *session) emitUpdate(u map[string]any) { s.emit(provider.UpdateFrame(s.agentID, u)) }

func (s *session) emit(frame map[string]any) { provider.SendFrame(s.onEventFrame, frame) }
