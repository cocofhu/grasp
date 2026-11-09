package services

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

type workspaceBindingJobHooks struct {
	completed func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob, sandbox.WorkspaceJobReceipt) error
}
type workspaceBindingCompletion struct {
	service   *WorkspaceBindingService
	job       models.WorkspaceSnapshotJob
	operation models.WorkspaceBindingOperation
	result    sandbox.WorkspaceBindingResult
	native    sandbox.WorkspaceJobReceipt
	path      string
	validated bool
}

// Process observes the switch dispatched by the shared job worker. It never
// sends a second create, switches a directory, or replays a native operation.
func (s *WorkspaceBindingService) Process(ctx context.Context, job models.WorkspaceSnapshotJob, remote WorkspaceJobRemote, _ func(context.Context) (sandbox.WorkspaceJobRenew, error)) (WorkspaceJobCompletion, error) {
	binding, ok := remote.(WorkspaceBindingRemote)
	if !ok {
		return nil, ErrWorkspaceJobUnsupported
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		receipt, err := binding.WorkspaceBindingOperation(ctx, job.ID)
		if err == nil {
			if receipt.State == "committed" {
				return s.bindingCompletion(ctx, job, binding, receipt)
			}
			if receipt.State == "unknown" {
				return nil, ErrWorkspaceJobUnknown
			}
		} else {
			var unavailable *sandbox.WorkspaceJobError
			if !errors.As(err, &unavailable) || unavailable.Status != 404 {
				return nil, errors.Join(ErrWorkspaceJobUnknown, err)
			}
		}
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-tick.C:
		}
	}
}

