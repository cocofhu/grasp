package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

type WorkspaceBindingInput struct {
	ClientRequestID  string        `json:"clientRequestId"`
	SoloID           string        `json:"soloId"`
	Action           string        `json:"action"`
	ExpectedBinding  string        `json:"expectedBinding"`
	RestoreID        string        `json:"restoreId,omitempty"`
	TargetBindingID  string        `json:"targetBindingId,omitempty"`
	ReturnSnapshotID string        `json:"returnSnapshotId"`
	Duration         time.Duration `json:"duration"`
}

// Startup attaches the same worker that handles capture and materialization.
// A separate worker would lose the single physical lease and publication fence.
type workspaceBindingOperations struct {
	restore        *WorkspaceRestoreService
	solo           *SoloService
	consumersReady bool
}

func (s *WorkspaceBindingService) AttachOperations(restore *WorkspaceRestoreService, solo *SoloService) error {
	if restore == nil || restore.Jobs == nil || solo == nil || solo.durable == nil || restore.Jobs.options.Processors["activate_workspace"] != s || restore.Jobs.options.Processors["return_workspace"] != s {
		return ErrWorkspaceJobAuthority
	}
	value := &workspaceBindingOperations{restore: restore, solo: solo, consumersReady: true}
	if !s.operations.CompareAndSwap(nil, value) {
		return ErrWorkspaceJobConflict
	}
	return nil
}
func (s *WorkspaceBindingService) operationsReady() bool {
	operations := s.operations.Load()
	return operations != nil && operations.consumersReady
}
func bindingInputFingerprint(in WorkspaceBindingInput) string {
	in.ClientRequestID = ""
	return bindingDigest(in)
}
func validateBindingSpec(body json.RawMessage) error {
	var spec sandbox.WorkspaceBindingSpec
	if workspacefiles.Decode(body, &spec) != nil || spec.Version != 1 || !sandbox.ValidWorkspaceBindingReference(spec.Expected) || !workspaceJobID.MatchString(spec.BindingID) || !workspaceJobID.MatchString(spec.ReturnSnapshotID) || !validDigest(spec.Authorization) || (spec.Target.Kind != "restore" && spec.Target.Kind != "binding") || !workspaceJobID.MatchString(spec.Target.ID) {
		return ErrWorkspaceJobInvalid
	}
	return nil
}

