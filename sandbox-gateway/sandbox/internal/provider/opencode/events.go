package opencode

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"backend/internal/provider"
)

const abortedError = "MessageAbortedError"

type event struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

type errorInfo struct {
	Name string `json:"name"`
	Data struct {
		Message string `json:"message"`
	} `json:"data"`
}

func (e *errorInfo) text() string {
	if e == nil {
		return ""
	}
	if e.Data.Message != "" {
		return e.Data.Message
	}
	return e.Name
}

type messageInfo struct {
	ID       string     `json:"id"`
	Role     string     `json:"role"`
	ParentID string     `json:"parentID"`
	Error    *errorInfo `json:"error"`
	Time     struct {
		Created int64 `json:"created"`
	} `json:"time"`
}

type tokens struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

type toolState struct {
	Status string          `json:"status"`
	Input  json.RawMessage `json:"input"`
	Output any             `json:"output"`
	Error  any             `json:"error"`
}

type part struct {
	ID        string     `json:"id"`
	MessageID string     `json:"messageID"`
	Type      string     `json:"type"`
	Text      string     `json:"text"`
	Tool      string     `json:"tool"`
	CallID    string     `json:"callID"`
	State     *toolState `json:"state"`
	Tokens    *tokens    `json:"tokens"`
}

// partState is what was already forwarded for one part.
type partState struct {
	kind    string // agent_message_chunk / agent_thought_chunk
	emitted string
	started bool // tool parts: the tool_call was emitted
}

// onEvent handles one event of the server's stream. Only this session's
// events shape the transcript; a child session (subagent) shows through the
// parent's tool call.
func (s *session) onEvent(raw json.RawMessage) {
	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil {
		log.Printf("opencode: 事件无法解析: %v", err)
		return
	}
	switch ev.Type {
	case "message.updated":
		var p struct {
			SessionID string      `json:"sessionID"`
			Info      messageInfo `json:"info"`
		}
		if json.Unmarshal(ev.Properties, &p) == nil && p.SessionID == s.id {
			s.messageUpdated(p.Info)
		}
	case "message.part.updated":
		var p struct {
			SessionID string `json:"sessionID"`
			Part      part   `json:"part"`
		}
		if json.Unmarshal(ev.Properties, &p) == nil && p.SessionID == s.id {
			s.partUpdated(p.Part)
		}
	case "message.part.delta":
		var p struct {
			SessionID string `json:"sessionID"`
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
			Field     string `json:"field"`
			Delta     string `json:"delta"`
		}
		if json.Unmarshal(ev.Properties, &p) == nil && p.SessionID == s.id && p.Field == "text" {
			s.partDelta(p.MessageID, p.PartID, p.Delta)
		}
	case "session.status":
		var p struct {
			SessionID string `json:"sessionID"`
			Status    struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"status"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || p.SessionID != s.id {
			return
		}
		switch p.Status.Type {
		case "busy":
			s.mu.Lock()
			if t := s.turn; t != nil {
				t.busy = true
			}
			s.mu.Unlock()
		case "retry":
			log.Printf("opencode: 模型请求重试中: %s", p.Status.Message)
		case "idle":
			s.idle()
		}
	case "session.idle":
		var p struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(ev.Properties, &p) == nil && p.SessionID == s.id {
			s.idle()
		}
	case "session.error":
		var p struct {
			SessionID string     `json:"sessionID"`
			Error     *errorInfo `json:"error"`
		}
		if json.Unmarshal(ev.Properties, &p) == nil && (p.SessionID == s.id || p.SessionID == "") {
			s.turnError(p.Error)
		}
	case "permission.asked":
		s.answer(ev.Properties, "/permission/%s/reply", map[string]any{"reply": "always"})
	case "question.asked":
		s.answer(ev.Properties, "/question/%s/reject", nil)
	}
}

func (s *session) messageUpdated(info messageInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[info.ID] = info.Role
	t := s.turn
	if t == nil {
		return
	}
	switch info.Role {
	case "user":
		// The server also re-publishes earlier messages; only the ones
		// posted into this turn must be answered before it ends.
		if info.Time.Created >= t.since && !t.users[info.ID] {
			t.users[info.ID] = true
			t.userOrder = append(t.userOrder, info.ID)
		}
	case "assistant":
		if info.ParentID != "" {
			t.answered[info.ParentID] = true
		}
		if info.Error != nil && info.Error.Name != abortedError {
			t.errText = info.Error.text()
		}
	}
}

func (s *session) partUpdated(p part) {
	s.mu.Lock()
	if s.turn == nil || s.roles[p.MessageID] == "user" {
		s.mu.Unlock()
		return
	}
	st := s.parts[p.ID]
	if st == nil {
		st = &partState{}
		s.parts[p.ID] = st
	}
	t := s.turn
	if p.Type == "step-finish" && p.Tokens != nil {
		t.usage.InputTokens += p.Tokens.Input
		t.usage.OutputTokens += p.Tokens.Output + p.Tokens.Reasoning
		t.usage.CacheReadTokens += p.Tokens.Cache.Read
		t.usage.CacheWriteTokens += p.Tokens.Cache.Write
		t.reported = true
	}
	s.mu.Unlock()

	switch p.Type {
	case "text":
		s.streamText(st, "agent_message_chunk", p.Text)
	case "reasoning":
		s.streamText(st, "agent_thought_chunk", p.Text)
	case "tool":
		s.toolUpdated(st, p)
	}
}

func (s *session) partDelta(messageID, partID, delta string) {
	s.mu.Lock()
	st := s.parts[partID]
	if s.turn == nil || s.roles[messageID] == "user" || st == nil || st.kind == "" || delta == "" {
		s.mu.Unlock()
		return
	}
	st.emitted += delta
	s.mu.Unlock()
	s.emitChunk(st.kind, delta)
}

// streamText forwards the part's text not yet sent as deltas.
func (s *session) streamText(st *partState, kind, text string) {
	s.mu.Lock()
	st.kind = kind
	var suffix string
	if strings.HasPrefix(text, st.emitted) {
		suffix = text[len(st.emitted):]
		st.emitted = text
	}
	s.mu.Unlock()
	if suffix != "" {
		s.emitChunk(kind, suffix)
	}
}

func (s *session) toolUpdated(st *partState, p part) {
	if p.State == nil || p.State.Status == "pending" {
		return
	}
	s.mu.Lock()
	first := !st.started
	st.started = true
	s.openTools[p.CallID] = true
	if p.State.Status == "completed" || p.State.Status == "error" {
		delete(s.openTools, p.CallID)
	}
	s.mu.Unlock()
	if first {
		update := map[string]any{"sessionUpdate": "tool_call", "toolCallId": p.CallID, "title": p.Tool, "status": "in_progress"}
		if len(p.State.Input) > 0 && string(p.State.Input) != "{}" {
			update["rawInput"] = p.State.Input
		}
		s.emitUpdate(update)
	}
	var status, text string
	switch p.State.Status {
	case "completed":
		status, text = "completed", renderOutput(p.State.Output)
	case "error":
		status, text = "failed", renderOutput(p.State.Error)
	default:
		return
	}
	update := map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": p.CallID, "status": status}
	if text != "" {
		update["content"] = []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": text}}}
	}
	s.emitUpdate(update)
}

func renderOutput(v any) string {
	switch out := v.(type) {
	case nil:
		return ""
	case string:
		return out
	default:
		b, _ := json.Marshal(out)
		return string(b)
	}
}

func (s *session) turnError(e *errorInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.turn
	if t == nil || e == nil {
		return
	}
	if e.Name == abortedError {
		t.aborted = true
		return
	}
	t.errText = e.text()
}

// idle settles the turn once every prompt posted into it was answered; a
// steer that has not been picked up yet keeps it open for the next run.
func (s *session) idle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.turn; t != nil {
		t.idleSeen = true
		s.settleLocked(t)
	}
}

