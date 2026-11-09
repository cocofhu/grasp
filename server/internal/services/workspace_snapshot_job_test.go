package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type jobAuthority struct{ changed atomic.Bool }

func (a *jobAuthority) Current(context.Context) (string, error) {
	if a.changed.Load() {
		return "lineage-restored", nil
	}
	return "lineage-original", nil
}
func (a *jobAuthority) Verify(_ context.Context, lineage string) error {
	if lineage != "lineage-original" || a.changed.Load() {
		return ErrWorkspaceJobAuthority
	}
	return nil
}

type jobRemote struct {
	mu          sync.Mutex
	db          *gorm.DB
	id          uint
	duration    time.Duration
	created     time.Time
	creates     int
	renews      int
	gets        int
	cancels     int
	transient   int
	ambiguous   bool
	unsupported bool
	started     chan struct{}
	receipt     sandbox.WorkspaceJobReceipt
}

func (r *jobRemote) Capabilities(context.Context) (sandbox.WorkspaceJobCapabilities, error) {
	kinds := []string{"control_probe"}
	if r.unsupported {
		kinds = nil
	}
	return sandbox.WorkspaceJobCapabilities{Version: 1, Resource: "resource-one", Lineage: "lineage-original", Kinds: kinds, RenewableLease: true, PersistentGeneration: true, MaxLeaseMillis: 1000, MaxDurationMillis: 10000}, nil
}
func (r *jobRemote) Create(ctx context.Context, req sandbox.WorkspaceJobCreate) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var job models.WorkspaceSnapshotJob
	if err := r.db.WithContext(ctx).First(&job, "id = ?", req.Spec.ID).Error; err != nil {
		return sandbox.WorkspaceJobReceipt{}, err
	}
	if job.State != "dispatching" || job.DispatchedAt == nil || job.RuntimeSHA256 != req.SHA256 || job.RuntimeOwner != req.Fence.Owner {
		return sandbox.WorkspaceJobReceipt{}, errors.New("provider dispatch preceded the durable marker")
	}
	var resource models.Sandbox
	if err := r.db.WithContext(ctx).First(&resource, r.id).Error; err != nil {
		return sandbox.WorkspaceJobReceipt{}, err
	}
	if resource.OperationOwner == "" || resource.Generation != req.Fence.Generation || resource.OperationExpiresAt == nil || req.Fence.ExpiresAt.After(*resource.OperationExpiresAt) {
		return sandbox.WorkspaceJobReceipt{}, errors.New("provider dispatch lacked physical fencing")
	}
	r.created, r.creates = time.Now(), r.creates+1
	r.receipt = sandbox.WorkspaceJobReceipt{Version: 1, ID: req.Spec.ID, Kind: req.Spec.Kind, Resource: req.Spec.Resource, Lineage: req.Spec.Lineage, SHA256: req.SHA256, Generation: req.Fence.Generation, ExpiresAt: req.Fence.ExpiresAt, Deadline: req.Spec.Deadline, State: "running", CreatedAt: time.Now()}
	if r.started != nil {
		close(r.started)
		r.started = nil
	}
	if r.ambiguous {
		r.receipt.State, r.receipt.Result = "completed", json.RawMessage(`{"control":true}`)
		return sandbox.WorkspaceJobReceipt{}, io.ErrUnexpectedEOF
	}
	return r.receipt, nil
}
func (r *jobRemote) current() sandbox.WorkspaceJobReceipt {
	if r.receipt.State == "running" && time.Since(r.created) >= r.duration {
		r.receipt.State, r.receipt.Result = "completed", json.RawMessage(`{"control":true}`)
	}
	return r.receipt
}
func (r *jobRemote) Get(context.Context, string) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gets++
	return r.current(), nil
}
func (r *jobRemote) Renew(ctx context.Context, _ string, req sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renews++
	if err := ctx.Err(); err != nil {
		return sandbox.WorkspaceJobReceipt{}, err
	}
	if r.transient > 0 {
		r.transient--
		return sandbox.WorkspaceJobReceipt{}, io.ErrUnexpectedEOF
	}
	if r.current().State != "running" {
		return sandbox.WorkspaceJobReceipt{}, &sandbox.WorkspaceJobError{Status: 409, Code: "workspace_job_owner_lost"}
	}
	if req.Fence.Generation != r.receipt.Generation || !r.receipt.ExpiresAt.After(time.Now()) {
		return sandbox.WorkspaceJobReceipt{}, &sandbox.WorkspaceJobError{Status: 409, Code: "workspace_job_owner_lost"}
	}
	r.receipt.ExpiresAt = req.Fence.ExpiresAt
	return r.receipt, nil
}
func (r *jobRemote) Cancel(context.Context, string, sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels++
	if r.receipt.State == "running" {
		r.receipt.State = "cancelled"
	}
	return r.receipt, nil
}

