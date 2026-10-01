package handlers

import (
	"net/http"
	"sort"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/gin-gonic/gin"
)

// promptPreviewRunes bounds prompt text on payloads that are refetched often
// (run detail, node events). The full text is served by RunLlmTranscript.
const promptPreviewRunes = 300

// previewPromptEvents returns events with every prompt Text cut to a short
// preview. The input slice is never mutated.
func previewPromptEvents(events []models.AcpEvent) []models.AcpEvent {
	var out []models.AcpEvent
	for i, ev := range events {
		if ev.Kind != models.AcpKindPrompt {
			continue
		}
		r := []rune(ev.Text)
		if len(r) <= promptPreviewRunes {
			continue
		}
		if out == nil {
			out = append([]models.AcpEvent(nil), events...)
		}
		out[i].Text = string(r[:promptPreviewRunes])
		out[i].Truncated = true
	}
	if out == nil {
		return events
	}
	return out
}

// RunLlmTranscript returns every node execution of a run (oldest first) with
// its full event log — prompts included — plus the prompts of turns still
// streaming, for the run detail "LLM 过程" view.
func (h *Handlers) RunLlmTranscript(c *gin.Context) {
	runID := c.Param("id")
	if _, ok := h.Runs.Get(runID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	states := h.Runs.States(runID)
	sort.SliceStable(states, func(i, j int) bool {
		a, b := states[i].StartedAt, states[j].StartedAt
		if a == nil || b == nil || a.Equal(*b) {
			return states[i].ID < states[j].ID
		}
		return a.Before(*b)
	})
	execs := make([]gin.H, 0, len(states))
	for _, s := range states {
		ex := gin.H{
			"id": s.ID, "nodeId": s.NodeID, "nodeType": s.NodeType, "iteration": s.Iteration,
			"status": s.Status, "durationSec": s.DurationSec, "events": s.Events, "mcpCalls": s.McpCalls,
		}
		if s.StartedAt != nil {
			ex["startedAt"] = *s.StartedAt
		}
		if s.Usage != nil {
			ex["usage"] = s.Usage
		}
		if len(s.UsageByModel) > 0 {
			ex["usageByModel"] = s.UsageByModel
		}
		if s.Error != "" {
			ex["error"] = s.Error
		}
		execs = append(execs, ex)
	}
	out := gin.H{"executions": execs}
	if h.Eng != nil {
		if inflight := h.Eng.InflightPrompts(runID); len(inflight) > 0 {
			out["inflight"] = inflight
		}
	}
	c.JSON(http.StatusOK, out)
}
