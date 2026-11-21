package codex

import (
	"encoding/json"
	"log"
	"strings"

	"backend/internal/provider"
)

// tokenTotals is one TokenUsageBreakdown; inputTokens includes cached input.
type tokenTotals struct {
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	CacheWriteInputTokens int64 `json:"cacheWriteInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
}

func (a tokenTotals) minus(b tokenTotals) tokenTotals {
	return tokenTotals{
		InputTokens:           a.InputTokens - b.InputTokens,
		CachedInputTokens:     a.CachedInputTokens - b.CachedInputTokens,
		CacheWriteInputTokens: a.CacheWriteInputTokens - b.CacheWriteInputTokens,
		OutputTokens:          a.OutputTokens - b.OutputTokens,
	}
}

// usage is the turn's own consumption in Grasp's disjoint buckets.
func (t *activeTurn) usage() map[string]provider.TokenUsage {
	if t.total == nil || t.baseline == nil {
		return nil
	}
	d := t.total.minus(*t.baseline)
	return map[string]provider.TokenUsage{"default": {
		InputTokens:      max(d.InputTokens-d.CachedInputTokens-d.CacheWriteInputTokens, 0),
		OutputTokens:     max(d.OutputTokens, 0),
		CacheReadTokens:  max(d.CachedInputTokens, 0),
		CacheWriteTokens: max(d.CacheWriteInputTokens, 0),
	}}
}

type notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type turnRef struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type turnError struct {
	Message string `json:"message"`
}

type threadItem struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	Command          string          `json:"command"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	Status           string          `json:"status"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           json.RawMessage `json:"result"`
	Error            *turnError      `json:"error"`
	Query            string          `json:"query"`
	Changes          []struct {
		Path string `json:"path"`
		Kind any    `json:"kind"`
	} `json:"changes"`
	Content []string `json:"content"`
	Summary []string `json:"summary"`
	// collabAgentToolCall: the agents a collab tool targets and their last
	// known status.
	ReceiverThreadIDs []string `json:"receiverThreadIds"`
	AgentsStates      map[string]struct {
		Status string `json:"status"`
	} `json:"agentsStates"`
	// subAgentActivity (multi-agent v2): a subagent thread started or ended.
	Kind          string `json:"kind"`
	AgentThreadID string `json:"agentThreadId"`
}

// currentTurn returns the active turn when ref belongs to it. A notification
// that names another turn or thread is stale and ignored.
func (s *session) currentTurn(ref turnRef) *activeTurn {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.turn
	if t == nil || (ref.ThreadID != "" && ref.ThreadID != s.threadID) {
		return nil
	}
	if ref.TurnID != "" && t.id != "" && ref.TurnID != t.id {
		return nil
	}
	if t.id == "" {
		t.id = ref.TurnID
	}
	return t
}

func (s *session) onNotification(raw json.RawMessage) error {
	var n notification
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	switch n.Method {
	case "turn/started":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(n.Params, &p)
		s.currentTurn(turnRef{ThreadID: p.ThreadID, TurnID: p.Turn.ID})
	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string     `json:"id"`
				Status string     `json:"status"`
				Error  *turnError `json:"error"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(n.Params, &p)
		if t := s.currentTurn(turnRef{ThreadID: p.ThreadID, TurnID: p.Turn.ID}); t != nil {
			end := turnEnd{status: p.Turn.Status}
			if p.Turn.Error != nil {
				end.errText = p.Turn.Error.Message
			}
			select {
			case t.done <- end:
			default:
			}
		}
	case "thread/tokenUsage/updated":
		var p struct {
			turnRef
			TokenUsage struct {
				Total tokenTotals `json:"total"`
				Last  tokenTotals `json:"last"`
			} `json:"tokenUsage"`
		}
		_ = json.Unmarshal(n.Params, &p)
		if t := s.currentTurn(p.turnRef); t != nil {
			s.mu.Lock()
			if t.baseline == nil {
				base := p.TokenUsage.Total.minus(p.TokenUsage.Last)
				t.baseline = &base
			}
			total := p.TokenUsage.Total
			t.total = &total
			s.mu.Unlock()
		}
	case "item/agentMessage/delta", "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		var p struct {
			turnRef
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		_ = json.Unmarshal(n.Params, &p)
		t := s.currentTurn(p.turnRef)
		if t == nil || p.Delta == "" {
			return nil
		}
		s.mu.Lock()
		t.streamed[p.ItemID] = true
		s.mu.Unlock()
		kind := "agent_message_chunk"
		if n.Method != "item/agentMessage/delta" {
			kind = "agent_thought_chunk"
		}
		s.emitUpdate(map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": p.Delta}})
	case "item/started", "item/completed":
		var p struct {
			turnRef
			Item threadItem `json:"item"`
		}
		_ = json.Unmarshal(n.Params, &p)
		t := s.currentTurn(p.turnRef)
		if t == nil {
			return nil
		}
		s.mu.Lock()
		streamed := t.streamed[p.Item.ID]
		s.mu.Unlock()
		if p.Item.Type == "collabAgentToolCall" || p.Item.Type == "subAgentActivity" {
			s.trackSubagents(p.Item)
		}
		if n.Method == "item/started" {
			s.itemStarted(p.Item)
		} else {
			s.itemCompleted(p.Item, streamed)
		}
	case "turn/plan/updated":
		var p struct {
			turnRef
			Plan []struct {
				Step   string `json:"step"`
				Status string `json:"status"`
			} `json:"plan"`
		}
		_ = json.Unmarshal(n.Params, &p)
		if s.currentTurn(p.turnRef) == nil {
			return nil
		}
		entries := make([]any, 0, len(p.Plan))
		for _, step := range p.Plan {
			entries = append(entries, map[string]any{"content": step.Step, "status": planStatus(step.Status), "priority": "medium"})
		}
		s.emitUpdate(map[string]any{"sessionUpdate": "plan", "entries": entries})
	case "error":
		var p struct {
			turnRef
			Error     turnError `json:"error"`
			WillRetry bool      `json:"willRetry"`
		}
		_ = json.Unmarshal(n.Params, &p)
		if p.WillRetry {
			log.Printf("codex: 可重试错误（app-server 自动重试）: %s", p.Error.Message)
			return nil
		}
		if t := s.currentTurn(p.turnRef); t != nil {
			s.mu.Lock()
			t.errText = p.Error.Message
			s.mu.Unlock()
		}
	case "warning", "configWarning", "deprecationNotice":
		// Advisory only (e.g. fallback metadata for a custom model); the turn
		// is unaffected, so it never reaches the transcript as an error.
		log.Printf("codex: %s %s", n.Method, truncate(string(n.Params), 300))
	}
	return nil
}

