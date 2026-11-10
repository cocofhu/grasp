// Package sandboxop owns the per-sandbox operation lease: a database row
// fence that serializes file, model and lifecycle mutations across replicas.
package sandboxop

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/leasewatch"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrLost = errors.New("环境操作租约已丢失，已停止本轮操作，可核对状态后重试")

const LeaseTTL = 45 * time.Second

// Hooks are the sandbox-service checks every lease write runs under.
type Hooks struct {
	// Check rejects work for a sandbox whose execution lineage is no longer active.
	Check func(ctx context.Context, id uint) error
	// Transaction runs body under the sandbox's execution lineage fence.
	Transaction func(ctx context.Context, id uint, body func(*gorm.DB) error) error
	// Guard rejects acquisition while an environment release owns the sandbox.
	Guard func(ctx context.Context, tx *gorm.DB, id uint) error
	// WrapError maps lineage failures onto ErrLost.
	WrapError func(error) error
	// RenewInterval may shorten renewal in tests; the default is LeaseTTL/3.
	RenewInterval func() time.Duration
}

// Lease identifies the physical owner and generation held by a context.
type Lease struct {
	ID         uint
	Owner      string
	Generation uint64
}

type contextKey struct{}

type operation struct {
	manager *Manager
	Lease
	parent *operation
}

type Manager struct {
	db    *gorm.DB
	hooks Hooks
	// Per-resident locks serialize work inside one replica without making one
	// unavailable sandbox block every other session.
	locks sync.Map
}

func New(db *gorm.DB, hooks Hooks) *Manager {
	return &Manager{db: db, hooks: hooks}
}

func (m *Manager) wrap(err error) error {
	if m.hooks.WrapError == nil {
		return err
	}
	return m.hooks.WrapError(err)
}

func (m *Manager) held(ctx context.Context, id uint) *operation {
	for op, _ := ctx.Value(contextKey{}).(*operation); op != nil; op = op.parent {
		if op.manager == m && op.ID == id {
			return op
		}
	}
	return nil
}

// Held reports the lease this manager holds for id in ctx.
func (m *Manager) Held(ctx context.Context, id uint) (Lease, bool) {
	if op := m.held(ctx, id); op != nil {
		return op.Lease, true
	}
	return Lease{}, false
}

// Current reports the innermost lease this manager holds in ctx.
func (m *Manager) Current(ctx context.Context) (Lease, bool) {
	for op, _ := ctx.Value(contextKey{}).(*operation); op != nil; op = op.parent {
		if op.manager == m {
			return op.Lease, true
		}
	}
	return Lease{}, false
}

// InLease reports whether ctx carries any operation lease.
func InLease(ctx context.Context) bool {
	_, ok := ctx.Value(contextKey{}).(*operation)
	return ok
}

// LocalIdle reports whether this replica has used id's lock and released it.
func (m *Manager) LocalIdle(id uint) bool {
	value, ok := m.locks.Load(id)
	return ok && len(value.(chan struct{})) == 0
}

// Acquire serializes mutations across replicas. A protected context may
// re-enter the same manager/resource without acquiring its own lease. Losing
// ownership or renewal cancels the returned context.
func (m *Manager) Acquire(ctx context.Context, id uint, kinds ...string) (context.Context, func(), error) {
	return m.acquire(ctx, id, "", kinds...)
}

