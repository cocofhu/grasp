package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// WorkspaceJobCompletion holds privately verified evidence. Commit is pure SQL
// and runs inside the same owner/current-grant/physical fence as the job final
// state. Close performs local cleanup only, after the outer transaction ends.
type WorkspaceJobCompletion interface {
	Commit(context.Context, *gorm.DB) error
	Close() error
}

type WorkspaceJobProcessor interface {
	// Process runs under the existing SQL, physical and renewable remote job
	// lease. All archive/object I/O belongs here, outside any SQL transaction.
	Process(context.Context, models.WorkspaceSnapshotJob, WorkspaceJobRemote, func(context.Context) (sandbox.WorkspaceJobRenew, error)) (WorkspaceJobCompletion, error)
	// Recover is explicit and read-only at the native transport: it may verify
	// an existing storage hold, never CREATE/replay or send Finish again.
	Recover(context.Context, models.WorkspaceSnapshotJob, sandbox.WorkspaceJobReceipt) (WorkspaceJobCompletion, error)
}

func (s *WorkspaceSnapshotJobs) processorFence(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease, acknowledged *atomic.Int64) (sandbox.WorkspaceJobRenew, error) {
	fence, err := s.controlFence(ctx, row, lease)
	if err != nil {
		return sandbox.WorkspaceJobRenew{}, err
	}
	until := time.Unix(0, acknowledged.Load())
	if fence.Generation != row.RuntimeGeneration || fence.Owner != row.RuntimeOwner || !until.After(time.Now()) {
		return sandbox.WorkspaceJobRenew{}, ErrWorkspaceJobOwner
	}
	if until.Before(fence.ExpiresAt) {
		fence.ExpiresAt = until
	}
	return sandbox.WorkspaceJobRenew{SHA256: row.RuntimeSHA256, Lineage: row.Lineage, Fence: fence}, nil
}

func (s *WorkspaceSnapshotJobs) observedCompletion(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease, receipt sandbox.WorkspaceJobReceipt, states []string, completion WorkspaceJobCompletion) error {
	if completion == nil || receipt.State != "completed" {
		return s.observed(ctx, row, lease, receipt, states)
	}
	if validateRuntimeJob(row, receipt) != nil || len(receipt.Result) > 64<<10 || !json.Valid(receipt.Result) {
		return ErrWorkspaceJobUnknown
	}
	if validator, ok := completion.(interface {
		ValidateReceipt(sandbox.WorkspaceJobReceipt) error
	}); ok {
		if err := validator.ValidateReceipt(receipt); err != nil {
			return err
		}
	}
	if err := s.publishCompletion(ctx, row, lease, states, map[string]any{"state": "executed", "result": string(receipt.Result), "error": receipt.Error}, completion); err != nil {
		return errors.Join(ErrWorkspaceJobUnknown, err)
	}
	if publisher, ok := completion.(interface{ Published(context.Context) error }); ok {
		return publisher.Published(ctx)
	}
	return nil
}

type workspaceJobMetadataCompletion struct {
	commit func(context.Context, *gorm.DB) error
}

func (c workspaceJobMetadataCompletion) Commit(ctx context.Context, tx *gorm.DB) error {
	return c.commit(ctx, tx)
}
func (c workspaceJobMetadataCompletion) Close() error { return nil }