func snapshotJobs(t *testing.T) (*WorkspaceSnapshotJobs, *jobRemote, *jobAuthority, WorkspaceJobInput) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.WorkspaceSnapshotJob{}, &models.WorkspaceSnapshot{}, &models.WorkspaceRestore{}); err != nil {
		t.Fatal(err)
	}
	sbx := newSandboxService(t, db, &dockerState{})
	resource := models.Sandbox{Name: "resource-one", Status: "running", Purpose: "solo", ThreadID: "solo-original"}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	authority := &jobAuthority{}
	remote := &jobRemote{db: db, id: resource.ID, duration: 650 * time.Millisecond}
	service := NewWorkspaceSnapshotJobs(db, sbx, WorkspaceJobOptions{
		Authority: authority,
		Authorize: func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
			if row.ActorID != "alice" || row.ScopeID != "solo-original" || row.SandboxID != resource.ID {
				return errors.New("source is not accessible")
			}
			var source models.Sandbox
			return tx.WithContext(ctx).First(&source, "id = ? AND thread_id = ?", row.SandboxID, row.ScopeID).Error
		},
		Validate: map[string]func(json.RawMessage) error{"control_probe": func(body json.RawMessage) error { return nil }},
		Remote:   func(context.Context, uint) (WorkspaceJobRemote, error) { return remote, nil },
		LeaseTTL: 300 * time.Millisecond, RenewInterval: 75 * time.Millisecond, RemoteInterval: 40 * time.Millisecond, PollInterval: 15 * time.Millisecond, MaxDuration: 10 * time.Second,
	})
	input := WorkspaceJobInput{ClientRequestID: "request-original", ScopeKind: "solo", ScopeID: "solo-original", SandboxID: resource.ID, Kind: "control_probe", Body: json.RawMessage(`{"value":1}`), Duration: 3 * time.Second}
	return service, remote, authority, input
}

func TestWorkspaceSnapshotJobAdmissionIsIdempotentAuthorizedAndFinite(t *testing.T) {
	s, _, _, input := snapshotJobs(t)
	row, err := s.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Admit(t.Context(), "alice", input)
	if err != nil || row.ID != retry.ID || !row.Deadline.Equal(retry.Deadline) {
		t.Fatal("retry reset immutable job deadline", err)
	}
	input.Body = json.RawMessage(`{"value":2}`)
	if _, err := s.Admit(t.Context(), "alice", input); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("nonce accepted a different frozen spec", err)
	}
	if _, err := s.Admit(t.Context(), "bob", input); err == nil {
		t.Fatal("unrelated actor admitted a source operation")
	}
	if _, err := s.Get(t.Context(), "bob", row.ID); err == nil {
		t.Fatal("cross-owner receipt was exposed")
	}
	input.Duration = time.Hour
	if _, err := s.Admit(t.Context(), "alice", input); err == nil {
		t.Fatal("unbounded job admitted")
	}
	closed := NewWorkspaceSnapshotJobs(s.db, s.sandbox, WorkspaceJobOptions{})
	input.Duration = time.Second
	if _, err := closed.Admit(t.Context(), "alice", input); !errors.Is(err, ErrWorkspaceJobUnsupported) {
		t.Fatal("no collector/validator must remain unavailable", err)
	}
}

