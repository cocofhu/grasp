package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// Run belongs to the application's lifetime, never to an HTTP request. Only
// never-dispatched queued intents are eligible for automatic execution.
func (s *WorkspaceSnapshotJobs) Run(ctx context.Context) error {
	if s.options.Authority == nil {
		return ErrWorkspaceJobAuthority
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, s.options.MaxWorkers)
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		lineage, err := s.options.Authority.Current(ctx)
		if err == nil && s.verify(ctx, lineage) == nil {
			s.expire(ctx, lineage)
			var rows []models.WorkspaceSnapshotJob
			if s.db.WithContext(ctx).Select("id").Where("lineage = ? AND state = ? AND dispatched_at IS NULL AND deadline > ? AND (owner = '' OR owner IS NULL OR expires_at < ?)", lineage, "queued", time.Now().UTC(), time.Now().UTC()).Order("created_at, id").Limit(s.options.MaxWorkers).Find(&rows).Error == nil {
				for _, row := range rows {
					select {
					case slots <- struct{}{}:
					default:
						continue
					}
					if _, loaded := s.local.LoadOrStore(row.ID, struct{}{}); loaded {
						<-slots
						continue
					}
					wg.Add(1)
					go func(id string) {
						defer wg.Done()
						defer func() { s.local.Delete(id); <-slots }()
						_ = s.RunOne(ctx, id)
					}(row.ID)
				}
			}
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-ticker.C:
		}
	}
}

// expire does not infer provider success or retry a side effect. An expired
// dispatched intent becomes unknown and requires an explicit read-only check.
func (s *WorkspaceSnapshotJobs) expire(ctx context.Context, lineage string) {
	now := time.Now().UTC()
	var rows []models.WorkspaceSnapshotJob
	if err := s.db.WithContext(ctx).Where("lineage = ? AND ((state IN ? AND dispatched_at IS NOT NULL AND (expires_at IS NULL OR expires_at <= ? OR deadline <= ?)) OR (state = ? AND dispatched_at IS NULL AND deadline <= ?))", lineage, []string{"dispatching", "running"}, now, now, "queued", now).Order("id").Limit(100).Find(&rows).Error; err != nil {
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		_ = s.metadataTransaction(ctx, row, func(tx *gorm.DB) error {
			query := tx.Model(&models.WorkspaceSnapshotJob{}).Where("id = ? AND lineage = ? AND state = ? AND generation = ?", row.ID, row.Lineage, row.State, row.Generation)
			if row.DispatchedAt != nil {
				return query.Where("dispatched_at IS NOT NULL AND (expires_at IS NULL OR expires_at <= ? OR deadline <= ?)", now, now).Updates(map[string]any{"state": "unknown", "error": "execution_owner_expired"}).Error
			}
			return query.Where("dispatched_at IS NULL AND deadline <= ?", now).Updates(map[string]any{"state": "failed", "error": "job_deadline_expired"}).Error
		})
	}
}

