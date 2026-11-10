package services

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

type workspaceJobRenewal struct {
	until time.Time
	err   error
}

// A lease may legitimately stay at its existing deadline: the enclosing job
// has a finite deadline, and an independent physical lease can advance more
// slowly than the control heartbeat. An unchanged acknowledgement never resets
// the hard expiry timer. Late replies cannot revive an expired worker.
func workspaceJobMaintain(ctx context.Context, initial time.Time, interval time.Duration, acknowledged *atomic.Int64, renew func(context.Context) (time.Time, error)) error {
	until := initial
	if interval <= 0 {
		interval = time.Second
	}
	query, stop := context.WithCancel(ctx)
	defer stop()
	expiry := time.NewTimer(max(time.Until(until), 0))
	defer expiry.Stop()
	next := time.NewTimer(interval)
	defer next.Stop()
	results := make(chan workspaceJobRenewal, 1)
	backoff := min(interval, 100*time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-expiry.C:
			return ErrWorkspaceJobOwner
		case <-next.C:
			if !until.After(time.Now()) {
				return ErrWorkspaceJobOwner
			}
			call, cancel := context.WithDeadline(query, until)
			go func() {
				defer cancel()
				deadline, err := renew(call)
				results <- workspaceJobRenewal{deadline, err}
			}()
		case result := <-results:
			if !until.After(time.Now()) || errors.Is(result.err, ErrWorkspaceJobOwner) {
				return ErrWorkspaceJobOwner
			}
			if result.err != nil {
				next.Reset(backoff)
				backoff = min(backoff*2, time.Second)
				continue
			}
			if result.until.Before(until) {
				return ErrWorkspaceJobOwner
			}
			if result.until.After(until) {
				until = result.until
				if acknowledged != nil {
					acknowledged.Store(until.UnixNano())
				}
				expiry.Reset(time.Until(until))
			}
			backoff = min(interval, 100*time.Millisecond)
			next.Reset(interval)
		}
	}
}

func workspaceJobDefinitive(err error) bool {
	var failure *sandbox.WorkspaceJobError
	return errors.As(err, &failure) && (failure.Status == http.StatusConflict || failure.Status == http.StatusNotFound || failure.Status == http.StatusUnauthorized || failure.Status == http.StatusForbidden || failure.Status == http.StatusNotImplemented || failure.Status == http.StatusBadRequest)
}

func workspaceJobTerminal(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled"
}

func (s *WorkspaceSnapshotJobs) remoteWatch(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease, remote WorkspaceJobRemote, maxLease time.Duration, acknowledged, sent *atomic.Int64, dispatch *sync.Mutex, terminal chan<- sandbox.WorkspaceJobReceipt) error {
	initial := time.Unix(0, acknowledged.Load())
	return workspaceJobMaintain(ctx, initial, min(s.options.RemoteInterval, maxLease/3), acknowledged, func(call context.Context) (time.Time, error) {
		previous := time.Unix(0, acknowledged.Load())
		fence, err := s.controlFence(call, row, lease)
		if err != nil {
			return time.Time{}, err
		}
		if fence.Generation != row.RuntimeGeneration || fence.Owner != row.RuntimeOwner {
			return time.Time{}, ErrWorkspaceJobOwner
		}
		if limit := time.Now().Add(maxLease); fence.ExpiresAt.After(limit) {
			fence.ExpiresAt = limit
		}
		if !fence.ExpiresAt.After(previous) {
			return previous, nil
		}
		dispatch.Lock()
		if call.Err() != nil {
			dispatch.Unlock()
			return time.Time{}, ErrWorkspaceJobOwner
		}
		for prior := sent.Load(); fence.ExpiresAt.UnixNano() > prior; prior = sent.Load() {
			if sent.CompareAndSwap(prior, fence.ExpiresAt.UnixNano()) {
				break
			}
		}
		dispatch.Unlock()
		receipt, err := remote.Renew(call, row.ID, sandbox.WorkspaceJobRenew{SHA256: row.RuntimeSHA256, Lineage: row.Lineage, Fence: fence})
		if err != nil {
			// Completion can race a heartbeat. Only the exact immutable receipt
			// can settle that race; a conflict alone is never success.
			if workspaceJobDefinitive(err) {
				observed, readErr := remote.Get(call, row.ID)
				if readErr == nil && validateRuntimeJob(row, observed) == nil && workspaceJobTerminal(observed.State) {
					select {
					case terminal <- observed:
					default:
					}
					return previous, nil
				}
				return time.Time{}, ErrWorkspaceJobOwner
			}
			return time.Time{}, err
		}
		if validateRuntimeJob(row, receipt) != nil || !receipt.ExpiresAt.After(time.Now()) || receipt.ExpiresAt.After(fence.ExpiresAt) || receipt.ExpiresAt.Before(previous) || receipt.State == "unknown" {
			return time.Time{}, ErrWorkspaceJobOwner
		}
		return receipt.ExpiresAt, nil
	})
}

// An ambiguous response may have reached the runtime. Preserve exclusivity
// until the last *sent* fence expires, or an exact terminal receipt proves that
// native work and its children have ended. Releasing SQL immediately on a lost
// HTTP response would otherwise admit a conflicting operation too early.
func (s *WorkspaceSnapshotJobs) quiesce(row models.WorkspaceSnapshotJob, remote WorkspaceJobRemote, sent *atomic.Int64) {
	until := time.Unix(0, sent.Load())
	if !until.After(time.Now()) {
		return
	}
	ctx, cancel := context.WithDeadline(context.Background(), until)
	defer cancel()
	req := sandbox.WorkspaceJobRenew{SHA256: row.RuntimeSHA256, Lineage: row.Lineage, Fence: sandbox.WorkspaceJobFence{Owner: row.RuntimeOwner, Generation: row.RuntimeGeneration, ExpiresAt: until}}
	call, stop := context.WithTimeout(ctx, min(time.Until(until), 5*time.Second))
	_, _ = remote.Cancel(call, row.ID, req)
	stop()
	ticker := time.NewTicker(min(s.options.PollInterval, 100*time.Millisecond))
	defer ticker.Stop()
	for {
		call, stop := context.WithTimeout(ctx, min(time.Until(until), 5*time.Second))
		observed, err := remote.Get(call, row.ID)
		stop()
		if err == nil && validateRuntimeJob(row, observed) == nil && workspaceJobTerminal(observed.State) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
