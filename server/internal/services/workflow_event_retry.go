package services

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// workflowEventRecipient is metadata-only and precedes any source/object load.
// The NOT EXISTS predicate prevents a concurrent valid recipient from being
// orphaned using an earlier absent-session observation. A zero-row race simply
// leaves that event pending for a later pass.
func (s *SoloService) workflowEventRecipient(ctx context.Context, event workflowEventRecord) (bool, error) {
	var session models.SoloSession
	err := s.db.WithContext(ctx).Select("id").Where("id = ? AND user_id = ?", event.SoloID, event.UserID).First(&session).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Preserve delivery's parent -> event order on locking databases, even
		// when the earlier missing observation races with recipient restoration.
		var current models.SoloSession
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ? AND user_id = ?", event.SoloID, event.UserID).First(&current).Error
		if err == nil {
			return nil // A later pass can deliver to this now-valid recipient.
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Model(&workflowEventRecord{}).
			Where("id = ? AND solo_id = ? AND user_id = ? AND disposition = ?", event.ID, event.SoloID, event.UserID, "pending").
			Where("NOT EXISTS (SELECT 1 FROM solo_sessions s WHERE s.id = workflow_conversation_events.solo_id AND s.user_id = workflow_conversation_events.user_id)").
			Updates(map[string]any{"disposition": "orphaned", "next_attempt_at": nil, "last_error": ""}).Error
	})
	return false, err
}

func workflowEventRetryDelay(attempt int) time.Duration {
	if attempt >= 9 {
		return 5 * time.Minute
	}
	if attempt < 1 {
		attempt = 1
	}
	return (2 * time.Second) << (attempt - 1)
}

// deferWorkflowEvent stores only a fixed stage code, never a source body,
// signed object URL, or another user's artifact/error text. The pending +
// attempt + immutable identity CAS prevents a late loser from downgrading a
// delivered/orphaned receipt or doubling the same failure across replicas.
func (s *SoloService) deferWorkflowEvent(ctx context.Context, event workflowEventRecord, stage string) error {
	attempt := event.Attempts + 1
	next := time.Now().UTC().Add(workflowEventRetryDelay(attempt))
	result := s.db.WithContext(ctx).Model(&workflowEventRecord{}).
		Where("id = ? AND disposition = ? AND COALESCE(attempts, 0) = ? AND run_id = ? AND version = ? AND solo_id = ? AND user_id = ? AND kind = ? AND COALESCE(source_version, 0) = ? AND COALESCE(source_count, 0) = ? AND COALESCE(source_fingerprint, '') = ?",
			event.ID, "pending", event.Attempts, event.RunID, event.Version, event.SoloID, event.UserID, event.Kind, event.SourceVersion, event.SourceCount, event.SourceFingerprint).
		Updates(map[string]any{"attempts": attempt, "next_attempt_at": next, "last_error": stage})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		log.Warn().Str("event", event.ID).Str("stage", stage).Int("attempt", attempt).Time("next_attempt_at", next).
			Msg("workflow result delivery deferred; other events continue")
	}
	return nil
}
