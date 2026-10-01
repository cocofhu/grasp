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
// of every turn currently streaming for a run, keyed by node id.
type InflightPromptSource interface {
	InflightPrompts(runID string) map[string]InflightPrompt
}

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

// InflightPrompts implements InflightPromptSource.
func (c *acpProvider) InflightPrompts(runID string) map[string]InflightPrompt {
	prefix := runID + "|"
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	var out map[string]InflightPrompt
	for k, v := range c.inflight {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if out == nil {
			out = map[string]InflightPrompt{}
		}
		out[strings.TrimPrefix(k, prefix)] = v
	}
	return out
}

// InflightPrompts fans out to every backend that streams turns.
func (r *ProviderRegistry) InflightPrompts(runID string) map[string]InflightPrompt {
	var out map[string]InflightPrompt
	for _, p := range r.providers {
		src, ok := p.(InflightPromptSource)
		if !ok {
			continue
		}
		for node, v := range src.InflightPrompts(runID) {
			if out == nil {
				out = map[string]InflightPrompt{}
			}
			out[node] = v
		}
	}
	return out
}
