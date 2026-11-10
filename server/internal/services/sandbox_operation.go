package services

import (
	"context"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/sandboxop"
	"gorm.io/gorm"
)

var errSandboxOperationLost = sandboxop.ErrLost

const sandboxOperationLeaseTTL = sandboxop.LeaseTTL

var publicationContext = sandboxop.PublicationContext

type sandboxOperations struct {
	once    sync.Once
	manager *sandboxop.Manager
}

func (s *SandboxService) operations() *sandboxop.Manager {
	s.ops.once.Do(func() {
		s.ops.manager = sandboxop.New(s.db, sandboxop.Hooks{
			Check:         s.executionCheck,
			Transaction:   s.executionTransaction,
			Guard:         s.guardWorkspaceReleaseTx,
			WrapError:     sandboxExecutionLeaseError,
			RenewInterval: func() time.Duration { return s.operationRenewInterval },
		})
	})
	return s.ops.manager
}

// lockSoloRecovery preserves the existing locking API for scoped operations.
// New data-plane operations use acquireSandboxOperation's cancellable context.
func (s *SandboxService) lockSoloRecovery(ctx context.Context, id uint) (func(), error) {
	_, release, err := s.acquireSandboxOperation(ctx, id)
	return release, err
}

func (s *SandboxService) acquireSandboxOperation(ctx context.Context, id uint, kinds ...string) (context.Context, func(), error) {
	return s.operations().Acquire(ctx, id, kinds...)
}

func (s *SandboxService) heldSandboxOperation(ctx context.Context, id uint) (sandboxop.Lease, bool) {
	if s == nil {
		return sandboxop.Lease{}, false
	}
	return s.operations().Held(ctx, id)
}

func (s *SandboxService) commitSandboxOperation(ctx context.Context, id uint, publish func(*gorm.DB) error) error {
	return s.operations().Commit(ctx, id, nil, publish)
}

func (s *SandboxService) commitSandboxOperationPrepared(ctx context.Context, id uint, prepare, publish func(*gorm.DB) error) error {
	return s.operations().Commit(ctx, id, prepare, publish)
}

func (s *SandboxService) updateSandboxOperation(ctx context.Context, tx *gorm.DB, id uint, updates map[string]any) error {
	return s.operations().Update(ctx, tx, id, updates)
}
