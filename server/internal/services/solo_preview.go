package services

import (
	"context"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// MessageHasAttachment checks the attachment against the specific persisted
// message in the caller's already authorized Solo session.
func (s *SoloService) MessageHasAttachment(sessionID, messageID, ref string) bool {
	message, err := readSoloMessage(s.db.Select("id", "images").Where("session_id = ? AND id = ?", sessionID, messageID), soloMessageImages)
	if err != nil {
		return false
	}
	for _, image := range message.Images {
		if image.Ref == ref {
			return true
		}
	}
	return false
}

// PreviewMessageAttachments reads attachments explicitly submitted to the shared chat.
func (s *SoloService) PreviewMessageAttachments(sessionID, messageID string) []models.PromptImage {
	message, err := readSoloMessage(s.db.Select("id", "images").Where("session_id = ? AND id = ? AND preview_shared = ?", sessionID, messageID, true), soloMessageImages)
	if err != nil {
		return nil
	}
	return message.Images
}

// PreviewTranscript combines saved messages with the current turn for the
// explicit main-conversation share. Callers must sanitize its event timeline.
func (s *SoloService) PreviewTranscript(id, user string) ([]models.SoloMessage, bool, error) {
	messages, err := s.Messages(id, user)
	if err != nil {
		return messages, false, err
	}
	live, busy := s.PreviewLiveReply(id)
	if live == nil {
		return messages, busy, nil
	}
	for i, message := range messages {
		if "live:"+message.ID != live.ID {
			continue
		}
		// The finished reply may already be saved while the queue is settling.
		for _, next := range messages[i+1:] {
			if next.Role == "assistant" {
				return messages, busy, nil
			}
		}
		// Later system messages must not hide the active reply.
		messages = append(messages, models.SoloMessage{})
		copy(messages[i+2:], messages[i+1:])
		messages[i+1] = *live
		break
	}
	return messages, busy, nil
}

// PreviewLiveReply snapshots only the active turn, without reading persisted
// history. Callers must authorize the session before subscribing and sanitize
// its event timeline before sending it to public viewers.
func (s *SoloService) PreviewLiveReply(id string) (*models.SoloMessage, bool) {
	ctx := context.Background()
	snapshot, err := s.durable.Snapshot(ctx, soloQueueScope(id))
	if err != nil || snapshot.Active == nil {
		return nil, err != nil
	}
	events, execution, err := s.journalTurn(ctx, snapshot.Active.ID, id, int64(snapshot.Queue.NextEventSequence)-1)
	if err != nil || len(events) == 0 {
		return nil, true
	}
	var text strings.Builder
	for _, event := range events {
		text.WriteString(extractPmAgentText(event))
	}
	return &models.SoloMessage{ID: "live:" + snapshot.Active.ID, Role: "assistant", Content: text.String(), Events: events, Execution: execution}, true
}

// PreviewPending reads the authoritative FIFO without querying stored history.
// Callers must authorize the session and redact attachments before publication.
func (s *SoloService) PreviewPending(id string) []models.SoloMessage {
	c := s.lookup(id)
	if c == nil {
		return nil
	}
	pending := c.sess.View().Pending
	out := make([]models.SoloMessage, 0, len(pending))
	for _, item := range pending {
		out = append(out, models.SoloMessage{ID: item.ID, Role: "user", Content: item.Text,
			Images:      append([]models.PromptImage(nil), item.Images...),
			Annotations: append([]models.ReactAnnotation(nil), item.Annotations...)})
	}
	return out
}

// PreviewActiveTurnID reads the active FIFO item without querying history.
// Callers must authorize the preview before publishing this opaque identifier.
func (s *SoloService) PreviewActiveTurnID(id string) string {
	if c := s.lookup(id); c != nil {
		if active, ok := c.sess.Active(); ok {
			return active.ID
		}
	}
	return ""
}