func (s *session) settleLocked(t *activeTurn) {
	if t.settled || !t.idleSeen {
		return
	}
	if !t.aborted && t.errText == "" {
		if !t.busy || len(t.userOrder) < t.posted {
			return
		}
		for _, id := range t.userOrder {
			if !t.answered[id] {
				return
			}
		}
	}
	t.settled = true
	close(t.done)
}

// answer replies to a permission or question request of this session or one
// of its subagents. Sessions run with every permission allowed and the
// question tool denied, so these are a fallback that keeps a turn from
// waiting on a prompt no one answers.
func (s *session) answer(props json.RawMessage, pathFormat string, body any) {
	var p struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(props, &p) != nil || p.ID == "" {
		return
	}
	go func() {
		if err := s.srv.call(context.Background(), http.MethodPost, sprintfPath(pathFormat, p.ID), body, nil); err != nil {
			log.Printf("opencode: 应答请求 %s 失败: %v", p.ID, err)
		}
	}()
}

func (s *session) emitChunk(kind, text string) {
	s.emitUpdate(map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": text}})
}

func (s *session) emitUpdate(update map[string]any) {
	s.emit(map[string]any{"type": "session_update", "sessionId": s.id, "update": update})
}

func (s *session) emitError(text string) {
	s.emit(map[string]any{"op": "raw", "type": "error_text", "text": text})
}

func (s *session) emitPromptDone(stopReason string, usage map[string]provider.TokenUsage, partial bool) {
	frame := map[string]any{"type": "prompt_done", "sessionId": s.id, "stopReason": stopReason}
	if len(usage) > 0 {
		frame["usage"] = usage
		if partial {
			frame["usagePartial"] = true
		}
	}
	s.emit(frame)
}

func (s *session) emit(ev map[string]any) {
	if s.onEventFrame == nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		log.Printf("opencode: 事件序列化失败: %v", err)
		return
	}
	s.onEventFrame(b)
}
