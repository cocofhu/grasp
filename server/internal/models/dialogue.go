package models

import (
	"regexp"
	"strings"
)

// Reply outcomes: how one dialogue turn ended. Agent rows carry one; the UI
// shows the empty / failure cards only from these values.
const (
	OutcomeOK          = "ok"
	OutcomeEmpty       = "empty"
	OutcomeFailed      = "failed"
	OutcomeInterrupted = "interrupted"
	OutcomeDropped     = "dropped"
)

// failureBanner matches the reply text the platform writes for a failed turn:
// the named banners, or any whole-line "(…失败…)".
var failureBanner = regexp.MustCompile(`澄清开场失败|澄清回复失败|澄清会话已失效|复审修改失败|^\([^)]*失败[^)]*\)$`)

// ReplyOutcome is the single rule for how an agent row ended. Interrupted and
// a stored failed/dropped outcome (which the text alone cannot show) win;
// everything else, including rows written before outcomes existed, follows
// the content. Human rows have no outcome.
func ReplyOutcome(m ReactMessage) string {
	if m.Role != "agent" {
		return ""
	}
	if m.Interrupted {
		return OutcomeInterrupted
	}
	switch m.Outcome {
	case OutcomeFailed, OutcomeDropped, OutcomeInterrupted:
		return m.Outcome
	}
	if m.Handoff || len(m.Questions) > 0 || len(m.Forms) > 0 {
		return OutcomeOK
	}
	t := strings.TrimSpace(m.Text)
	if t == "" {
		return OutcomeEmpty
	}
	if failureBanner.MatchString(t) {
		return OutcomeFailed
	}
	return OutcomeOK
}

// WithReplyOutcomes returns msgs with every agent row's Outcome set by
// ReplyOutcome, so clients never infer it from the text. msgs is not changed.
func WithReplyOutcomes(msgs []ReactMessage) []ReactMessage {
	out := make([]ReactMessage, len(msgs))
	for i, m := range msgs {
		m.Outcome = ReplyOutcome(m)
		out[i] = m
	}
	return out
}

// RetryableReply reports an agent row the user may cover with "retry last".
func RetryableReply(m ReactMessage) bool {
	switch ReplyOutcome(m) {
	case OutcomeEmpty, OutcomeFailed:
		return true
	}
	return false
}

// ReplyBody is the tool and step projection of one reply.
type ReplyBody struct {
	Tools []ReactTool
	Parts []AcpPart
}

// ReplyRow projects a reply's events and final text into its tools and steps.
// Persisting a reply and drawing it while it streams both use it, so a row
// does not change shape when it is sealed.
func ReplyRow(events []AcpEvent, text string) ReplyBody {
	return ReplyBody{Tools: ToolsFromEvents(events), Parts: PartsForReply(events, text)}
}

// LiveReply is one agent row of an in-flight reply.
type LiveReply struct {
	Text    string
	Thought string
	Tools   []ReactTool
	Parts   []AcpPart
	// Handoff marks an earlier segment of the same user message.
	Handoff bool
}

// isChatFailureEcho is the runtime's own error line, never reply text.
func isChatFailureEcho(text string) bool {
	return strings.HasPrefix(text, "react reply chat failed") || strings.HasPrefix(text, "review revise chat failed")
}

// LiveReplyRows projects an in-flight event snapshot into agent rows. A
// snapshot with segment markers is one user message answered by several rows:
// every row but the last is a sealed handoff segment; the last is the open row
// (empty until its output starts) and carries the tools and steps, as the
// persisted reply will. Always returns at least one row.
func LiveReplyRows(events []AcpEvent) []LiveReply {
	type seg struct{ thought, message string }
	segs := []seg{{}}
	sawMessage := false
	for _, ev := range eventsForReply(events) {
		switch {
		case ev.Kind == "segment":
			segs = append(segs, seg{})
			sawMessage = false
		case ev.Kind == "thought" && ev.Text != "":
			if sawMessage {
				segs = append(segs, seg{thought: ev.Text})
				sawMessage = false
			} else {
				segs[len(segs)-1].thought = ev.Text
			}
		case ev.Kind == "message" && ev.Text != "":
			if isChatFailureEcho(ev.Text) {
				continue
			}
			if sawMessage {
				segs = append(segs, seg{message: ev.Text})
			} else {
				segs[len(segs)-1].message = ev.Text
			}
			sawMessage = true
		}
	}
	out := make([]LiveReply, 0, len(segs))
	for i, s := range segs {
		row := LiveReply{Text: s.message, Thought: s.thought}
		if i == len(segs)-1 {
			body := ReplyRow(events, s.message)
			row.Tools, row.Parts = body.Tools, body.Parts
		} else {
			row.Handoff = true
		}
		out = append(out, row)
	}
	return out
}
