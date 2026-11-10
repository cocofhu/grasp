package chatsession

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

type executionKey struct{}

type Execution struct {
	Store        *DurableStore
	Lease        QueueLease
	TurnID       string
	ownerContext context.Context
}

func executionContext(ctx context.Context, execution Execution) context.Context {
	execution.ownerContext = ctx
	return context.WithValue(ctx, executionKey{}, execution)
}

func (e Execution) checkOwner() error {
	if e.ownerContext != nil && errors.Is(context.Cause(e.ownerContext), ErrQueueOwnerLost) {
		return ErrQueueOwnerLost
	}
	return nil
}

func CurrentExecution(ctx context.Context) (Execution, bool) {
	execution, ok := ctx.Value(executionKey{}).(Execution)
	return execution, ok
}

// FenceExecution and FinishExecution compose the conversation's owner check
// with the surface's own transaction, including its sandbox operation fence.
func FenceExecution(ctx context.Context, tx *gorm.DB) error {
	if execution, ok := CurrentExecution(ctx); ok {
		if err := execution.checkOwner(); err != nil {
			return err
		}
		return execution.Store.Fence(tx, execution.Lease, execution.TurnID)
	}
	return nil
}

func DispatchExecution(ctx context.Context) error {
	if execution, ok := CurrentExecution(ctx); ok {
		if err := execution.checkOwner(); err != nil {
			return err
		}
		return execution.Store.Dispatch(ctx, execution.Lease, execution.TurnID)
	}
	return nil
}

func FinishExecution(ctx context.Context, tx *gorm.DB, state, reason, resultID string) error {
	if execution, ok := CurrentExecution(ctx); ok {
		if err := execution.checkOwner(); err != nil {
			return err
		}
		return execution.Store.Finish(tx, execution.Lease, execution.TurnID, state, reason, resultID)
	}
	return nil
}

func CheckExecution(ctx context.Context) error {
	if execution, ok := CurrentExecution(ctx); ok {
		if err := execution.checkOwner(); err != nil {
			return err
		}
		_, err := execution.Store.Cancellation(ctx, execution.Lease, execution.TurnID)
		return err
	}
	return nil
}

func ExecutionCancellation(ctx context.Context, tx *gorm.DB) (bool, error) {
	execution, ok := CurrentExecution(ctx)
	if !ok {
		return false, nil
	}
	var queue models.ChatQueue
	err := tx.Select("cancel_requested", "retired").First(&queue, "id = ?", execution.Lease.Scope.ID()).Error
	return queue.CancelRequested || queue.Retired, err
}
