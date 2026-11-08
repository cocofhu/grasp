package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errSandboxOperationLost = errors.New("环境操作租约已丢失，已停止本轮操作，可核对状态后重试")

type sandboxOperationKey struct{}

type sandboxOperation struct {
	service    *SandboxService
	id         uint
	owner      string
	generation uint64
	parent     *sandboxOperation
}

// lockSoloRecovery preserves the existing locking API for scoped operations.
// New data-plane operations use acquireSandboxOperation's cancellable context.
func (s *SandboxService) lockSoloRecovery(ctx context.Context, id uint) (func(), error) {
	_, release, err := s.acquireSandboxOperation(ctx, id)
	return release, err
}

// acquireSandboxOperation serializes file/model/lifecycle mutations across
// replicas. A protected context may re-enter this same service/resource without
// trying to acquire its own lease. Losing ownership or renewal stops its work.
func (s *SandboxService) acquireSandboxOperation(ctx context.Context, id uint, kinds ...string) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	parent, _ := ctx.Value(sandboxOperationKey{}).(*sandboxOperation)
	for held := parent; held != nil; held = held.parent {
		if held.service == s && held.id == id {
			return ctx, func() {}, nil
		}
	}
	value, _ := s.soloRecoveryLocks.LoadOrStore(id, make(chan struct{}, 1))
	lock := value.(chan struct{})
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	owner := uuid.NewString()
	kind := "lifecycle"
	if len(kinds) > 0 && kinds[0] != "" {
		kind = kinds[0]
	}
	for {
		now := time.Now().UTC()
		result := s.db.WithContext(ctx).Model(&models.Sandbox{}).
			Where("id = ? AND (operation_owner = '' OR operation_owner IS NULL OR operation_expires_at < ?)", id, now).
			Updates(map[string]any{"operation_owner": owner, "operation_kind": kind, "operation_expires_at": now.Add(5 * time.Minute), "generation": gorm.Expr("COALESCE(generation, 0) + 1")})
		if result.Error != nil {
			<-lock
			return nil, nil, result.Error
		}
		if result.RowsAffected > 0 {
			break
		}
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.Sandbox{}).Where("id = ?", id).Count(&count).Error; err != nil || count == 0 {
			<-lock
			if err != nil {
				return nil, nil, err
			}
			return nil, nil, fmt.Errorf("sandbox not found")
		}
		select {
		case <-ctx.Done():
			<-lock
			return nil, nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	var row models.Sandbox
	if err := s.db.WithContext(ctx).Select("generation").Where("id = ? AND operation_owner = ?", id, owner).First(&row).Error; err != nil {
		s.releaseSandboxOperation(id, owner, nil)
		<-lock
		return nil, nil, err
	}
	operation := &sandboxOperation{service: s, id: id, owner: owner, generation: row.Generation, parent: parent}
	leaseCtx, cancel := context.WithCancelCause(ctx)
	leaseCtx = context.WithValue(leaseCtx, sandboxOperationKey{}, operation)
	done := make(chan struct{})
	interval := s.operationRenewInterval
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				now := time.Now().UTC()
				result := s.db.WithContext(leaseCtx).Model(&models.Sandbox{}).
					Where("id = ? AND operation_owner = ? AND generation = ? AND operation_expires_at > ?", id, owner, operation.generation, now).
					Update("operation_expires_at", now.Add(5*time.Minute))
				if result.Error != nil || result.RowsAffected != 1 {
					cancel(errSandboxOperationLost)
					return
				}
			}
		}
	}()
	var once sync.Once
	return leaseCtx, func() {
		once.Do(func() {
			cancel(context.Canceled)
			<-done
			s.releaseSandboxOperation(id, owner, &operation.generation)
			<-lock
		})
	}, nil
}

func (s *SandboxService) releaseSandboxOperation(id uint, owner string, generation *uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := s.db.WithContext(ctx).Model(&models.Sandbox{}).Where("id = ? AND operation_owner = ?", id, owner)
	if generation != nil {
		query = query.Where("generation = ?", *generation)
	}
	_ = query.Updates(map[string]any{"operation_owner": "", "operation_kind": "", "operation_expires_at": nil}).Error
}

// commitSandboxOperation fences the final SQL publication on the same row used
// to acquire/renew the lease. The row lock lives through the entire transaction,
// so a superseded worker cannot overwrite its successor's transcript/state.
// A cancelled user turn may still persist its final cancellation while it owns
// the resource; publication gets a separate bounded cleanup context.
func (s *SandboxService) commitSandboxOperation(ctx context.Context, id uint, publish func(*gorm.DB) error) error {
	var operation *sandboxOperation
	for held, _ := ctx.Value(sandboxOperationKey{}).(*sandboxOperation); held != nil; held = held.parent {
		if held.service == s && held.id == id {
			operation = held
			break
		}
	}
	if operation == nil {
		return errSandboxOperationLost
	}
	persistence, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.db.WithContext(persistence).Transaction(func(tx *gorm.DB) error {
		var row models.Sandbox
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "operation_owner", "generation", "operation_expires_at").First(&row, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errSandboxOperationLost
		}
		if err != nil {
			return err
		}
		if row.OperationOwner != operation.owner || row.Generation != operation.generation || row.OperationExpiresAt == nil || !row.OperationExpiresAt.After(time.Now()) {
			return errSandboxOperationLost
		}
		return publish(tx)
	})
}
