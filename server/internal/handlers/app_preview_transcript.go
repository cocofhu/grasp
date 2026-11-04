package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

var (
	errPreviewMainUnavailable        = errors.New("main conversation unavailable")
	errPreviewMainServiceUnavailable = errors.New("main conversation service unavailable")
)

func writePreviewMainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errPreviewMainUnavailable):
		c.JSON(http.StatusConflict, gin.H{"error": "main conversation unavailable"})
	case errors.Is(err, errPreviewMainServiceUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
	default:
		writeSoloErr(c, err)
	}
}

type previewMainHistory struct {
	Rows  []sharedPreviewRow `json:"rows"`
	Busy  bool               `json:"busy"`
	Model any                `json:"model,omitempty"`
}

// appPreviewMainHistory is shared by live pushes and reconnect/poll recovery.
// Raw ACP frames and tool arguments/outputs never cross the public boundary.
func (h *Handlers) appPreviewMainHistory(link *models.GateShareLink, sb models.Sandbox) (*previewMainHistory, error) {
	out := &previewMainHistory{Rows: []sharedPreviewRow{}}
	if sb.Purpose == services.SandboxPurposeSolo {
		if h.Solo == nil {
			return nil, errPreviewMainServiceUnavailable
		}
		messages, busy, err := h.Solo.PreviewTranscript(sb.ThreadID, link.CreatedBy)
		if err != nil {
			return nil, err
		}
		out.Busy = busy
		for _, message := range messages {
			out.Rows = append(out.Rows, previewSoloRow(message))
		}
	} else {
		state, active := h.workflowPreviewDialogue(link.RunID, link.NodeID)
		if !active || state.Iteration != link.Iteration {
			return nil, errPreviewMainUnavailable
		}
		out.Busy = state.Busy
		public := engine.PublicDialogue(state)
		for i, row := range public.Rows {
			source := state.Rows[i]
			item := sharedPreviewRow{ID: row.ID, Role: row.Role, Text: row.Text, At: row.At,
				Streaming: row.Streaming, Thought: row.Thought, Parts: row.Parts, Tools: row.Tools,
				Interrupted: row.Interrupted, Annotations: row.Annotations}
			if row.Role == "agent" {
				previewRowTiming(&item, source.Execution, source.DurationMs)
			}
			if source.PreviewShared {
				item.Images = previewAttachmentMetadata(source.Images)
				item.References = source.References
			}
			out.Rows = append(out.Rows, item)
		}
	}
	if choice, err := h.mainPreviewModel(link, sb); err == nil {
		out.Model = choice
	}
	return out, nil
}

func previewSoloRow(message models.SoloMessage) sharedPreviewRow {
	row := sharedPreviewRow{ID: message.ID, Role: message.Role, Text: message.Content,
		Streaming: strings.HasPrefix(message.ID, "live:"), Annotations: gateshare.SanitizeAnnotations(message.Annotations),
		Interrupted: message.Status == "stopped"}
	if !message.CreatedAt.IsZero() {
		row.At = message.CreatedAt.Format(time.RFC3339Nano)
	}
	if message.Role == "assistant" {
		events := sandbox.AggregateFrames(message.Events)
		row.Parts = gateshare.SanitizeParts(models.PartsForReply(events, message.Content))
		if len(row.Parts) == 0 {
			for _, event := range gateshare.SanitizeLiveEvents(events) {
				if event.Kind == "tool_call" {
					row.Tools = append(row.Tools, models.ReactTool{Title: event.Title, Status: event.Status})
				} else if event.Kind == "thought" {
					row.Thought += event.Text
				}
			}
		}
		previewRowTiming(&row, message.Execution, message.DurationMs)
	}
	if message.PreviewShared {
		row.Images = previewAttachmentMetadata(message.Images)
		row.References = message.References
	}
	return row
}

func previewRowTiming(row *sharedPreviewRow, execution *models.ChatExecution, durationMs int64) {
	if execution != nil && !execution.StartedAt.IsZero() {
		row.StartedAt = execution.StartedAt.Format(time.RFC3339Nano)
	}
	if !row.Streaming {
		row.EndedAt = row.At
	}
	if durationMs > 0 {
		if end, err := time.Parse(time.RFC3339Nano, row.At); err == nil {
			row.StartedAt = end.Add(-time.Duration(durationMs) * time.Millisecond).Format(time.RFC3339Nano)
		}
	}
}
