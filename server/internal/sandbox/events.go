package sandbox

import (
	"encoding/json"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/textutil"
)

// ChatResult is the structured aggregation of one prompt turn's
// session_update events from cursor-agent (via the cursor-acp bridge).
//
//   - Narration: concatenated agent_message_chunk text (the visible reply)
//   - Thought:   concatenated agent_thought_chunk text (internal reasoning)
//   - Plan:      latest plan session_update (or extracted from a plan tool)
//   - ToolCalls: aggregated tool calls (merged by toolCallId)
//   - Commands:  latest available_commands_update list
//   - RawEvents: raw event passthrough for audit/snapshot
type ChatResult struct {
	Narration string            `json:"narration,omitempty"`
	Thought   string            `json:"thought,omitempty"`
	Plan      *ACPPlan          `json:"plan,omitempty"`
	ToolCalls []ACPToolCall     `json:"tool_calls,omitempty"`
	Commands  []ACPCommand      `json:"commands,omitempty"`
	RawEvents []json.RawMessage `json:"-"`

	// Usage is the turn's token accounting from prompt_done.usage (components
	// summed across model buckets). nil = capabilities omitted usage / not
	// reported; non-nil (incl. all zeros) = explicitly reported for this turn.
	Usage *models.TokenUsage `json:"usage,omitempty"`
	// UsageByModel is the per-model breakdown after weak-key merge / optional
	// ACP_BRIDGE_MODEL backfill. Non-nil whenever Usage is non-nil.
	UsageByModel models.TokenUsageByModel `json:"usageByModel,omitempty"`

	// Busy carries the latest authoritative queue_state.busy flag from the
	// cursor-acp bridge (true while a session/prompt is in flight). BusySet
	// reports whether a queue_state frame has been observed at all, so callers
	// can distinguish "not yet reported" from an explicit false. Neither is
	// persisted to snapshots; they only drive the live running/idle indicator.
	Busy    bool `json:"-"`
	BusySet bool `json:"-"`

	// ErrorText collects provider/CLI error bodies from {op:event,data.type:
	// "error_text"} frames (quota exhaustion, HTTP 4xx/5xx). Non-empty means
	// the turn failed even when the bridge still emitted prompt_done.
	ErrorText string `json:"errorText,omitempty"`
	// Failed is set when prompt_done.stopReason == "failed".
	Failed bool `json:"-"`

	// OpID is the turn id this client sent with {op:chat}; the bridge echoes it
	// on every frame of the turn.
	OpID string `json:"-"`
	// StopReason is prompt_done.stopReason (empty when the turn never finished).
	StopReason string `json:"-"`
	// Interrupted marks a turn that did not finish on its own: the bridge
	// watchdog timed it out, it was cancelled, or this client gave up after its
	// idle window. Narration is partial; ErrorText carries the reason.
	Interrupted bool `json:"-"`
}

// appendErrorText records a provider/bridge error body on the turn. Multiple
// fragments are joined with newlines so a later error_text frame does not
// erase an earlier op:"error" message (or vice versa).
func (r *ChatResult) appendErrorText(s string) {
	if r == nil {
		return
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if r.ErrorText == "" {
		r.ErrorText = s
		return
	}
	r.ErrorText += "\n" + s
}

type ACPPlan struct {
	Entries []ACPPlanEntry `json:"entries"`
}

type ACPPlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
}

type ACPToolCall struct {
	ID        string          `json:"id,omitempty"`
	Title     string          `json:"title,omitempty"`
	Status    string          `json:"status,omitempty"`
	RawInput  json.RawMessage `json:"raw_input,omitempty"`
	RawOutput json.RawMessage `json:"raw_output,omitempty"`
}

type ACPCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

var planToolNames = map[string]bool{
	"plan":                true,
	"create_plan":         true,
	"cursor_plan":         true,
	"update_todos":        true,
	"cursor_create_plan":  true,
	"cursor_update_todos": true,
}

// normalizeSessionUpdate normalises cursor-agent version differences into
// snake_case kind + a flattened map (nested sessionUpdate object lifted up).
func normalizeSessionUpdate(update json.RawMessage) (kind string, flat map[string]any) {
	if len(update) == 0 {
		return "", nil
	}
	var raw map[string]any
	if err := json.Unmarshal(update, &raw); err != nil {
		return "", nil
	}
	flat = mergeNested(raw)
	kind = normalizeKindString(extractKindString(flat))
	return kind, flat
}