func (s *WorkspaceBindingService) Admit(ctx context.Context, actor string, in WorkspaceBindingInput) (result models.WorkspaceBindingOperation, resultErr error) {
	var zero models.WorkspaceBindingOperation
	if actor == "" || len(actor) > 191 || strings.ContainsRune(actor, 0) || !workspaceJobID.MatchString(in.ClientRequestID) || in.SoloID == "" || len(in.SoloID) > 191 || strings.ContainsRune(in.SoloID, 0) || !validDigest(in.ExpectedBinding) || !workspaceJobID.MatchString(in.ReturnSnapshotID) || bindingKind(in.Action) == "" || in.Duration <= 0 || in.Duration > 30*time.Minute {
		return zero, ErrWorkspaceJobInvalid
	}
	if in.Action == "activate" && (!workspaceJobID.MatchString(in.RestoreID) || in.TargetBindingID != "") || in.Action == "return" && (!workspaceJobID.MatchString(in.TargetBindingID) || in.RestoreID != "") {
		return zero, ErrWorkspaceJobInvalid
	}
	id, fp := workspaceRestoreIdentity(actor, in.SoloID, in.ClientRequestID), bindingInputFingerprint(in)
	prior, err := s.GetOperation(ctx, actor, in.SoloID, id)
	if err == nil {
		if prior.Fingerprint != fp {
			return zero, ErrWorkspaceJobConflict
		}
		return prior, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, err
	}
	// A concurrent identical caller may cross admission while this caller is
	// validating its now-stale head. Once accepted, the original receipt wins;
	// no new authorization, provider access or side effect is needed to read it.
	defer func() {
		if resultErr != nil {
			if accepted, lookup := s.GetOperation(ctx, actor, in.SoloID, id); lookup == nil {
				if accepted.Fingerprint == fp {
					result, resultErr = accepted, nil
				} else {
					result, resultErr = zero, ErrWorkspaceJobConflict
				}
			}
		}
	}()
	operations := s.operations.Load()
	if !s.options.FileOperationsEnabled || operations == nil || !operations.consumersReady {
		return zero, ErrWorkspaceJobUnsupported
	}
	ctx, err = s.options.Prepare(ctx)
	if err != nil {
		return zero, err
	}
	current, err := s.Resolve(ctx, actor, in.SoloID)
	if err != nil {
		return zero, err
	}
	if current.Fingerprint != in.ExpectedBinding {
		return zero, ErrWorkspaceJobConflict
	}
	var head models.WorkspaceBindingHead
	if err := s.db.WithContext(ctx).Take(&head, "sandbox_id = ?", current.SandboxID).Error; err != nil {
		return zero, err
	}
	if head.State != "ready" || head.PendingOperationID != "" || head.Fingerprint != current.Fingerprint {
		return zero, ErrWorkspaceJobConflict
	}
	spec := sandbox.WorkspaceBindingSpec{Version: 1, Expected: current.Reference, BindingID: bindingDigest(struct{ Operation, Kind string }{id, "root"}), ReturnSnapshotID: in.ReturnSnapshotID, Authorization: current.Authorization}
	row := models.WorkspaceBindingOperation{ID: id, ActorID: actor, SoloID: in.SoloID, SandboxID: current.SandboxID, ClientNonce: in.ClientRequestID, Fingerprint: fp, Action: in.Action, ExpectedBinding: in.ExpectedBinding, PreviousBindingID: head.BindingID, BindingID: bindingHistoryID(current.SandboxID, spec.BindingID), TargetBindingID: in.TargetBindingID, RestoreID: in.RestoreID, ReturnSnapshotID: in.ReturnSnapshotID, Resource: current.Resource, Lineage: current.Lineage, AuthorizationFingerprint: current.Authorization, LifecycleEpoch: current.LifecycleEpoch, LifecycleAttempt: current.LifecycleAttempt, JobID: id, State: "queued"}
	if in.Action == "activate" {
		restored, err := operations.restore.Get(ctx, actor, in.RestoreID)
		if err != nil {
			return zero, err
		}
		if restored.SoloID != in.SoloID || restored.TargetSandboxID != current.SandboxID || restored.Lineage != current.Lineage || restored.State != "restored" || restored.Destination != restored.JobID {
			return zero, ErrWorkspaceJobConflict
		}
		spec.Target = sandbox.WorkspaceBindingSelector{Kind: "restore", ID: restored.JobID}
	} else {
		var target models.WorkspaceBinding
		if err := s.db.WithContext(ctx).Take(&target, "id = ? AND actor_id = ? AND solo_id = ? AND sandbox_id = ?", in.TargetBindingID, actor, in.SoloID, current.SandboxID).Error; err != nil {
			return zero, err
		}
		if target.State != "retained" || target.Lineage != current.Lineage || target.ReleasedAt != nil {
			return zero, ErrWorkspaceJobConflict
		}
		var old sandbox.WorkspaceBindingHead
		if workspacefiles.Decode([]byte(head.NativeHead), &old) != nil || old.Target.Kind == target.SelectorKind && old.Target.ID == target.SelectorID {
			return zero, ErrWorkspaceJobConflict
		}
		spec.Target = sandbox.WorkspaceBindingSelector{Kind: "binding", ID: target.NativeID}
	}
	body, _ := json.Marshal(spec)
	canonical, err := canonicalWorkspaceJob(body)
	if err != nil {
		return zero, err
	}
	row.NativeSpec = string(canonical)
	ctx = context.WithValue(ctx, workspaceBindingAdmissionKey{}, workspaceBindingAdmission{s, row})
	identity, err := s.bindingSourcesIdentity(ctx, s.db.WithContext(ctx), row)
	if err != nil {
		return zero, err
	}
	row.SourceIdentity = identity
	ctx = context.WithValue(ctx, workspaceBindingAdmissionKey{}, workspaceBindingAdmission{s, row})
	_, err = operations.restore.Jobs.Admit(ctx, actor, WorkspaceJobInput{ClientRequestID: in.ClientRequestID, ScopeKind: "solo", ScopeID: in.SoloID, SandboxID: current.SandboxID, Kind: bindingKind(in.Action), Body: canonical, Duration: in.Duration})
	if err != nil {
		if accepted, lookup := s.GetOperation(ctx, actor, in.SoloID, id); lookup == nil && accepted.Fingerprint == fp {
			return accepted, nil
		}
		return zero, err
	}
	return s.GetOperation(ctx, actor, in.SoloID, id)
}

