package sandbox

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/textutil"
)

// ChatStep is one entry of the open row's timeline: a run of thought or
// narration text, or a pointer into ToolCalls.
type ChatStep struct {
	Kind string // thought|message|tool
	Text string
	Tool int
}

const (
	// MaxTimelineSteps caps the steps one row reports; later steps are dropped.
	MaxTimelineSteps = 120
	// maxStepText is the former per-step prose cap. Thought and message text
	// are no longer cut to it; tests still use it as the size that used to truncate.
	maxStepText = 8000
	// maxTimelineText budgets tool details. Prose is counted so later tool
	// input/output can be dropped, but thought and message text are not cut.
	maxTimelineText = 64 << 10
	maxToolSummary   = 160
	maxToolDetail    = 2000
)

// addText appends a thought/message chunk, extending the last step when it is
// of the same kind.
func (r *ChatResult) addText(kind, text string) {
	if n := len(r.Timeline); n > 0 && r.Timeline[n-1].Kind == kind {
		r.Timeline[n-1].Text += text
		return
	}
	r.Timeline = append(r.Timeline, ChatStep{Kind: kind, Text: text})
}

func (r *ChatResult) addTool(idx int) {
	r.Timeline = append(r.Timeline, ChatStep{Kind: "tool", Tool: idx})
}

// timelineEvent renders the open row's steps (nil when there are none).
func (r *ChatResult) timelineEvent(t int) *models.AcpEvent {
	if len(r.Timeline) == 0 {
		return nil
	}
	parts := make([]models.AcpPart, 0, min(len(r.Timeline), MaxTimelineSteps))
	budget := maxTimelineText
	for _, st := range r.Timeline {
		if len(parts) == MaxTimelineSteps {
			break
		}
		if st.Kind == "tool" {
			if st.Tool < 0 || st.Tool >= len(r.ToolCalls) {
				continue
			}
			p := toolPart(r.ToolCalls[st.Tool])
			budget -= len(p.Summary) + len(p.Input) + len(p.Output)
			if budget < 0 {
				p.Input, p.Output = "", ""
			}
			parts = append(parts, p)
			continue
		}
		if strings.TrimSpace(st.Text) == "" {
			continue
		}
		// Chat prose is the text the ReAct bubble shows. Deliver the accumulated
		// original even when it exceeds the old 8000-byte step cap or the
		// remaining 64KiB budget. Counting it still lets later tool details drop.
		text := st.Text
		budget -= len(text)
		parts = append(parts, models.AcpPart{Kind: st.Kind, Text: text})
	}
	if len(parts) == 0 {
		return nil
	}
	return &models.AcpEvent{T: t, Kind: models.AcpKindTimeline, Parts: parts}
}

func toolPart(tc ACPToolCall) models.AcpPart {
	title := tc.Title
	if title == "" {
		title = tc.ID
	}
	return models.AcpPart{
		Kind:    "tool",
		Title:   title,
		Status:  tc.Status,
		Summary: ToolSummary(tc.RawInput),
		Input:   toolDetail(tc.RawInput, false),
		Output:  toolDetail(tc.RawOutput, true),
	}
}

// summaryKeys are the argument names that best describe a call, in order.
var summaryKeys = []string{
	"command", "cmd", "file_path", "filePath", "path", "target_file", "targetFile",
	"url", "uri", "pattern", "glob_pattern", "query", "search_term", "name", "description",
}

// ToolSummary is a redacted one-line description of a tool call's input (the
// command, path, URL or query it ran on), or "" when none is recognisable.
func ToolSummary(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, k := range summaryKeys {
		if s := summaryValue(m[k]); strings.TrimSpace(s) != "" {
			return oneLine(s, maxToolSummary)
		}
	}
	return ""
}

func summaryValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var parts []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func oneLine(s string, maxRunes int) string {
	s = textutil.RedactSecrets(strings.Join(strings.Fields(s), " "))
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes-1]) + "…"
}

// toolDetail renders a tool input (pretty JSON) or output (its text content
// when it has one), redacted and truncated for the chat.
func toolDetail(raw json.RawMessage, output bool) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	var s string
	if output {
		s = outputText(v)
	}
	if s == "" {
		buf, err := json.MarshalIndent(maskKeys(v, 0), "", "  ")
		if err != nil {
			return ""
		}
		s = string(buf)
	}
	s = strings.TrimSpace(textutil.RedactSecrets(s))
	if s == "" || s == "{}" || s == "[]" || s == "null" || s == `""` {
		return ""
	}
	return textutil.TruncateBytes(s, maxToolDetail, "\n…(truncated)")
}

// outputText pulls readable text out of common tool result shapes
// ({stdout,stderr}, {output}, {content:[{text}]}, plain strings).
func outputText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		return extractContentText(t)
	case map[string]any:
		if s, ok := t["stdout"].(string); ok && strings.TrimSpace(s) != "" {
			if e, ok := t["stderr"].(string); ok && strings.TrimSpace(e) != "" {
				return s + "\n" + e
			}
			return s
		}
		for _, k := range []string{"output", "text", "result", "stderr"} {
			if s, ok := t[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
		if c, ok := t["content"]; ok {
			return extractContentText(c)
		}
	}
	return ""
}

var secretKeyHints = []string{"password", "passwd", "secret", "token", "apikey", "api_key", "access_key", "private_key", "credential", "authorization", "cookie"}

func secretKey(k string) bool {
	lk := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
	for _, h := range secretKeyHints {
		if strings.Contains(lk, h) {
			return true
		}
	}
	return false
}

func maskKeys(v any, depth int) any {
	if depth > 8 {
		return "…"
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if secretKey(k) {
				out[k] = textutil.SecretMask
			} else {
				out[k] = maskKeys(x, depth+1)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = maskKeys(x, depth+1)
		}
		return out
	}
	return v
}