func TestWorkspaceSnapshotJobWorkerOutlivesRequestAndRenewsTransientControlFailure(t *testing.T) {
	s, remote, _, input := snapshotJobs(t)
	remote.transient = 2
	request, cancel := context.WithCancel(t.Context())
	row, err := s.Admit(request, "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // browser disconnect must not cancel an accepted durable job
	app, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(app) }()
	for until := time.Now().Add(4 * time.Second); time.Now().Before(until); {
		got, err := s.Get(t.Context(), "alice", row.ID)
		if err == nil && got.State == "executed" {
			stop()
			<-done
			got, err = s.Get(t.Context(), "alice", row.ID)
			if err != nil {
				t.Fatal(err)
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if remote.creates != 1 || remote.renews < 4 || got.DispatchedAt == nil || got.Owner != "" || got.ExpiresAt != nil {
				t.Fatalf("worker replayed or lost renewals: creates=%d renews=%d row=%+v", remote.creates, remote.renews, got)
			}
			for _, model := range []any{&models.WorkspaceSnapshot{}, &models.WorkspaceRestore{}} {
				var count int64
				if err := s.db.Model(model).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("control receipt fabricated a ready file snapshot/restore", count, err)
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	got, _ := s.Get(t.Context(), "alice", row.ID)
	t.Fatalf("independent worker did not complete: state=%s error=%s", got.State, got.Error)
}

func TestWorkspaceSnapshotJobAmbiguousDispatchNeverReplaysAndExplicitReconcileOnlyReads(t *testing.T) {
	s, remote, _, input := snapshotJobs(t)
	remote.ambiguous = true
	row, err := s.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunOne(t.Context(), row.ID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal(err)
	}
	if err := s.RunOne(t.Context(), row.ID); !errors.Is(err, ErrWorkspaceJobOwner) {
		t.Fatal("unknown intent became automatically eligible", err)
	}
	remote.mu.Lock()
	creates, renews, cancels := remote.creates, remote.renews, remote.cancels
	remote.mu.Unlock()
	got, err := s.Reconcile(t.Context(), "alice", row.ID)
	if err != nil || got.State != "executed" || got.RuntimeGeneration == got.Generation {
		t.Fatalf("read-only original receipt not reconciled: %+v %v", got, err)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.creates != creates || remote.renews != renews || remote.cancels != cancels || creates != 1 {
		t.Fatal("reconciliation replayed a provider mutation")
	}
}

func TestWorkspaceSnapshotJobRestoredSQLDoesNotAcquireRetainedRuntime(t *testing.T) {
	s, remote, authority, input := snapshotJobs(t)
	row, err := s.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	authority.changed.Store(true)
	if err := s.RunOne(t.Context(), row.ID); !errors.Is(err, ErrWorkspaceJobAuthority) {
		t.Fatal("restored lineage acquired an old queued job", err)
	}
	if remote.creates != 0 {
		t.Fatal("authority mismatch still dispatched native work")
	}
	var stored models.WorkspaceSnapshotJob
	if err := s.db.First(&stored, "id = ?", row.ID).Error; err != nil || stored.Generation != 0 || stored.State != "queued" {
		t.Fatal("lineage mismatch mutated job ownership", err)
	}
}

func TestWorkspaceSnapshotJobOwnerLossCannotPublishOrClearSuccessor(t *testing.T) {
	s, remote, _, input := snapshotJobs(t)
	started := make(chan struct{})
	remote.started, remote.duration = started, 3*time.Second
	row, err := s.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.RunOne(t.Context(), row.ID) }()
	<-started
	if err := s.db.Model(&models.WorkspaceSnapshotJob{}).Where("id = ?", row.ID).Updates(map[string]any{"owner": "successor-owner", "generation": gorm.Expr("generation + 1"), "expires_at": time.Now().Add(time.Second)}).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("superseded worker reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("superseded worker did not stop")
	}
	var got models.WorkspaceSnapshotJob
	if err := s.db.First(&got, "id = ?", row.ID).Error; err != nil || got.Owner != "successor-owner" || got.State == "executed" || got.Result != "" {
		t.Fatalf("old worker published or released new owner: %+v %v", got, err)
	}
}

func TestWorkspaceSnapshotJobRestartOnlyExpiresDispatchedMarker(t *testing.T) {
	s, remote, _, input := snapshotJobs(t)
	row, err := s.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Second)
	if err := s.db.Model(&models.WorkspaceSnapshotJob{}).Where("id = ?", row.ID).Updates(map[string]any{"state": "dispatching", "dispatched_at": now, "expires_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx)
	got, err := s.Get(t.Context(), "alice", row.ID)
	if err != nil || got.State != "unknown" || remote.creates != 0 {
		t.Fatalf("restart repeated an ambiguous dispatch: %+v %v creates=%d", got, err, remote.creates)
	}
}

func TestWorkspaceSnapshotJobMaintainerRetriesWithoutRevivingExpiry(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(stringJSONJobBool(blocked), func(t *testing.T) {
			var calls atomic.Int32
			initial := time.Now().Add(200 * time.Millisecond)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := workspaceJobMaintain(ctx, initial, 20*time.Millisecond, nil, func(ctx context.Context) (time.Time, error) {
				calls.Add(1)
				if blocked {
					<-ctx.Done()
					return time.Now().Add(time.Second), nil // late success is forbidden
				}
				return initial, nil // unchanged acknowledgement cannot extend
			})
			if !errors.Is(err, ErrWorkspaceJobOwner) || time.Since(initial) > 150*time.Millisecond || calls.Load() == 0 {
				t.Fatalf("fence lived past known expiry: %v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestWorkspaceSnapshotJobTransientSQLReadDoesNotPretendOwnershipLost(t *testing.T) {
	s, _, _, input := snapshotJobs(t)
	row, err := s.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	_, lease, err := s.claim(t.Context(), row.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	err = s.owned(t.Context(), lease, false, func(work context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease) error {
		transient := errors.New("injected transient SQL read")
		var failures atomic.Int32
		if err := s.db.Callback().Query().Before("gorm:query").Register("workspace_job_transient_read", func(tx *gorm.DB) {
			if tx.Statement.Table == "workspace_snapshot_jobs" && failures.Add(1) == 1 {
				_ = tx.AddError(transient)
			}
		}); err != nil {
			return err
		}
		defer func() { _ = s.db.Callback().Query().Remove("workspace_job_transient_read") }()
		if _, err := s.controlFence(work, row, lease); !errors.Is(err, transient) || errors.Is(err, ErrWorkspaceJobOwner) {
			return errors.New("transient database error was incorrectly treated as definitive owner loss")
		}
		if _, err := s.controlFence(work, row, lease); err != nil {
			return err
		}
		if err := s.db.Model(&models.WorkspaceSnapshotJob{}).Where("id = ?", row.ID).Update("owner", "different-owner").Error; err != nil {
			return err
		}
		if _, err := s.controlFence(work, row, lease); !errors.Is(err, ErrWorkspaceJobOwner) {
			return errors.New("actual ownership loss was incorrectly retried")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func stringJSONJobBool(v bool) string {
	if v {
		return "blocked_rpc"
	}
	return "unchanged_ack"
}
