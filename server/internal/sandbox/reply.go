package sandbox

import "encoding/json"

// Reply folds a turn's frames, as the bridge sent them, into what the turn
// showed. It reads frames the same way the turn itself was aggregated, so a
// caller holding raw frames never parses them again.
type Reply struct {
	r ChatResult
}

// ReplyTool is one tool call of a reply, with its output as the transcript
// shows it (text only, redacted and truncated).
type ReplyTool struct {
	ID, Title, Status, Output string
}

// Add folds one frame, wrapped or bare.
func (p *Reply) Add(raw json.RawMessage) {
	dispatchFrame(raw, &p.r)
}

// Text is the visible reply, across same-turn continuations.
func (p *Reply) Text() string {
	return p.r.VisibleText()
}

// Steers is the input the turn took mid-flight, in order.
func (p *Reply) Steers() []string {
	return append([]string(nil), p.r.Steers...)
}

// Tools lists the tool calls in the order they started.
func (p *Reply) Tools() []ReplyTool {
	out := make([]ReplyTool, len(p.r.ToolCalls))
	for i, tc := range p.r.ToolCalls {
		out[i] = ReplyTool{ID: tc.ID, Title: tc.Title, Status: tc.Status, Output: toolDetail(tc.RawOutput, true)}
	}
	return out
}
