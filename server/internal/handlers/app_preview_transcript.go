package handlers

import (
	"errors"
	"net/http"
	"sort"
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
	Rows         []sharedPreviewRow  `json:"rows"`
	Queue        []previewQueuedItem `json:"queue"`
	Busy         bool                `json:"busy"`
	Model        any                 `json:"model,omitempty"`
	ActiveTurnID string              `json:"activeTurnId,omitempty"`
}

// Public reply facts deliberately omit model-configuration and operation IDs.
type previewReplyExecution struct {
	AgentName       string `json:"agentName"`
	Backend         string `json:"backend"`
	ModelID         string `json:"modelId,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	StartedAt       string `json:"startedAt,omitempty"`
}

func previewExecution(source *models.ChatExecution) *previewReplyExecution {
	if source == nil {
		return nil
	}
	out := &previewReplyExecution{AgentName: source.AgentName, Backend: source.Backend,
		ModelID: source.ModelID, ReasoningEffort: source.ReasoningEffort}
	if !source.StartedAt.IsZero() {
		out.StartedAt = source.StartedAt.Format(time.RFC3339Nano)
	}
	return out
}

type previewQueuedItem struct {
	ID          string                        `json:"id"`
	Text        string                        `json:"text"`
	Images      []models.PromptImage          `json:"images,omitempty"`
	Annotations []gateshare.PreviewAnnotation `json:"annotations,omitempty"`
}

func previewPending(item models.SoloMessage) previewQueuedItem {
	out := previewQueuedItem{ID: item.ID, Text: item.Content, Annotations: gateshare.SanitizeAnnotations(item.Annotations)}
	for _, image := range item.Images {
		// Queue badges need only a count. Owner attachment names and addresses stay private.
		out.Images = append(out.Images, models.PromptImage{MimeType: image.MimeType})
	}
	return out
}

func previewWorkflowQueue(state engine.PublicDialogueState) []previewQueuedItem {
	turns := append([]engine.PublicDialogueTurn(nil), state.Turns...)
	sort.SliceStable(turns, func(i, j int) bool {
		return turns[i].Position != nil && (turns[j].Position == nil || *turns[i].Position < *turns[j].Position)
	})
	queue := []previewQueuedItem{}
	for _, turn := range turns {
		if turn.Phase != "queued" || turn.Item == nil {
			continue
		}
		item := previewQueuedItem{ID: turn.TurnID, Text: turn.Item.Text, Annotations: turn.Item.Annotations}
		for _, image := range turn.Item.Images {
			item.Images = append(item.Images, models.PromptImage{MimeType: image.MimeType})
		}
		queue = append(queue, item)
	}
	return queue
}

// appPreviewMainHistory is shared by live pushes and reconnect/poll recovery.
// Raw ACP frames and tool arguments/outputs never cross the public boundary.
func (h *Handlers) appPreviewMainHistory(link *models.GateShareLink, sb models.Sandbox) (*previewMainHistory, error) {
	out := &previewMainHistory{Rows: []sharedPreviewRow{}, Queue: []previewQueuedItem{}}
	if sb.Purpose == services.SandboxPurposeSolo {
		if h.Solo == nil {
			return nil, errPreviewMainServiceUnavailable
		}
		messages, busy, err := h.Solo.PreviewTranscript(sb.ThreadID, link.CreatedBy)
		if err != nil {
			return nil, err
		}
		out.Busy = busy
		out.ActiveTurnID = h.Solo.PreviewActiveTurnID(sb.ThreadID)
		for _, item := range h.Solo.PreviewPending(sb.ThreadID) {
			out.Queue = append(out.Queue, previewPending(item))
		}
		for _, message := range messages {
			out.Rows = append(out.Rows, previewSoloRow(message))
		}
	} else {
		state, active := h.workflowPreviewDialogue(link.RunID, link.NodeID)
		if !active || state.Iteration != link.Iteration {
			return nil, errPreviewMainUnavailable
		}
		out.Busy = state.Busy
		for _, turn := range state.Turns {
			if turn.Phase == "running" {
				out.ActiveTurnID = turn.TurnID
				break
			}
		}
		public := engine.PublicDialogue(state)
		out.Queue = previewWorkflowQueue(public)
		for i, row := range public.Rows {
			source := state.Rows[i]
			item := sharedPreviewRow{ID: row.ID, Role: row.Role, Text: row.Text, At: row.At,
				Streaming: row.Streaming, Thought: row.Thought, Parts: row.Parts, Tools: row.Tools,
				Interrupted: row.Interrupted, Annotations: row.Annotations}
			if row.Role == "agent" {
				item.Execution = previewExecution(source.Execution)
				item.Usage = models.CloneTokenUsage(source.Usage)
				item.UsageByModel = models.CloneTokenUsageByModel(source.UsageByModel)
				item.DurationMs = source.DurationMs
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
		row.Execution = previewExecution(message.Execution)
		row.Usage = models.CloneTokenUsage(message.Usage)
		row.UsageByModel = models.CloneTokenUsageByModel(message.UsageByModel)
		row.DurationMs = message.DurationMs
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
