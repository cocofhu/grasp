package chatsession

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"gorm.io/gorm"
)

// SetExecutionController must run before a queue worker starts. Production
// always supplies a controller, including a closed one when startup is unbound.
// Unwired unit stores retain their isolated, in-memory fixture behavior.
func (s *DurableStore) SetExecutionController(c *executionlineage.Controller) { s.execution = c }

func (s *DurableStore) PrepareExecutionContext(ctx context.Context) (context.Context, error) {
	if s.execution == nil {
		return ctx, nil
	}
	return s.execution.PrepareContext(ctx)
}

func queueExecutionEntities(scope QueueScope, turnID string) []executionlineage.Entity {
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityQueue, ID: scope.ID()}}
	if scope.Surface == "solo" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: scope.Key})
	}
	if turnID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityChatTurn, ID: turnID})
	}
	return entities
}

func queueExecutionError(err error) error {
	if errors.Is(err, executionlineage.ErrInactive) || errors.Is(err, executionlineage.ErrConflict) || errors.Is(err, executionlineage.ErrUnbound) || errors.Is(err, executionlineage.ErrQuarantined) || errors.Is(err, executionlineage.ErrUninitialized) {
		return errors.Join(ErrQueueOwnerLost, err)
	}
	return err
}

func (s *DurableStore) guardExecutionTx(tx *gorm.DB, scope QueueScope, turnID string) error {
	if s.execution == nil {
		return nil
	}
	return queueExecutionError(s.execution.GuardContextTx(tx.Statement.Context, tx, queueExecutionEntities(scope, turnID)...))
}

func (s *DurableStore) executionTransaction(ctx context.Context, scope QueueScope, turnID string, body func(*gorm.DB) error) error {
	if s.execution == nil {
		return s.db.WithContext(ctx).Transaction(body)
	}
	return queueExecutionError(s.execution.Transaction(ctx, s.db, queueExecutionEntities(scope, turnID), body))
}

func (s *DurableStore) executableQueues(query *gorm.DB) *gorm.DB {
	if s.execution == nil {
		return query
	}
	return query.Where("NOT EXISTS (SELECT 1 FROM execution_quarantines q WHERE q.review_state = 'pending' AND ((q.entity_kind = 'chat_queue' AND q.entity_id = chat_queues.id) OR (chat_queues.surface = 'solo' AND q.entity_kind = 'solo' AND q.entity_id = chat_queues.session_key)))")
}