func (s *WorkspaceSnapshotJobs) RunOne(ctx context.Context, id string) error {
	row, lease, err := s.claim(ctx, id, false)
	if err != nil {
		return err
	}
	return s.owned(ctx, lease, false, func(work context.Context, _ models.WorkspaceSnapshotJob, lease workspaceJobLease) error {
		remote, err := s.options.Remote(work, row.SandboxID)
		if err != nil {
			return s.preflightFailure(work, row, lease, err)
		}
		var resource models.Sandbox
		if err := s.db.WithContext(work).First(&resource, row.SandboxID).Error; err != nil {
			return s.preflightFailure(work, row, lease, err)
		}
		if resource.Status != "running" || (resource.DesiredState != "" && resource.DesiredState != "running") {
			return s.preflightFailure(work, row, lease, ErrWorkspaceJobUnsupported)
		}
		call, cancel := context.WithTimeout(work, 5*time.Second)
		caps, err := remote.Capabilities(call)
		cancel()
		if err != nil || !supportsRuntimeJob(row, caps, resource.Name) || row.Deadline.After(time.Now().Add(time.Duration(caps.MaxDurationMillis)*time.Millisecond)) {
			return s.preflightFailure(work, row, lease, ErrWorkspaceJobUnsupported)
		}
		fence, err := s.controlFence(work, row, lease)
		if err != nil {
			return s.preflightFailure(work, row, lease, err)
		}
		maxLease := min(time.Duration(caps.MaxLeaseMillis)*time.Millisecond, s.options.LeaseTTL)
		if until := time.Now().Add(maxLease); until.Before(fence.ExpiresAt) {
			fence.ExpiresAt = until
		}
		spec, hash, err := runtimeJobSpec(row, resource.Name)
		if err != nil {
			return s.preflightFailure(work, row, lease, err)
		}
		now := time.Now().UTC()
		row.RuntimeOwner, row.RuntimeGeneration, row.RuntimeResource, row.RuntimeSHA256 = fence.Owner, fence.Generation, resource.Name, hash
		row.DispatchedAt, row.RemoteExpiresAt = &now, &fence.ExpiresAt
		if err := s.publish(work, row, lease, []string{"queued"}, map[string]any{"state": "dispatching", "dispatched_at": now, "runtime_owner": row.RuntimeOwner, "runtime_generation": row.RuntimeGeneration, "runtime_resource": row.RuntimeResource, "runtime_sha256": row.RuntimeSHA256, "remote_expires_at": fence.ExpiresAt}); err != nil {
			return err
		}
		var sent atomic.Int64
		var dispatch sync.Mutex
		sent.Store(fence.ExpiresAt.UnixNano())
		terminal := false
		defer func() {
			if !terminal {
				dispatch.Lock()
				defer dispatch.Unlock()
				s.quiesce(row, remote, &sent)
			}
		}()
		call, cancel = context.WithDeadline(work, fence.ExpiresAt)
		receipt, err := remote.Create(call, sandbox.WorkspaceJobCreate{Spec: spec, SHA256: hash, Fence: fence})
		cancel()
		if err != nil || validateRuntimeJob(row, receipt) != nil || receipt.ExpiresAt.After(fence.ExpiresAt) || !receipt.ExpiresAt.After(time.Now()) {
			_ = s.publish(work, row, lease, []string{"dispatching"}, map[string]any{"state": "unknown", "error": "dispatch_result_unknown"})
			return ErrWorkspaceJobUnknown
		}
		if workspaceJobTerminal(receipt.State) {
			terminal = true
			return s.observed(work, row, lease, receipt, []string{"dispatching"})
		}
		if receipt.State != "accepted" && receipt.State != "running" {
			_ = s.publish(work, row, lease, []string{"dispatching"}, map[string]any{"state": "unknown", "error": "runtime_result_unknown"})
			return ErrWorkspaceJobUnknown
		}
		if err := s.publish(work, row, lease, []string{"dispatching"}, map[string]any{"state": "running", "remote_expires_at": receipt.ExpiresAt}); err != nil {
			return err
		}
		ack := atomic.Int64{}
		ack.Store(receipt.ExpiresAt.UnixNano())
		watch, stop := context.WithCancelCause(work)
		defer stop(context.Canceled)
		watched := make(chan struct{})
		completed := make(chan sandbox.WorkspaceJobReceipt, 1)
		go func() {
			defer close(watched)
			stop(s.remoteWatch(watch, row, lease, remote, maxLease, &ack, &sent, &dispatch, completed))
		}()
		defer func() {
			stop(context.Canceled)
			<-watched
		}()
		var completion WorkspaceJobCompletion
		if processor := s.options.Processors[row.Kind]; processor != nil {
			completion, err = processor.Process(watch, row, remote, func(ctx context.Context) (sandbox.WorkspaceJobRenew, error) {
				return s.processorFence(ctx, row, lease, &ack)
			})
			if completion != nil {
				defer func() { _ = completion.Close() }()
			}
			if err != nil || completion == nil {
				_ = s.publish(work, row, lease, []string{"running"}, map[string]any{"state": "unknown", "error": "artifact_publication_unconfirmed"})
				return errors.Join(ErrWorkspaceJobUnknown, err)
			}
		}
		tick := time.NewTicker(s.options.PollInterval)
		defer tick.Stop()
		for {
			select {
			case <-watch.Done():
				_ = s.publish(work, row, lease, []string{"running"}, map[string]any{"state": "unknown", "error": "execution_lease_unconfirmed"})
				return ErrWorkspaceJobUnknown
			case receipt = <-completed:
				terminal = true
				return s.observedCompletion(work, row, lease, receipt, []string{"running"}, completion)
			case <-tick.C:
				call, cancel := context.WithDeadline(watch, time.Unix(0, ack.Load()))
				receipt, err := remote.Get(call, row.ID)
				cancel()
				if err != nil {
					if workspaceJobDefinitive(err) {
						_ = s.publish(work, row, lease, []string{"running"}, map[string]any{"state": "unknown", "error": "runtime_receipt_unavailable"})
						return ErrWorkspaceJobUnknown
					}
					continue
				}
				if validateRuntimeJob(row, receipt) != nil || receipt.State == "unknown" {
					_ = s.publish(work, row, lease, []string{"running"}, map[string]any{"state": "unknown", "error": "runtime_receipt_mismatch"})
					return ErrWorkspaceJobUnknown
				}
				if workspaceJobTerminal(receipt.State) {
					terminal = true
					return s.observedCompletion(work, row, lease, receipt, []string{"running"}, completion)
				}
			}
		}
	})
}

