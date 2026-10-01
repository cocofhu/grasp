package runtime

import (
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"
)

// transcriptPromptMaxBytes caps a persisted prompt. Prompts carry assembled
// node context and can be large; the head is what a reader needs.
const transcriptPromptMaxBytes = 64 * 1024

// InflightPrompt is the question of a turn that is still streaming.
type InflightPrompt struct {
	Prompt     string `json:"prompt"`
	ImageCount int    `json:"imageCount,omitempty"`
	At         string `json:"at"`
}

// InflightPromptSource is an optional provider capability: report the prompt
// of every turn currently streaming for a run, and the bracketed turns that
// already finished (node StateRun events are only saved when the node returns),
// both keyed by node id.
type InflightPromptSource interface {
	InflightPrompts(runID string) map[string]InflightPrompt
	RecentTurns(runID string) map[string][]models.AcpEvent
}

// recentTurnsCap bounds the finished turns remembered per node.
const recentTurnsCap = 30

// transcriptTurnEvents wraps one finished turn's reply events with a leading
// prompt event and a trailing turn_end event (time, usage, failure).
func transcriptTurnEvents(res *sandbox.ChatResult) []models.AcpEvent {
	reply := chatResultToEvents(res)
	if res == nil || res.StartedAt.IsZero() {
		return reply
	}
	out := make([]models.AcpEvent, 0, len(reply)+2)
	out = append(out, promptEvent(res))
	out = append(out, reply...)
	return append(out, turnEndEvent(res))
}

func promptEvent(res *sandbox.ChatResult) models.AcpEvent {
	text := textutil.TruncateBytes(res.Prompt, transcriptPromptMaxBytes, "…(truncated)")
	ev := models.AcpEvent{
		Kind:      models.AcpKindPrompt,
		Text:      text,
		At:        res.StartedAt.UTC().Format(time.RFC3339Nano),
		Truncated: text != res.Prompt,
	}
	if res.ImageCount > 0 {
		ev.Title = strconv.Itoa(res.ImageCount) + " images"
	}
	return ev
}

func turnEndEvent(res *sandbox.ChatResult) models.AcpEvent {
	ended := res.EndedAt
	if ended.IsZero() {
		ended = time.Now()
	}
	ev := models.AcpEvent{
		Kind:   models.AcpKindTurnEnd,
		At:     ended.UTC().Format(time.RFC3339Nano),
		Usage:  res.Usage,
		Status: "completed",
	}
	if fail := chatFailure(res); fail != "" {
		ev.Status = "failed"
		ev.Text = fail
	} else if res.Interrupted {
		ev.Status = "failed"
		ev.Text = strings.TrimSpace(res.ErrorText)
	} else if res.StopReason != "" && res.StopReason != "end_turn" {
		ev.Text = res.StopReason
	}
	return ev
}

// transcriptTurn is one prompt … turn_end bracket of persisted events.
type transcriptTurn struct {
	prompt, end models.AcpEvent
}

// transcriptTurns parses streamed events that consist solely of bracketed
// turns. ok is false when there are no brackets or anything sits outside one,
// in which case the slice cannot be aligned with sandbox turns.
func transcriptTurns(events []models.AcpEvent) (turns []transcriptTurn, ok bool) {
	open := false
	for _, ev := range events {
		switch {
		case ev.Kind == models.AcpKindPrompt:
			if open {
				return nil, false
			}
			turns = append(turns, transcriptTurn{prompt: ev})
			open = true
		case ev.Kind == models.AcpKindTurnEnd:
			if !open {
				return nil, false
			}
			turns[len(turns)-1].end = ev
			open = false
		case !open:
			return nil, false
		}
	}
	return turns, len(turns) > 0 && !open
}

