package engine

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

const (
	pendingSurfaceClarify = "workflow.clarify"
	pendingSurfaceReview  = "workflow.review"

	clarifyRestartStopped = "澄清回复失败：服务重启，本轮回复已中断，请重试"
	reviewRestartStopped  = "复审修改失败：服务重启，本轮修改已中断，请重试"
)

func pendingSurface(kind sessionKind) string {
	if kind == sessionKindClarify {
		return pendingSurfaceClarify
	}
	return pendingSurfaceReview
}

// RestoreQueuedTurns puts the dialogue turns that were still unfinished when
// the previous process exited back into their sessions, in order. Call it once
// at startup, after the provider hooks are wired.
func (e *Engine) RestoreQueuedTurns() {
	for _, kind := range []sessionKind{sessionKindClarify, sessionKindReview} {
		surface := pendingSurface(kind)
		sessions, keys, err := e.pendingTurns.Load(surface)
		if err != nil {
			log.Error().Err(err).Str("surface", surface).Msg("load queued dialogue turns failed")
			continue
		}
		for _, key := range keys {
			e.restoreReviewSession(kind, key, sessions[key])
		}
	}
}

func (e *Engine) restoreReviewSession(kind sessionKind, key string, stored []chatsession.PendingTurn) {
	surface := pendingSurface(kind)
	runID, producerID, ok := strings.Cut(key, "|")
	var items []*reviewQueueItem
	for _, t := range stored {
		if !ok {
			break
		}
		var it reviewQueueItem
		if err := json.Unmarshal(t.Payload, &it); err != nil || it.ID == "" {
			log.Warn().Err(err).Str("session", key).Str("turn", t.ID).Msg("unreadable queued dialogue turn skipped")
			continue
		}
		if t.Started && e.settleInterruptedReviewTurn(runID, producerID, kind, &it, t.EnqueuedAt) {
			continue
		}
		if it.Live != nil {
			// A Live request carries operation state a replay cannot rebuild;
			// fail it so its card offers a retry.
			e.failCancelledQueuedLive(runID, producerID, it.Live.SID)
			continue
		}
		items = append(items, &it)
	}
	if len(items) > 0 && kind == sessionKindClarify && e.errIfClarifyVisitEnded(runID, producerID, false, nil) != nil {
		log.Info().Str("run_id", runID).Str("node_id", producerID).Int("turns", len(items)).
			Msg("queued clarify turns dropped: the visit ended")
		items = nil
	}
	if len(items) == 0 {
		if err := e.pendingTurns.Clear(surface, key); err != nil {
			log.Warn().Err(err).Str("session", key).Msg("clear queued dialogue turns failed")
		}
		return
	}
	s, release := e.reviewSess.AcquireOrCreate(key, func() *reviewSession {
		return e.newReviewSession(runID, producerID, "", "", kind)
	})
	s.Restore(items)
	release()
	log.Info().Str("run_id", runID).Str("node_id", producerID).Int("turns", len(items)).
		Msg("queued dialogue turns restored")
}

// settleInterruptedReviewTurn closes a turn the restart cut off with a
// retryable failure reply. It reports false when the turn's human message
// never reached the transcript, so it can simply run again.
func (e *Engine) settleInterruptedReviewTurn(runID, producerID string, kind sessionKind, it *reviewQueueItem, enqueuedAt time.Time) bool {
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, producerID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return false
	}
	idx := landedHumanIndex(conv.Messages, it, enqueuedAt)
	if idx < 0 {
		return false
	}
	if idx == len(conv.Messages)-1 && !conv.Done {
		human := conv.Messages[idx]
		text := reviewRestartStopped
		if kind == sessionKindClarify {
			text = clarifyRestartStopped
		}
		conv.Messages = append(conv.Messages, models.ReactMessage{Role: "agent", Text: text,
			At: time.Now().Format(time.RFC3339), ClientMessageID: human.ClientMessageID, GateNodeID: human.GateNodeID})
		logDB(e.db.Save(&conv), runID, "settle dialogue turn interrupted by restart")
		e.broker.Publish(runID, jsonMsg("react", runID, producerID))
	}
	if it.Live != nil {
		e.settleLiveAfterTurn(runID, producerID, it.Live.SID, true)
	}
	return true
}

// landedHumanIndex finds the human message the active turn wrote at its
// start, or -1. A retry reuses the last human, which is only in place once the
// failed reply before it has been dropped.
func landedHumanIndex(msgs []models.ReactMessage, it *reviewQueueItem, enqueuedAt time.Time) int {
	if it.RetryLast {
		if n := len(msgs); n > 0 && msgs[n-1].Role == "human" {
			return n - 1
		}
		return -1
	}
	since := enqueuedAt.Truncate(time.Second)
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != "human" || m.Text != it.Text || m.ClientMessageID != it.ClientMessageID {
			continue
		}
		at, err := time.Parse(time.RFC3339, m.At)
		if err == nil && !at.Before(since) {
			return i
		}
	}
	return -1
}
