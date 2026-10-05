package engine

import (
	"context"
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
	msg := c.run.FirstMessage
	if msg == nil || (strings.TrimSpace(msg.Text) == "" && len(msg.Images) == 0) {
		return
	}
	runID, nodeID := c.run.ID, node.ID
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
		if err := e.ReactReply(runID, nodeID, text, images, nil, false); err != nil {
			log.Warn().Err(err).Str("run_id", runID).Str("node_id", nodeID).
				Msg("clarify first message: deliver failed; releasing latch")
			e.releaseClarifyFirstMessageLatch(runID)
			return
		}
		log.Info().Str("run_id", runID).Str("node_id", nodeID).Int("images", len(images)).
			Msg("clarify first message delivered")
	}()
}