// trackSubagents follows the agents this thread spawned: through the collab
// tool calls that report on them, or with multi-agent v2 (the default for
// newer models) through subAgentActivity items. Whether one still runs is
// asked of the app-server when the turn ends.
func (s *session) trackSubagents(item threadItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.Type == "subAgentActivity" && item.Kind == "started" && item.AgentThreadID != "" {
		s.subagents[item.AgentThreadID] = true
	}
	if item.Tool == "spawnAgent" {
		for _, id := range item.ReceiverThreadIDs {
			s.subagents[id] = true
		}
	}
	for id, state := range item.AgentsStates {
		switch state.Status {
		case "pendingInit", "running":
			s.subagents[id] = true
		default:
			delete(s.subagents, id)
		}
	}
}

func (s *session) itemStarted(item threadItem) {
	if !isToolItem(item.Type) {
		return
	}
	update := map[string]any{"sessionUpdate": "tool_call", "toolCallId": item.ID, "title": toolTitle(item), "status": "in_progress"}
	if input := toolInput(item); input != nil {
		update["rawInput"] = input
	}
	s.emitUpdate(update)
}

func (s *session) itemCompleted(item threadItem, streamed bool) {
	switch item.Type {
	case "agentMessage":
		if !streamed && item.Text != "" {
			s.emitUpdate(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": item.Text}})
		}
	case "reasoning":
		text := strings.Join(append(append([]string(nil), item.Summary...), item.Content...), "\n")
		if !streamed && strings.TrimSpace(text) != "" {
			s.emitUpdate(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": text}})
		}
	default:
		if !isToolItem(item.Type) {
			return
		}
		update := map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": item.ID, "status": toolStatus(item.Status)}
		if text := toolOutput(item); text != "" {
			update["content"] = []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": text}}}
		}
		s.emitUpdate(update)
	}
}

func isToolItem(kind string) bool {
	switch kind {
	case "commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall", "webSearch", "collabAgentToolCall", "imageGeneration":
		return true
	}
	return false
}

func toolTitle(item threadItem) string {
	switch item.Type {
	case "commandExecution":
		if item.Command != "" {
			return item.Command
		}
	case "mcpToolCall":
		if item.Server != "" && item.Tool != "" {
			return item.Server + "/" + item.Tool
		}
	case "fileChange":
		if len(item.Changes) > 0 && item.Changes[0].Path != "" {
			return item.Changes[0].Path
		}
	case "webSearch":
		if item.Query != "" {
			return item.Query
		}
	}
	if item.Tool != "" {
		return item.Tool
	}
	return item.Type
}

func toolInput(item threadItem) json.RawMessage {
	switch {
	case item.Type == "commandExecution" && item.Command != "":
		b, _ := json.Marshal(map[string]string{"command": item.Command})
		return b
	case len(item.Arguments) > 0 && string(item.Arguments) != "null":
		return item.Arguments
	}
	return nil
}

func toolStatus(status string) string {
	switch status {
	case "failed", "declined":
		return "failed"
	}
	return "completed"
}

func toolOutput(item threadItem) string {
	if item.AggregatedOutput != nil && *item.AggregatedOutput != "" {
		return *item.AggregatedOutput
	}
	if item.Error != nil && item.Error.Message != "" {
		return item.Error.Message
	}
	if item.Type == "fileChange" {
		paths := make([]string, 0, len(item.Changes))
		for _, ch := range item.Changes {
			if ch.Path != "" {
				paths = append(paths, ch.Path)
			}
		}
		return strings.Join(paths, "\n")
	}
	if len(item.Result) > 0 && string(item.Result) != "null" {
		var r struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(item.Result, &r) == nil {
			var parts []string
			for _, c := range r.Content {
				if c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func planStatus(status string) string {
	if status == "inProgress" {
		return "in_progress"
	}
	return status
}

func (s *session) emitUpdate(update map[string]any) { s.emit(provider.UpdateFrame(s.threadID, update)) }

func (s *session) emitError(text string) { s.emit(provider.ErrorFrame(text)) }

func (s *session) emitPromptDone(stopReason string, usage map[string]provider.TokenUsage, partial bool) {
	s.emit(provider.PromptDoneFrame(s.threadID, stopReason, usage, partial))
}

func (s *session) emit(frame map[string]any) { provider.SendFrame(s.onEvent, frame) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