func (s *WorkspaceBindingService) Recover(ctx context.Context, job models.WorkspaceSnapshotJob, native sandbox.WorkspaceJobReceipt) (WorkspaceJobCompletion, error) {
	remote, err := s.options.Remote(ctx, job.SandboxID)
	if err != nil {
		return nil, err
	}
	receipt, err := remote.WorkspaceBindingOperation(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	completion, err := s.bindingCompletion(ctx, job, remote, receipt)
	if err != nil {
		return nil, err
	}
	if err := completion.ValidateReceipt(native); err != nil {
		return nil, err
	}
	return completion, nil
}

// RecoverReceipt is invoked only by the user's explicit read-only reconcile.
// A separately fsynced committed head can prove the original switch after the
// generic runtime journal became unknown. That generic journal is not edited.
func (s *WorkspaceBindingService) RecoverReceipt(ctx context.Context, job models.WorkspaceSnapshotJob, remote WorkspaceJobRemote, original sandbox.WorkspaceJobReceipt) (sandbox.WorkspaceJobReceipt, error) {
	if validateRuntimeJob(job, original) != nil || original.State != "unknown" {
		return sandbox.WorkspaceJobReceipt{}, ErrWorkspaceJobUnknown
	}
	binding, ok := remote.(WorkspaceBindingRemote)
	if !ok {
		return sandbox.WorkspaceJobReceipt{}, ErrWorkspaceJobUnsupported
	}
	receipt, err := binding.WorkspaceBindingOperation(ctx, job.ID)
	if err != nil {
		return sandbox.WorkspaceJobReceipt{}, err
	}
	completion, err := s.bindingCompletion(ctx, job, binding, receipt)
	if err != nil {
		return sandbox.WorkspaceJobReceipt{}, err
	}
	return completion.native, nil
}

func (s *WorkspaceBindingService) bindingCompletion(ctx context.Context, job models.WorkspaceSnapshotJob, remote WorkspaceBindingRemote, receipt sandbox.WorkspaceBindingOperation) (*workspaceBindingCompletion, error) {
	if receipt.Version != 1 || receipt.State != "committed" || receipt.Result == nil || receipt.CompletedAt == nil {
		return nil, ErrWorkspaceJobUnknown
	}
	identity := receipt.Identity
	native := sandbox.WorkspaceJobReceipt{Version: 1, ID: identity.ID, Kind: identity.Kind, Resource: identity.Resource, Lineage: identity.Lineage, SHA256: identity.SHA256, Generation: identity.Generation, Deadline: identity.Deadline, State: "completed"}
	if validateRuntimeJob(job, native) != nil {
		return nil, ErrWorkspaceJobConflict
	}
	op, err := bindingOperationMetadata(ctx, s.db, job)
	if err != nil {
		return nil, err
	}
	var spec sandbox.WorkspaceBindingSpec
	if validateBindingSpec([]byte(op.NativeSpec)) != nil || workspacefiles.Decode([]byte(op.NativeSpec), &spec) != nil {
		return nil, ErrWorkspaceJobConflict
	}
	var previous models.WorkspaceBinding
	if err := s.db.WithContext(ctx).Take(&previous, "id = ? AND sandbox_id = ?", op.PreviousBindingID, op.SandboxID).Error; err != nil {
		return nil, err
	}
	var old sandbox.WorkspaceBindingHead
	if workspacefiles.Decode([]byte(previous.NativeHead), &old) != nil || old.Reference != spec.Expected {
		return nil, ErrWorkspaceJobConflict
	}
	result := *receipt.Result
	consumers := slices.Clone(result.Consumers)
	slices.Sort(consumers)
	if result.Version != 1 || result.OperationID != op.ID || result.Previous != old || result.ReturnSnapshotID != op.ReturnSnapshotID || result.ProcessMemory || !slices.Equal(consumers, []string{"agent", "capture", "code"}) || !validNativeBinding(result.Active, op.Resource, op.Lineage) || result.Active.Reference.ID != spec.BindingID || result.Active.Reference.Revision != old.Reference.Revision+1 || result.Active.ParentID != old.Reference.ID {
		return nil, ErrWorkspaceJobConflict
	}
	expectedTarget := spec.Target
	if op.Action == "return" {
		var target models.WorkspaceBinding
		if err := s.db.WithContext(ctx).Take(&target, "id = ? AND actor_id = ? AND solo_id = ? AND sandbox_id = ?", op.TargetBindingID, op.ActorID, op.SoloID, op.SandboxID).Error; err != nil {
			return nil, err
		}
		if target.State == "released" || target.NativeID != spec.Target.ID {
			return nil, ErrWorkspaceJobConflict
		}
		expectedTarget = sandbox.WorkspaceBindingSelector{Kind: target.SelectorKind, ID: target.SelectorID}
	}
	if result.Active.Target != expectedTarget {
		return nil, ErrWorkspaceJobConflict
	}
	caps, err := remote.WorkspaceBindingCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	if caps.Version != 1 || caps.Resource != op.Resource || caps.Lineage != op.Lineage || !caps.Activate || !caps.Return || !caps.DurableHead || !caps.PortOwnership || caps.ProcessMemory {
		return nil, ErrWorkspaceJobUnsupported
	}
	view, err := remote.WorkspaceBindingHead(ctx)
	if err != nil {
		return nil, err
	}
	if !view.Ready || view.PendingOperationID != "" || view.Head != result.Active || !filepath.IsAbs(view.Path) || strings.ContainsRune(view.Path, 0) {
		return nil, ErrWorkspaceJobUnknown
	}
	resultJSON, _ := json.Marshal(result)
	native.Result = resultJSON
	return &workspaceBindingCompletion{service: s, job: job, operation: op, result: result, native: native, path: view.Path}, nil
}

func (c *workspaceBindingCompletion) ValidateReceipt(native sandbox.WorkspaceJobReceipt) error {
	c.validated = false
	var result sandbox.WorkspaceBindingResult
	if validateRuntimeJob(c.job, native) != nil || native.State != "completed" || workspacefiles.Decode(native.Result, &result) != nil || bindingDigest(result) != bindingDigest(c.result) {
		return ErrWorkspaceJobUnknown
	}
	c.validated = true
	return nil
}
func (c *workspaceBindingCompletion) PrepareTx(ctx context.Context, tx *gorm.DB) error {
	if !c.validated {
		return ErrWorkspaceJobUnknown
	}
	return c.service.authorizeBindingJob(ctx, tx, c.job)
}
func (c *workspaceBindingCompletion) Commit(ctx context.Context, tx *gorm.DB) error {
	if !c.validated {
		return ErrWorkspaceJobUnknown
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceJobInvalid
	}
	s, op := c.service, c.operation
	// PrepareTx already locked queue→parent→head→source metadata before SB.
	// All operations below are comparisons and SQL writes under those locks.
	var head models.WorkspaceBindingHead
	if err := tx.Take(&head, "sandbox_id = ? AND pending_operation_id = ? AND state = 'pending'", op.SandboxID, op.ID).Error; err != nil {
		return err
	}
	if head.BindingID != op.PreviousBindingID || head.Fingerprint != op.ExpectedBinding {
		return ErrWorkspaceJobConflict
	}
	parent, err := s.parent(ctx, tx, op.ActorID, "solo", op.SoloID, op.SandboxID)
	if err != nil {
		return err
	}
	resource, err := bindingResourceRead(tx, op.SandboxID)
	if err != nil {
		return err
	}
	if !validBindingResource(resource, parent) {
		return ErrWorkspaceJobConflict
	}
	if hooks := s.jobHooks.Load(); hooks != nil && hooks.completed != nil {
		if err := hooks.completed(ctx, tx, c.job, c.native); err != nil {
			return err
		}
	}
	nativeJSON, _ := json.Marshal(c.result.Active)
	var sourceID, restoreID string
	if op.Action == "activate" {
		var restored models.WorkspaceRestore
		if err := tx.Select("id", "snapshot_id").Take(&restored, "id = ?", op.RestoreID).Error; err != nil {
			return err
		}
		sourceID, restoreID = restored.SnapshotID, restored.ID
	} else {
		var target models.WorkspaceBinding
		if err := tx.Take(&target, "id = ?", op.TargetBindingID).Error; err != nil {
			return err
		}
		sourceID, restoreID = target.SnapshotID, target.RestoreID
	}
	old := tx.Model(&models.WorkspaceBinding{}).Where("id = ? AND actor_id = ? AND solo_id = ? AND sandbox_id = ? AND state = 'active'", op.PreviousBindingID, op.ActorID, op.SoloID, op.SandboxID).Updates(map[string]any{"state": "retained", "snapshot_id": op.ReturnSnapshotID})
	if old.Error != nil {
		return old.Error
	}
	if old.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	next := models.WorkspaceBinding{ID: op.BindingID, ActorID: op.ActorID, SoloID: op.SoloID, SandboxID: op.SandboxID, Resource: op.Resource, Lineage: op.Lineage, NativeID: c.result.Active.Reference.ID, Revision: c.result.Active.Reference.Revision, NativeFingerprint: c.result.Active.Reference.Fingerprint, NativeHead: string(nativeJSON), SelectorKind: c.result.Active.Target.Kind, SelectorID: c.result.Active.Target.ID, ParentBindingID: op.PreviousBindingID, RestoreID: restoreID, SnapshotID: sourceID, State: "active"}
	if err := tx.Create(&next).Error; err != nil {
		return err
	}
	point := models.WorkspaceReturnPoint{ID: op.ID, ActorID: op.ActorID, SoloID: op.SoloID, SandboxID: op.SandboxID, OperationID: op.ID, PreviousBindingID: op.PreviousBindingID, LeavingBindingID: next.ID, SnapshotID: op.ReturnSnapshotID, State: "ready"}
	if err := tx.Create(&point).Error; err != nil {
		return err
	}
	advanced := tx.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ? AND fingerprint = ? AND pending_operation_id = ? AND state = 'pending'", op.SandboxID, op.ExpectedBinding, op.ID).Updates(map[string]any{"binding_id": next.ID, "native_id": next.NativeID, "revision": next.Revision, "native_fingerprint": next.NativeFingerprint, "native_head": next.NativeHead, "fingerprint": bindingAggregate(parent, resource, c.result.Active, op.AuthorizationFingerprint), "pending_operation_id": "", "last_operation_id": op.ID, "state": "ready"})
	if advanced.Error != nil {
		return advanced.Error
	}
	if advanced.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	state := "activated"
	if op.Action == "return" {
		state = "returned"
	}
	updated := tx.Model(&models.WorkspaceBindingOperation{}).Where("id = ? AND fingerprint = ? AND source_identity = ? AND state = 'queued'", op.ID, op.Fingerprint, op.SourceIdentity).Updates(map[string]any{"state": state, "result": string(c.native.Result), "error": ""})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	parentUpdated := tx.Model(&models.SoloSession{}).Where("id = ? AND user_id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = '' AND status = ?", op.SoloID, op.ActorID, op.SandboxID, op.LifecycleEpoch, op.LifecycleAttempt, models.SoloStatusRunning).Update("conversation_context_pending", true)
	if parentUpdated.Error != nil {
		return parentUpdated.Error
	}
	if parentUpdated.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	return nil
}

// Published is called only after the outer SQL transaction actually commits.
func (c *workspaceBindingCompletion) Published(context.Context) error {
	s := c.service.sandbox
	s.mu.Lock()
	live := s.live[c.operation.SandboxID]
	var old *sandbox.ACPClient
	if live != nil {
		old = live.acp
		live.acp = nil
		if live.sb != nil {
			live.sb.SetActiveWorkspace(c.path, c.result.Active.Reference.Fingerprint)
		}
	}
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}
func (c *workspaceBindingCompletion) Close() error { return nil }