func (s *WorkspaceSnapshotJobs) preflightFailure(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease, cause error) error {
	err := s.publish(ctx, row, lease, []string{"queued"}, map[string]any{"state": "failed", "error": "job_preflight_failed"})
	return errors.Join(cause, err)
}

func (s *WorkspaceSnapshotJobs) observed(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease, receipt sandbox.WorkspaceJobReceipt, states []string) error {
	if validateRuntimeJob(row, receipt) != nil || !workspaceJobTerminal(receipt.State) || len(receipt.Result) > 64<<10 || (len(receipt.Result) > 0 && !json.Valid(receipt.Result)) {
		return ErrWorkspaceJobUnknown
	}
	state := receipt.State
	if state == "completed" {
		if s.options.Processors[row.Kind] != nil {
			return ErrWorkspaceJobUnknown // Artifact evidence is mandatory.
		}
		// This is a control-protocol execution receipt only. Phase B separately
		// validates archive+manifest and publishes an actual ready snapshot.
		state = "executed"
	}
	if receipt.State == "completed" && s.options.CompletedTx != nil {
		return s.publishCompletion(ctx, row, lease, states, map[string]any{"state": state, "result": string(receipt.Result), "error": receipt.Error}, workspaceJobMetadataCompletion{commit: func(ctx context.Context, tx *gorm.DB) error {
			return s.options.CompletedTx(ctx, tx, row, receipt)
		}})
	}
	return s.publish(ctx, row, lease, states, map[string]any{"state": state, "result": string(receipt.Result), "error": receipt.Error})
}

// Reconcile is explicitly requested by the user. It only reads the original
// immutable runtime receipt; acquiring a new SQL lease never recreates or
// renews the previous runtime operation.
func (s *WorkspaceSnapshotJobs) Reconcile(ctx context.Context, actor, id string) (models.WorkspaceSnapshotJob, error) {
	row, err := s.Get(ctx, actor, id)
	if err != nil || row.State == "executed" || row.State == "failed" || row.State == "cancelled" {
		return row, err
	}
	if row.State != "unknown" || row.DispatchedAt == nil || row.RuntimeSHA256 == "" {
		return row, ErrWorkspaceJobUnknown
	}
	_, lease, err := s.claim(ctx, id, true)
	if err != nil {
		return row, err
	}
	err = s.owned(ctx, lease, true, func(work context.Context, _ models.WorkspaceSnapshotJob, lease workspaceJobLease) error {
		remote, err := s.options.Remote(work, row.SandboxID)
		if err != nil {
			return err
		}
		call, cancel := context.WithTimeout(work, 5*time.Second)
		defer cancel()
		receipt, err := remote.Get(call, row.ID)
		if err != nil {
			return errors.Join(ErrWorkspaceJobUnknown, err)
		}
		if processor := s.options.Processors[row.Kind]; processor != nil && receipt.State == "completed" {
			if validateRuntimeJob(row, receipt) != nil {
				return ErrWorkspaceJobUnknown
			}
			completion, err := processor.Recover(work, row, receipt)
			if completion != nil {
				defer func() { _ = completion.Close() }()
			}
			if err != nil || completion == nil {
				return errors.Join(ErrWorkspaceJobUnknown, err)
			}
			return s.observedCompletion(work, row, lease, receipt, []string{"unknown"}, completion)
		}
		return s.observed(work, row, lease, receipt, []string{"unknown"})
	})
	if err != nil {
		return row, err
	}
	return s.Get(ctx, actor, id)
}