// mergeTranscriptSnapshot re-brackets the sandbox's per-turn events with the
// prompt/turn_end markers of the streamed copy. The streamed turns are the
// most recent ones in the sandbox log (earlier turns may belong to previous
// executions sharing the sandbox). Returns fallback when they cannot align.
func mergeTranscriptSnapshot(snapTurns [][]models.AcpEvent, fallback []models.AcpEvent) []models.AcpEvent {
	marks, ok := transcriptTurns(fallback)
	if !ok || len(snapTurns) < len(marks) {
		return fallback
	}
	lead := len(snapTurns) - len(marks)
	var out []models.AcpEvent
	for _, evs := range snapTurns[:lead] {
		out = append(out, evs...)
	}
	for i, m := range marks {
		out = append(out, m.prompt)
		out = append(out, snapTurns[lead+i]...)
		out = append(out, m.end)
	}
	return out
}

func inflightKey(runID, nodeID string) string { return runID + "|" + nodeID }

func (c *acpProvider) setInflightPrompt(req NodeReq, prompt string, imageCount int, at time.Time) {
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	if c.inflight == nil {
		c.inflight = map[string]InflightPrompt{}
	}
	c.inflight[inflightKey(req.RunID, req.NodeID)] = InflightPrompt{
		Prompt:     textutil.TruncateBytes(prompt, transcriptPromptMaxBytes, "…(truncated)"),
		ImageCount: imageCount,
		At:         at.UTC().Format(time.RFC3339Nano),
	}
}

func (c *acpProvider) clearInflightPrompt(req NodeReq) {
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	delete(c.inflight, inflightKey(req.RunID, req.NodeID))
}

func (c *acpProvider) recordRecentTurn(req NodeReq, turn []models.AcpEvent) {
	if len(turn) == 0 || turn[0].Kind != models.AcpKindPrompt {
		return
	}
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	if c.recentTurns == nil {
		c.recentTurns = map[string][][]models.AcpEvent{}
	}
	key := inflightKey(req.RunID, req.NodeID)
	turns := append(c.recentTurns[key], turn)
	if len(turns) > recentTurnsCap {
		turns = turns[len(turns)-recentTurnsCap:]
	}
	c.recentTurns[key] = turns
}

func (c *acpProvider) dropRunTranscript(runID string) {
	prefix := runID + "|"
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	for k := range c.recentTurns {
		if strings.HasPrefix(k, prefix) {
			delete(c.recentTurns, k)
		}
	}
	for k := range c.inflight {
		if strings.HasPrefix(k, prefix) {
			delete(c.inflight, k)
		}
	}
}

// InflightPrompts implements InflightPromptSource.
func (c *acpProvider) InflightPrompts(runID string) map[string]InflightPrompt {
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	return byNode(c.inflight, runID, func(v InflightPrompt) InflightPrompt { return v })
}

// RecentTurns implements InflightPromptSource.
func (c *acpProvider) RecentTurns(runID string) map[string][]models.AcpEvent {
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	return byNode(c.recentTurns, runID, func(turns [][]models.AcpEvent) []models.AcpEvent {
		var flat []models.AcpEvent
		for _, t := range turns {
			flat = append(flat, t...)
		}
		return flat
	})
}

func byNode[V, O any](m map[string]V, runID string, conv func(V) O) map[string]O {
	prefix := runID + "|"
	var out map[string]O
	for k, v := range m {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if out == nil {
			out = map[string]O{}
		}
		out[strings.TrimPrefix(k, prefix)] = conv(v)
	}
	return out
}

// InflightPrompts fans out to every backend that streams turns.
func (r *ProviderRegistry) InflightPrompts(runID string) map[string]InflightPrompt {
	return fanOut(r, func(src InflightPromptSource) map[string]InflightPrompt { return src.InflightPrompts(runID) })
}

// RecentTurns fans out to every backend that streams turns.
func (r *ProviderRegistry) RecentTurns(runID string) map[string][]models.AcpEvent {
	return fanOut(r, func(src InflightPromptSource) map[string][]models.AcpEvent { return src.RecentTurns(runID) })
}

func fanOut[V any](r *ProviderRegistry, get func(InflightPromptSource) map[string]V) map[string]V {
	var out map[string]V
	for _, p := range r.providers {
		src, ok := p.(InflightPromptSource)
		if !ok {
			continue
		}
		for node, v := range get(src) {
			if out == nil {
				out = map[string]V{}
			}
			out[node] = v
		}
	}
	return out
}
