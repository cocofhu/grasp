package pi

import (
	"cmp"
	"encoding/json"
	"log"
	"regexp"
	"strings"

	"backend/internal/provider"
)

type event struct {
	Type                  string `json:"type"`
	AssistantMessageEvent *struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent"`
	Message    json.RawMessage `json:"message"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Args       json.RawMessage `json:"args"`
	Result     *toolResult     `json:"result"`
	IsError    bool            `json:"isError"`
	Success    bool            `json:"success"`
	FinalError string          `json:"finalError"`
	// WillRetry on agent_end: Pi retries the failed request as another run.
	WillRetry bool `json:"willRetry"`
	// extension_ui_request
	ID     string `json:"id"`
	Method string `json:"method"`
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type message struct {
	Role         string `json:"role"`
	Model        string `json:"model"`
	StopReason   string `json:"stopReason"`
	ErrorMessage string `json:"errorMessage"`
	Usage        *struct {
		Input      int64 `json:"input"`
		Output     int64 `json:"output"`
		CacheRead  int64 `json:"cacheRead"`
		CacheWrite int64 `json:"cacheWrite"`
	} `json:"usage"`
}

// onEvent handles one event line of the agent.
func (s *session) onEvent(raw json.RawMessage) {
	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil {
		log.Printf("pi: 事件无法解析: %v", err)
		return
	}
	if ev.Type == "extension_ui_request" {
		s.answerDialog(ev.ID, ev.Method)
		return
	}
	s.mu.Lock()
	t := s.turn
	if t == nil {
		s.mu.Unlock()
		return
	}
	var chunks [][2]string
	var updates []map[string]any
	switch ev.Type {
	case "agent_start":
		t.ended = false
	case "agent_end":
		chunks = s.flushLocked(chunks)
		if ev.WillRetry {
			// The retry decides the outcome; this attempt's error is not it.
			t.errText = ""
		}
	case "message_update":
		if d := ev.AssistantMessageEvent; d != nil && d.Delta != "" {
			switch d.Type {
			case "text_delta":
				if text := s.text.drain(d.Delta); text != "" {
					chunks = append(chunks, [2]string{"agent_message_chunk", text})
				}
			case "thinking_delta":
				chunks = append(chunks, [2]string{"agent_thought_chunk", d.Delta})
			}
		}
	case "message_end":
		var m message
		if json.Unmarshal(ev.Message, &m) == nil {
			s.messageEndLocked(t, m)
		}
	case "turn_end":
		chunks = s.flushLocked(chunks)
	case "tool_execution_start":
		s.openTools[ev.ToolCallID] = true
		update := map[string]any{"sessionUpdate": "tool_call", "toolCallId": ev.ToolCallID, "title": ev.ToolName, "status": "in_progress"}
		if len(ev.Args) > 0 && string(ev.Args) != "null" && string(ev.Args) != "{}" {
			update["rawInput"] = ev.Args
		}
		updates = append(updates, update)
	case "tool_execution_end":
		delete(s.openTools, ev.ToolCallID)
		status := "completed"
		if ev.IsError {
			status = "failed"
		}
		update := map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": ev.ToolCallID, "status": status}
		if text := ev.Result.text(); text != "" {
			update["content"] = []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": text}}}
		}
		updates = append(updates, update)
	case "auto_retry_end":
		if !ev.Success {
			t.errText = cmp.Or(ev.FinalError, t.errText, "Pi 自动重试失败")
		}
	}
	s.mu.Unlock()
	for _, c := range chunks {
		s.emitChunk(c[0], c[1])
	}
	for _, u := range updates {
		s.emitUpdate(u)
	}
	// A run Pi retries goes on as another run; a retry that gave up or was
	// cancelled during its backoff sends no further agent_end.
	if (ev.Type == "agent_end" && !ev.WillRetry) || (ev.Type == "auto_retry_end" && !ev.Success) {
		// Settle only after the run's last output went out.
		s.mu.Lock()
		t.ended = true
		s.evaluateLocked(t)
		s.mu.Unlock()
	}
}

func (s *session) messageEndLocked(t *activeTurn, m message) {
	switch m.Role {
	case "user":
		t.users++
	case "assistant":
		if m.Usage != nil {
			model := m.Model
			if model == "" {
				model = "default"
			}
			u := t.usage[model]
			u.InputTokens += m.Usage.Input
			u.OutputTokens += m.Usage.Output
			u.CacheReadTokens += m.Usage.CacheRead
			u.CacheWriteTokens += m.Usage.CacheWrite
			t.usage[model] = u
		}
		switch m.StopReason {
		case "aborted":
			t.aborted = true
		case "error":
			t.errText = m.ErrorMessage
			if t.errText == "" {
				t.errText = "Pi 模型请求失败"
			}
		}
	}
}

func (s *session) flushLocked(chunks [][2]string) [][2]string {
	if text := s.text.flush(); text != "" {
		chunks = append(chunks, [2]string{"agent_message_chunk", text})
	}
	return chunks
}

// answerDialog dismisses an extension's dialog: no one can answer it.
func (s *session) answerDialog(id, method string) {
	switch method {
	case "select", "confirm", "input", "editor":
		go func() {
			if err := s.proc.send(map[string]any{"type": "extension_ui_response", "id": id, "cancelled": true}); err != nil {
				log.Printf("pi: 应答扩展对话框失败: %v", err)
			}
		}()
	}
}

func (r *toolResult) text() string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

// controlTokenRE matches inline tool-call control tokens (e.g. <|foo>bar,
// <name|>) that some models leak into the text stream.
var controlTokenRE = regexp.MustCompile(`<\|[A-Za-z0-9_-]+>[A-Za-z0-9_-]*|<[A-Za-z0-9_-]+\|>`)

// sanitizer strips control tokens from streamed text, holding back a
// trailing "<…" that may still complete into one.
type sanitizer struct{ held string }

func (z *sanitizer) drain(delta string) string {
	cleaned := controlTokenRE.ReplaceAllString(z.held+delta, "")
	hold := 0
	if i := strings.LastIndexByte(cleaned, '<'); i >= 0 && !strings.ContainsRune(cleaned[i:], '>') {
		hold = len(cleaned) - i
	}
	z.held = cleaned[len(cleaned)-hold:]
	return cleaned[:len(cleaned)-hold]
}

func (z *sanitizer) flush() string {
	out := controlTokenRE.ReplaceAllString(z.held, "")
	z.held = ""
	return out
}

func (s *session) emitChunk(kind, text string) { s.emitUpdate(provider.ChunkUpdate(kind, text)) }

func (s *session) emitUpdate(update map[string]any) {
	s.emit(provider.UpdateFrame(s.sessionFile, update))
}

func (s *session) emitError(text string) { s.emit(provider.ErrorFrame(text)) }

func (s *session) emitPromptDone(stopReason string, usage map[string]provider.TokenUsage, partial bool) {
	s.emit(provider.PromptDoneFrame(s.sessionFile, stopReason, usage, partial))
}

func (s *session) emit(frame map[string]any) { provider.SendFrame(s.onEventFrame, frame) }