func (m *Manager) acquire(ctx context.Context, id uint, concurrentKind string, kinds ...string) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := m.hooks.Check(ctx, id); err != nil {
		return nil, nil, m.wrap(err)
	}
	parent, _ := ctx.Value(contextKey{}).(*operation)
	if m.held(ctx, id) != nil {
		return ctx, func() {}, nil
	}
	value, _ := m.locks.LoadOrStore(id, make(chan struct{}, 1))
	lock := value.(chan struct{})
	observed, err := m.lockAccess(ctx, id, lock, concurrentKind)
	if err != nil {
		return nil, nil, err
	}
	if observed != nil {
		return observed, func() {}, nil
	}
	owner := uuid.NewString()
	kind := "lifecycle"
	if len(kinds) > 0 && kinds[0] != "" {
		kind = kinds[0]
	}
	for {
		now := time.Now().UTC()
		var result *gorm.DB
		err := m.hooks.Transaction(ctx, id, func(tx *gorm.DB) error {
			if err := m.hooks.Guard(ctx, tx, id); err != nil {
				return err
			}
			result = tx.Model(&models.Sandbox{}).
				Where("id = ? AND (operation_owner = '' OR operation_owner IS NULL OR operation_expires_at < ?)", id, now).
				Updates(map[string]any{"operation_owner": owner, "operation_kind": kind, "operation_expires_at": now.Add(LeaseTTL), "generation": gorm.Expr("COALESCE(generation, 0) + 1")})
			return result.Error
		})
		if err != nil {
			<-lock
			return nil, nil, m.wrap(err)
		}
		if result.RowsAffected > 0 {
			break
		}
		if concurrentKind != "" {
			observed, err := m.observe(ctx, id, concurrentKind)
			if err != nil || observed != nil {
				<-lock
				return observed, func() {}, err
			}
		}
		var count int64
		if err := m.db.WithContext(ctx).Model(&models.Sandbox{}).Where("id = ?", id).Count(&count).Error; err != nil || count == 0 {
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
	if err := m.db.WithContext(ctx).Select("generation", "operation_expires_at").Where("id = ? AND operation_owner = ?", id, owner).First(&row).Error; err != nil {
		m.release(id, owner, nil)
		<-lock
		return nil, nil, err
	}
	if row.OperationExpiresAt == nil || !row.OperationExpiresAt.After(time.Now()) {
		m.release(id, owner, &row.Generation)
		<-lock
		return nil, nil, ErrLost
	}
	op := &operation{manager: m, Lease: Lease{ID: id, Owner: owner, Generation: row.Generation}, parent: parent}
	leaseCtx, cancel := context.WithCancelCause(ctx)
	leaseCtx = context.WithValue(leaseCtx, contextKey{}, op)
	done := make(chan struct{})
	interval := LeaseTTL / 3
	if m.hooks.RenewInterval != nil {
		if custom := m.hooks.RenewInterval(); custom > 0 && custom < interval {
			interval = custom
		}
	}
	go func() {
		defer close(done)
		err := leasewatch.Maintain(leaseCtx, *row.OperationExpiresAt, interval, ErrLost,
			func(ctx context.Context) (time.Time, error) {
				now := time.Now().UTC()
				expiresAt := now.Add(LeaseTTL)
				err := m.hooks.Transaction(ctx, id, func(tx *gorm.DB) error {
					result := tx.Model(&models.Sandbox{}).
						Where("id = ? AND operation_owner = ? AND generation = ? AND operation_expires_at > ?", id, owner, op.Generation, now).
						Update("operation_expires_at", expiresAt)
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected != 1 {
						return ErrLost
					}
					return nil
				})
				if err != nil {
					return time.Time{}, m.wrap(err)
				}
				return expiresAt, nil
			})
		cancel(err)
	}()
	var once sync.Once
	return leaseCtx, func() {
		once.Do(func() {
			cancel(context.Canceled)
			<-done
			m.release(id, owner, &op.Generation)
			<-lock
		})
	}, nil
}

func (m *Manager) release(id uint, owner string, generation *uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.hooks.Transaction(ctx, id, func(tx *gorm.DB) error {
		query := tx.Model(&models.Sandbox{}).Where("id = ? AND operation_owner = ?", id, owner)
		if generation != nil {
			query = query.Where("generation = ?", *generation)
		}
		return query.Updates(map[string]any{"operation_owner": "", "operation_kind": "", "operation_expires_at": nil}).Error
	})
}

// PublicationContext lets user cancellation save a final stopped result, while
// preserving an earlier persistence deadline across nested publication fences.
func PublicationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	if original, ok := ctx.Deadline(); ok && original.Before(deadline) {
		deadline = original
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}

// Commit fences the final SQL publication on the same row used to acquire and
// renew the lease. The row lock lives through the entire transaction, so a
// superseded worker cannot overwrite its successor's transcript or state. A
// cancelled user turn may still persist its final cancellation while it owns
// the resource; publication gets a separate bounded cleanup context.
//
// prepare takes any higher-level admission lock before the sandbox row lock.
// This keeps stop and new-message admission in the same lock order.
func (m *Manager) Commit(ctx context.Context, id uint, prepare, publish func(*gorm.DB) error) error {
	op := m.held(ctx, id)
	if op == nil {
		return ErrLost
	}
	persistence, cancel := PublicationContext(ctx, 5*time.Second)
	defer cancel()
	if err := persistence.Err(); err != nil {
		return err
	}
	return m.hooks.Transaction(persistence, id, func(tx *gorm.DB) error {
		if prepare != nil {
			if err := prepare(tx); err != nil {
				return err
			}
		}
		var row models.Sandbox
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "operation_owner", "generation", "operation_expires_at").First(&row, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLost
		}
		if err != nil {
			return err
		}
		if row.OperationOwner != op.Owner || row.Generation != op.Generation || row.OperationExpiresAt == nil || !row.OperationExpiresAt.After(time.Now()) {
			return ErrLost
		}
		if err := publish(tx); err != nil {
			return err
		}
		if err := persistence.Err(); err != nil {
			return err
		}
		// Contention or a slow SQL callback must not commit after the lease
		// checked at the start has already expired.
		if !row.OperationExpiresAt.After(time.Now()) {
			return ErrLost
		}
		return nil
	})
}

// Update also checks the affected row count: a callback, concurrent state
// change or expired generation must never be acknowledged as a successful
// intent/result write.
func (m *Manager) Update(ctx context.Context, tx *gorm.DB, id uint, updates map[string]any) error {
	op := m.held(ctx, id)
	if op == nil {
		return ErrLost
	}
	result := tx.Model(&models.Sandbox{}).
		Where("id = ? AND operation_owner = ? AND generation = ? AND operation_expires_at > ?", id, op.Owner, op.Generation, time.Now().UTC()).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrLost
	}
	return nil
}
