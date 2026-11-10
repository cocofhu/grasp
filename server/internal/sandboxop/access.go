package sandboxop

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type accessKey struct{}
type observation struct {
	manager *Manager
	Lease
	kind string
}

// Access obtains a short exclusive lease when idle, or observes an existing
// operation of concurrentKind. Observation grants no mutation ownership: Held,
// Commit and Update still reject it. CommitAccess must validate it before a
// connection is admitted, including when the owner lives on another replica.
func (m *Manager) Access(ctx context.Context, id uint, concurrentKind string) (context.Context, func(), error) {
	return m.acquire(ctx, id, concurrentKind, "access")
}

func (m *Manager) lockAccess(ctx context.Context, id uint, lock chan struct{}, concurrentKind string) (context.Context, error) {
	if concurrentKind == "" {
		select {
		case lock <- struct{}{}:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	for {
		select {
		case lock <- struct{}{}:
			return nil, nil
		default:
		}
		observed, err := m.observe(ctx, id, concurrentKind)
		if err != nil || observed != nil {
			return observed, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (m *Manager) observe(ctx context.Context, id uint, kind string) (context.Context, error) {
	var row models.Sandbox
	if err := m.db.WithContext(ctx).Select("id", "operation_owner", "generation", "operation_kind", "operation_expires_at").First(&row, id).Error; err != nil {
		return nil, err
	}
	if row.OperationOwner == "" || row.OperationKind != kind || row.OperationExpiresAt == nil || !row.OperationExpiresAt.After(time.Now()) {
		return nil, nil
	}
	return context.WithValue(ctx, accessKey{}, observation{m, Lease{id, row.OperationOwner, row.Generation}, kind}), nil
}

// CommitAccess admits a consumer under the exact observed operation. The
// parent's admission lock precedes the sandbox lock, matching lifecycle and
// workspace operations. An expired or superseded observation cannot publish a
// connection pin, renew the owner's lease, or clear it on disconnect.
func (m *Manager) CommitAccess(ctx context.Context, id uint, prepare, publish func(*gorm.DB) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.held(ctx, id) != nil {
		return m.Commit(ctx, id, prepare, publish)
	}
	observed, ok := ctx.Value(accessKey{}).(observation)
	if !ok || observed.manager != m || observed.ID != id {
		return ErrLost
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return m.wrap(m.hooks.Transaction(bounded, id, func(tx *gorm.DB) error {
		if prepare != nil {
			if err := prepare(tx); err != nil {
				return err
			}
		}
		var row models.Sandbox
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "operation_owner", "generation", "operation_kind", "operation_expires_at", "status", "desired_state").First(&row, id).Error; err != nil {
			return err
		}
		valid := func() bool {
			return row.OperationOwner == observed.Owner && row.Generation == observed.Generation && row.OperationKind == observed.kind &&
				row.OperationExpiresAt != nil && row.OperationExpiresAt.After(time.Now()) && row.Status == "running" && row.DesiredState != "stopped"
		}
		if !valid() {
			return ErrLost
		}
		if err := m.hooks.Guard(bounded, tx, id); err != nil {
			return err
		}
		if err := publish(tx); err != nil {
			return err
		}
		if !valid() {
			return ErrLost
		}
		return bounded.Err()
	}))
}