func mergeNested(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	su, ok := out["sessionUpdate"]
	if !ok {
		su = out["session_update"]
	}
	if obj, ok := su.(map[string]any); ok {
		for k, v := range obj {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
		delete(out, "sessionUpdate")
		delete(out, "session_update")
	}
	return out
}

func extractKindString(flat map[string]any) string {
	for _, k := range []string{"sessionUpdate", "session_update", "type", "kind"} {
		if v, ok := flat[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeKindString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				prev := rune(s[i-1])
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r + ('a' - 'A'))
		case r == '-':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isToolKind(k string) bool {
	switch k {
	case "tool_call", "tool_call_update", "toolcall", "toolcall_update",
		"mcp_tool_call", "mcp_tool_call_update":
		return true
	}
	return strings.Contains(k, "tool_call") || strings.Contains(k, "toolcall")
}

// AcpEvents flattens an aggregated ChatResult into the ordered AcpEvent
// timeline (thought → plan → tool calls → narration) the run/sandbox UIs
// render. Single conversion shared by the live turn, the sandbox event-log
// reader, and the interactive console — there is one source of truth for the
// event log: the sandbox itself.
func (r *ChatResult) AcpEvents() []models.AcpEvent {
	if r == nil {
		return nil
	}
	var ev []models.AcpEvent
	t := 0
	if r.Thought != "" {
		// ReAct / timeline / hard-refresh seed all consume this event: keep the
		// full Thought so UIs never see …(truncated). Message narration still
		// truncates below (8000 bytes).
		ev = append(ev, models.AcpEvent{T: t, Kind: "thought", Text: r.Thought})
		t++
	}
	if r.Plan != nil && len(r.Plan.Entries) > 0 {
		var lines []string
		for _, e := range r.Plan.Entries {
			lines = append(lines, "- ["+e.Status+"] "+e.Content)
		}
		ev = append(ev, models.AcpEvent{T: t, Kind: "plan", Text: strings.Join(lines, "\n")})
		t++
	}
	for _, tc := range r.ToolCalls {
		title := tc.Title
		if title == "" {
			title = tc.ID
		}
		ev = append(ev, models.AcpEvent{T: t, Kind: "tool_call", Title: title, Status: tc.Status})
		t++
	}
	if r.Narration != "" {
		ev = append(ev, models.AcpEvent{T: t, Kind: "message", Text: textutil.TruncateBytes(r.Narration, 8000, "…(truncated)")})
	}
	return ev
}

func dispatchSessionUpdate(kind string, flat map[string]any, result *ChatResult) {
	if result == nil || flat == nil {
		return
	}
	switch {
	case kind == "agent_message_chunk":
		if t := extractContentText(flat["content"]); t != "" {
			result.Narration += t
		}
	case kind == "agent_thought_chunk":
		if t := extractContentText(flat["content"]); t != "" {
			result.Thought += t
		}
	case kind == "plan":
		if entries := extractPlanEntries(flat); len(entries) > 0 {
			result.Plan = &ACPPlan{Entries: entries}
		}
	case kind == "available_commands_update":
		if cmds := extractCommands(flat); len(cmds) > 0 {
			result.Commands = cmds
		}
	case kind == "current_mode_update", kind == "session_info_update":
		// silent meta
	case isToolKind(kind):
		applyToolCall(flat, result)
	}
}

func applyToolCall(flat map[string]any, result *ChatResult) {
	id := stringField(flat, "toolCallId", "tool_call_id", "id", "callId")
	title := stringField(flat, "title", "name", "toolName", "tool_name")
	status := stringField(flat, "status", "state")
	rawIn := rawField(flat, "rawInput", "raw_input", "input", "arguments", "args")
	rawOut := rawField(flat, "rawOutput", "raw_output", "output", "result", "content")

	normTitle := normalizeKindString(strings.ReplaceAll(title, " ", "_"))
	if planToolNames[normTitle] {
		if entries := extractPlanEntriesFromRaw(rawIn); len(entries) > 0 {
			result.Plan = &ACPPlan{Entries: entries}
		}
		return
	}

	for i := range result.ToolCalls {
		tc := &result.ToolCalls[i]
		if id != "" && tc.ID == id {
			if title != "" {
				tc.Title = title
			}
			if status != "" {
				tc.Status = status
			}
			if len(rawIn) > 0 {
				tc.RawInput = rawIn
			}
			if len(rawOut) > 0 {
				tc.RawOutput = rawOut
			}
			return
		}
	}
	result.ToolCalls = append(result.ToolCalls, ACPToolCall{
		ID:        id,
		Title:     title,
		Status:    status,
		RawInput:  rawIn,
		RawOutput: rawOut,
	})
}

func extractContentText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["text"].(string); ok {
			return s
		}
		if parts, ok := m["parts"].([]any); ok {
			var b strings.Builder
			for _, p := range parts {
				b.WriteString(extractContentText(p))
			}
			return b.String()
		}
	}
	if arr, ok := v.([]any); ok {
		var b strings.Builder
		for _, p := range arr {
			b.WriteString(extractContentText(p))
		}
		return b.String()
	}
	return ""
}

func extractPlanEntries(flat map[string]any) []ACPPlanEntry {
	raw, ok := flat["entries"]
	if !ok {
		raw = flat["steps"]
	}
	return convertPlanEntries(raw)
}

func extractPlanEntriesFromRaw(rawInput json.RawMessage) []ACPPlanEntry {
	if len(rawInput) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(rawInput, &m); err != nil {
		return nil
	}
	raw, ok := m["entries"]
	if !ok {
		raw = m["todos"]
	}
	if raw == nil {
		raw = m["steps"]
	}
	return convertPlanEntries(raw)
}

func convertPlanEntries(raw any) []ACPPlanEntry {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ACPPlanEntry, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry := ACPPlanEntry{
			Content:  stringField(m, "content", "title", "text", "description"),
			Status:   normalizeStatus(stringField(m, "status", "state")),
			Priority: stringField(m, "priority"),
		}
		if entry.Content == "" {
			continue
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return "pending"
	case "in-progress", "running", "executing":
		return "in_progress"
	case "complete", "success", "done":
		return "completed"
	case "canceled":
		return "cancelled"
	}
	return s
}

func extractCommands(flat map[string]any) []ACPCommand {
	raw, ok := flat["availableCommands"]
	if !ok {
		raw = flat["commands"]
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ACPCommand, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		c := ACPCommand{
			Name:        stringField(m, "name", "command"),
			Description: stringField(m, "description", "summary"),
		}
		if c.Name == "" {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func rawField(m map[string]any, keys ...string) json.RawMessage {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		buf, err := json.Marshal(v)
		if err != nil || len(buf) == 0 || string(buf) == "null" {
			continue
		}
		return json.RawMessage(buf)
	}
	return nil
}