func (s *WorkspaceBindingService) Reconcile(ctx context.Context, actor, soloID, id string) (models.WorkspaceBindingOperation, error) {
	row, err := s.GetOperation(ctx, actor, soloID, id)
	if err != nil || row.State == "activated" || row.State == "returned" {
		return row, err
	}
	operations := s.operations.Load()
	if operations == nil {
		return row, ErrWorkspaceJobUnsupported
	}
	if row.State == "failed" || row.State == "cancelled" {
		if err := s.settleUnchangedBinding(ctx, row); err != nil {
			return row, err
		}
		return s.GetOperation(ctx, actor, soloID, id)
	}
	_, err = operations.restore.Jobs.Reconcile(ctx, actor, row.JobID)
	if err != nil {
		return row, err
	}
	return s.GetOperation(ctx, actor, soloID, id)
}

// Only explicit reconcile may remove a definitively unchanged barrier.
// An absent dispatch marker, or the original failed native receipt together
// with no native intent and the exact previous head, proves no switch occurred.
// Any unknown native intent or changed head keeps its original barrier.
func (s *WorkspaceBindingService) settleUnchangedBinding(ctx context.Context, op models.WorkspaceBindingOperation) error {
	ctx, err := s.options.Prepare(ctx)
	if err != nil {
		return err
	}
	var original models.WorkspaceSnapshotJob
	if err := s.db.WithContext(ctx).Take(&original, "id = ? AND actor_id = ?", op.JobID, op.ActorID).Error; err != nil {
		return err
	}
	var unchanged *sandbox.WorkspaceJobReceipt
	if original.DispatchedAt != nil {
		// A terminal native failure before the fsynced switch intent is also
		// definitive. Prove all three facts by GET: immutable final job, no
		// binding operation, and the exact unchanged native head. No replay.
		remote, err := s.options.Remote(ctx, op.SandboxID)
		if err != nil {
			return err
		}
		jobs, ok := remote.(WorkspaceJobRemote)
		if !ok {
			return ErrWorkspaceJobUnsupported
		}
		receipt, err := jobs.Get(ctx, op.ID)
		if err != nil {
			return err
		}
		if validateRuntimeJob(original, receipt) != nil || (receipt.State != "failed" && receipt.State != "cancelled") {
			return ErrWorkspaceJobUnknown
		}
		_, lookup := remote.WorkspaceBindingOperation(ctx, op.ID)
		var missing *sandbox.WorkspaceJobError
		if !errors.As(lookup, &missing) || missing.Status != 404 {
			return ErrWorkspaceJobUnknown
		}
		view, err := remote.WorkspaceBindingHead(ctx)
		if err != nil {
			return err
		}
		var spec sandbox.WorkspaceBindingSpec
		if workspacefiles.Decode([]byte(op.NativeSpec), &spec) != nil || !view.Ready || view.PendingOperationID != "" || !validNativeBinding(view.Head, op.Resource, op.Lineage) || view.Head.Reference != spec.Expected {
			return ErrWorkspaceJobUnknown
		}
		unchanged = &receipt
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		operations := s.operations.Load()
		if operations == nil || operations.solo == nil {
			return ErrWorkspaceJobAuthority
		}
		if err := operations.solo.durable.GuardIdle(tx, soloQueueScope(op.SoloID)); err != nil && !errors.Is(err, chatsession.ErrQueueRetired) {
			return err
		}
		if err := s.options.ReadScope(ctx, tx, op.ActorID, "solo", op.SoloID); err != nil {
			return err
		}
		var parent models.SoloSession
		if err := workspaceCurrentRead(tx).Select("id", "sandbox_id").Take(&parent, "id = ? AND user_id = ?", op.SoloID, op.ActorID).Error; err != nil {
			return err
		}
		if parent.SandboxID != op.SandboxID {
			return ErrWorkspaceJobConflict
		}
		var head models.WorkspaceBindingHead
		if err := workspaceCurrentRead(tx).Take(&head, "sandbox_id = ?", op.SandboxID).Error; err != nil {
			return err
		}
		if head.State == "ready" && head.PendingOperationID == "" && head.BindingID == op.PreviousBindingID {
			return nil
		}
		if head.State != "pending" || head.PendingOperationID != op.ID || head.BindingID != op.PreviousBindingID || head.Fingerprint != op.ExpectedBinding {
			return ErrWorkspaceJobConflict
		}
		resource, err := bindingResourceRead(tx, op.SandboxID)
		if err != nil {
			return err
		}
		if resource.Name != op.Resource {
			return ErrWorkspaceJobConflict
		}
		var job models.WorkspaceSnapshotJob
		if err := workspaceCurrentRead(tx).Take(&job, "id = ? AND actor_id = ?", op.JobID, op.ActorID).Error; err != nil {
			return err
		}
		if !bindingJobMatches(op, job) || (job.State != "failed" && job.State != "cancelled") || job.DispatchedAt != nil && (unchanged == nil || validateRuntimeJob(job, *unchanged) != nil || job.State != unchanged.State) {
			return ErrWorkspaceJobUnknown
		}
		if err := s.options.GuardTx(ctx, tx, "solo", op.SoloID, op.SandboxID); err != nil {
			return err
		}
		updated := tx.Model(&models.WorkspaceBindingOperation{}).Where("id = ? AND fingerprint = ? AND state = 'queued'", op.ID, op.Fingerprint).Updates(map[string]any{"state": job.State, "error": job.Error})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrWorkspaceJobConflict
		}
		cleared := tx.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ? AND pending_operation_id = ? AND binding_id = ? AND fingerprint = ? AND state = 'pending'", op.SandboxID, op.ID, op.PreviousBindingID, op.ExpectedBinding).Updates(map[string]any{"state": "ready", "pending_operation_id": ""})
		if cleared.Error != nil {
			return cleared.Error
		}
		if cleared.RowsAffected != 1 {
			return ErrWorkspaceJobConflict
		}
		return nil
	})
}

