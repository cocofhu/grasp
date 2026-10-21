package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
)

// normalizeFirstMessage trims the opening message and externalizes its inline
// image data. A blank message (no text, no images) collapses to nil so nothing
// is persisted or delivered.
func normalizeFirstMessage(ctx context.Context, store blob.Store, msg *models.CompositeText) (*models.CompositeText, error) {
	if msg == nil {
		return nil, nil
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" && len(msg.Images) == 0 {
		return nil, nil
	}
	images, err := blob.IngestPromptImages(ctx, store, msg.Images)
	if err != nil {
		return nil, err
	}
	return &models.CompositeText{Text: text, Images: images}, nil
}

// releaseClarifyFirstMessageLatch clears the delivery latch so a cancelled or
// resumed run can claim FirstMessage again on a fresh clarify visit.
func (e *Engine) releaseClarifyFirstMessageLatch(runID string) {
	logDB(e.db.Model(&models.Run{}).Where("id = ? AND first_message_delivered_at IS NOT NULL", runID).
		UpdateColumn("first_message_delivered_at", nil), runID, "release clarify first message latch")
}

// visitHumanTurns reports whether the visit's dialogue has a human turn, and
// whether its last message is a human turn still waiting for an answer.
func (e *Engine) visitHumanTurns(runID, nodeID string, iter int) (has, unanswered bool) {
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iter).
		Order("id desc").First(&conv).Error; err != nil {
		return false, false
	}
	for _, m := range conv.Messages {
		if m.Role == "human" {
			has = true
		}
	}
	n := len(conv.Messages)
	return has, n > 0 && conv.Messages[n-1].Role == "human"
}

// fireClarifyFirstMessage delivers Run.FirstMessage into a clarify node's
// sandbox right after that node parks, so the launcher (home chat) can navigate
// away immediately instead of polling for the pause and sending it itself.
//
// Exactly-once is enforced by a conditional UPDATE on
// runs.first_message_delivered_at: only the writer that flips it from NULL
// delivers. A failed delivery releases the latch so a later pause of the same
// node (loop-back / resume) can retry. Cancel / ResumeFrom also release the
// latch so a new visit with an empty transcript can re-claim.
func (e *Engine) fireClarifyFirstMessage(c *execCtx, node *models.Node) {
	if c == nil || c.run == nil || node == nil || !node.Caps.Clarify() {
		return
	}
	runID, nodeID := c.run.ID, node.ID
	if has, unanswered := e.visitHumanTurns(runID, nodeID, c.iter[nodeID]); has {
		// The dialogue carried over from an interrupted visit already has it;
		// a reply the interruption left unanswered is answered now.
		if unanswered {
			go func() {
				if _, err := e.EnqueueClarifyRetryLast(runID, nodeID); err != nil {
					log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
						Msg("resumed clarify: re-answer last reply failed")
				}
			}()
		}
		return
	}
	msg := c.run.FirstMessage
	if msg == nil || (strings.TrimSpace(msg.Text) == "" && len(msg.Images) == 0) {
		return
	}
	now := time.Now()
	res := e.db.Model(&models.Run{}).
		Where("id = ? AND first_message_delivered_at IS NULL", runID).
		Update("first_message_delivered_at", now)
	if res.Error != nil {
		log.Warn().Err(res.Error).Str("run_id", runID).Str("node_id", nodeID).
			Msg("clarify first message: claim failed")
		return
	}
	if res.RowsAffected == 0 {
		return
	}
	c.run.FirstMessageDeliveredAt = &now

	text := msg.Text
	images := append([]models.PromptImage(nil), msg.Images...)
	claimedAt := now
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Str("run_id", runID).Str("node_id", nodeID).
					Interface("panic", r).Msg("clarify first message: deliver panic")
			}
		}()
		// Cancel/Resume may release or re-claim the latch while this goroutine is
		// in flight; only deliver when our claim is still authoritative.
		var stored models.Run
		if err := e.db.Select("first_message_delivered_at").First(&stored, "id = ?", runID).Error; err != nil {
			log.Warn().Err(err).Str("run_id", runID).Msg("clarify first message: claim check failed")
			return
		}
		if stored.FirstMessageDeliveredAt == nil || !stored.FirstMessageDeliveredAt.Equal(claimedAt) {
			log.Info().Str("run_id", runID).Str("node_id", nodeID).
				Msg("clarify first message: claim superseded; skip stale delivery")
			return
		}
		// Marked AutoRetryOnResume so a visit-ended refusal tells callers the
		// latch will be released and ResumeFrom will redeliver.
		if _, err := e.EnqueueClarifyFirstMessageAs("", runID, nodeID, text, images); err != nil {
			// No client sent this turn, so broadcast the refusal for any open
			// page ("续跑后会自动重新发送").
			var ve *ClarifyVisitEndedError
			if errors.As(err, &ve) {
				e.publishClarifyVisitEnded(runID, ve)
			}
			log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
				Msg("clarify first message: deliver failed; releasing latch")
			e.releaseClarifyFirstMessageLatch(runID)
			return
		}
		log.Info().Str("run_id", runID).Str("node_id", nodeID).Int("images", len(images)).
			Msg("clarify first message delivered")
	}()
}