// acceptedBinding is available only under a private, short-lived proof for the
// original job. It permits that job to finish across its own head barrier; it
// never opens a file, wakes a guest, or grants a new request the previous root.
func (s *WorkspaceBindingService) acceptedBinding(ctx context.Context, db *gorm.DB, actor, kind, id string, sbid uint, proof workspaceBindingAccepted) (WorkspaceActiveBinding, error) {
	var zero WorkspaceActiveBinding
	if proof.service != s || !proof.expires.After(time.Now()) || kind != "solo" {
		return zero, ErrWorkspaceJobAuthority
	}
	var op models.WorkspaceBindingOperation
	if admission, ok := ctx.Value(workspaceBindingAdmissionKey{}).(workspaceBindingAdmission); ok && admission.service == s && admission.row.ID == proof.operationID {
		op = admission.row
	} else if err := workspaceCurrentRead(db.WithContext(ctx)).Take(&op, "id = ?", proof.operationID).Error; err != nil {
		return zero, err
	}
	if op.ActorID != actor || op.SoloID != id || op.SandboxID != sbid || op.ExpectedBinding != proof.expected {
		return zero, ErrWorkspaceJobConflict
	}
	parent, err := s.parent(ctx, db, actor, kind, id, sbid)
	if err != nil {
		return zero, err
	}
	if parent.Epoch != op.LifecycleEpoch || parent.Attempt != op.LifecycleAttempt {
		return zero, ErrWorkspaceJobConflict
	}
	var head models.WorkspaceBindingHead
	if err := workspaceCurrentRead(db).Take(&head, "sandbox_id = ?", sbid).Error; err != nil {
		return zero, err
	}
	original := head.BindingID == op.PreviousBindingID && head.Fingerprint == op.ExpectedBinding && (head.State == "ready" && head.PendingOperationID == "" || head.State == "pending" && head.PendingOperationID == op.ID)
	completed := head.State == "ready" && head.PendingOperationID == "" && head.LastOperationID == op.ID && head.BindingID == op.BindingID && (op.State == "activated" || op.State == "returned")
	if (!original && !completed) || head.AuthorizationFingerprint != op.AuthorizationFingerprint || head.LifecycleEpoch != op.LifecycleEpoch || head.LifecycleAttempt != op.LifecycleAttempt || head.Resource != op.Resource || head.Lineage != op.Lineage {
		return zero, ErrWorkspaceJobConflict
	}
	var spec sandbox.WorkspaceBindingSpec
	if workspacefiles.Decode([]byte(op.NativeSpec), &spec) != nil {
		return zero, ErrWorkspaceJobConflict
	}
	var native sandbox.WorkspaceBindingHead
	if workspacefiles.Decode([]byte(head.NativeHead), &native) != nil || !validNativeBinding(native, op.Resource, op.Lineage) || native.Reference.ID != head.NativeID || native.Reference.Revision != head.Revision || native.Reference.Fingerprint != head.NativeFingerprint || original && native.Reference != spec.Expected {
		return zero, ErrWorkspaceJobConflict
	}
	if completed {
		var result sandbox.WorkspaceBindingResult
		if workspacefiles.Decode([]byte(op.Result), &result) != nil || result.Active != native || result.OperationID != op.ID {
			return zero, ErrWorkspaceJobConflict
		}
	}
	return WorkspaceActiveBinding{SandboxID: sbid, Resource: op.Resource, Lineage: op.Lineage, Reference: spec.Expected, Fingerprint: op.ExpectedBinding, LifecycleEpoch: op.LifecycleEpoch, LifecycleAttempt: op.LifecycleAttempt, Authorization: op.AuthorizationFingerprint}, nil
}
