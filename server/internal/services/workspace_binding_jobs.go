package services

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func bindingJobKind(kind string) bool {
	return kind == "activate_workspace" || kind == "return_workspace"
}

// JobOptions decorates the strict business/lineage options, without replacing
// their authority, processors or hooks. It must be the outermost decorator so
// queue/parent/head locks precede the shared physical sandbox lock.
func (s *WorkspaceBindingService) JobOptions(options WorkspaceJobOptions) (WorkspaceJobOptions, error) {
	if options.Authorize == nil || options.PrepareJobAuthorization == nil {
		return options, ErrWorkspaceJobAuthority
	}
	validators := map[string]func(json.RawMessage) error{}
	for k, v := range options.Validate {
		validators[k] = v
	}
	processors := map[string]WorkspaceJobProcessor{}
	for k, v := range options.Processors {
		processors[k] = v
	}
	for _, kind := range []string{"activate_workspace", "return_workspace"} {
		previous := validators[kind]
		validators[kind] = func(body json.RawMessage) error {
			if err := validateBindingSpec(body); err != nil {
				return err
			}
			if previous != nil {
				return previous(body)
			}
			return nil
		}
		if processors[kind] != nil {
			return options, ErrWorkspaceJobConflict
		}
		processors[kind] = s
	}
	options.Validate, options.Processors = validators, processors
	prepare, authorize, admitted, completed, publication := options.PrepareJobAuthorization, options.Authorize, options.AdmittedTx, options.CompletedTx, options.GuardJobPublication
	readScope := options.ReadScope
	options.ReadScope = func(ctx context.Context, db *gorm.DB, actor, kind, id string) error {
		if err := s.options.ReadScope(ctx, db, actor, kind, id); err != nil {
			return err
		}
		if readScope != nil {
			return readScope(ctx, db, actor, kind, id)
		}
		return nil
	}
	if !s.jobHooks.CompareAndSwap(nil, &workspaceBindingJobHooks{completed: completed}) {
		return options, ErrWorkspaceJobConflict
	}
	options.PrepareJobAuthorization = func(ctx context.Context, job models.WorkspaceSnapshotJob) (context.Context, error) {
		if bindingJobKind(job.Kind) {
			var err error
			ctx, err = s.prepareBindingJob(ctx, job)
			if err != nil {
				return ctx, err
			}
		}
		return prepare(ctx, job)
	}
	options.Authorize = func(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob) error {
		if bindingJobKind(job.Kind) {
			if err := s.authorizeBindingJob(ctx, tx, job); err != nil {
				return err
			}
		}
		return authorize(ctx, tx, job)
	}
	options.AdmittedTx = func(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob) error {
		if bindingJobKind(job.Kind) {
			if err := s.admitBindingJob(ctx, tx, job); err != nil {
				return err
			}
		}
		if admitted != nil {
			return admitted(ctx, tx, job)
		}
		return nil
	}
	options.CompletedTx = func(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob, native sandbox.WorkspaceJobReceipt) error {
		if bindingJobKind(job.Kind) {
			return ErrWorkspaceJobUnknown
		}
		if completed != nil {
			return completed(ctx, tx, job, native)
		}
		return nil
	}
	options.GuardJobPublication = func(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob) error {
		if bindingJobKind(job.Kind) {
			if err := s.authorizeBindingJob(ctx, tx, job); err != nil {
				return err
			}
		}
		if publication != nil {
			return publication(ctx, tx, job)
		}
		return nil
	}
	return options, nil
}

func (s *WorkspaceBindingService) prepareBindingJob(ctx context.Context, job models.WorkspaceSnapshotJob) (context.Context, error) {
	if err := messageOutsideTransaction(s.db); err != nil {
		return ctx, err
	}
	op, err := bindingOperationMetadata(ctx, s.db, job)
	if err != nil {
		return ctx, err
	}
	if !bindingJobMatches(op, job) || op.Lineage != s.options.ExpectedLineage {
		return ctx, ErrWorkspaceJobConflict
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	expires, _ := check.Deadline()
	current, err := s.options.CurrentAuthorization(check, op.ActorID, "solo", op.SoloID, op.SandboxID)
	if err != nil {
		return ctx, err
	}
	if current != op.AuthorizationFingerprint {
		return ctx, ErrWorkspaceJobAuthority
	}
	proof := workspaceBindingAccepted{service: s, operationID: op.ID, jobIdentity: authorizationJobIdentity(job), expected: op.ExpectedBinding, expires: expires}
	ctx = context.WithValue(ctx, workspaceBindingAcceptedKey{}, proof)
	if _, err := s.acceptedBinding(checkWithBindingProof(check, proof), s.db, op.ActorID, "solo", op.SoloID, op.SandboxID, proof); err != nil {
		return ctx, err
	}
	return ctx, nil
}
func checkWithBindingProof(ctx context.Context, proof workspaceBindingAccepted) context.Context {
	return context.WithValue(ctx, workspaceBindingAcceptedKey{}, proof)
}

func (s *WorkspaceBindingService) authorizeBindingJob(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob) error {
	proof, ok := ctx.Value(workspaceBindingAcceptedKey{}).(workspaceBindingAccepted)
	if !ok || proof.service != s || proof.jobIdentity != authorizationJobIdentity(job) || !proof.expires.After(time.Now()) {
		return ErrWorkspaceJobAuthority
	}
	operations := s.operations.Load()
	if operations == nil || operations.solo == nil || operations.solo.durable == nil {
		return ErrWorkspaceJobAuthority
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
		if err := operations.solo.durable.GuardIdle(tx, soloQueueScope(job.ScopeID)); err != nil {
			if errors.Is(err, chatsession.ErrQueueBusy) {
				return soloErr(409, "workspace_consumers_busy", "请等待当前对话和队列完成，再切换工作区")
			}
			return err
		}
	}
	if _, err := s.acceptedBinding(ctx, tx, job.ActorID, job.ScopeKind, job.ScopeID, job.SandboxID, proof); err != nil {
		return err
	}
	if err := s.consumerIdleTx(ctx, tx, job.SandboxID); err != nil {
		return err
	}
	// Unknown remote jobs retain their binding until their original receipt is
	// confirmed. Starting a switch cannot invalidate another job's renewals.
	var other int64
	if err := workspaceCurrentRead(tx.WithContext(ctx)).Table("workspace_snapshot_jobs").Where("sandbox_id = ? AND id <> ? AND state NOT IN ?", job.SandboxID, job.ID, []string{"executed", "failed", "cancelled"}).Count(&other).Error; err != nil {
		return err
	}
	if other != 0 {
		return soloErr(409, "workspace_jobs_busy", "请先完成或核对当前环境的文件操作")
	}
	op, err := bindingOperationMetadata(ctx, tx, job)
	if err != nil {
		return err
	}
	identity, err := s.bindingSourcesIdentity(ctx, tx, op)
	if err != nil {
		return err
	}
	if identity != op.SourceIdentity {
		return ErrWorkspaceJobConflict
	}
	// Resource current reads follow all source/head locks. Checking this here
	// also prevents a corrupt cached fingerprint from authorizing dispatch.
	parent, err := s.parent(ctx, tx, op.ActorID, "solo", op.SoloID, op.SandboxID)
	if err != nil {
		return err
	}
	resource, err := bindingResourceRead(tx, op.SandboxID)
	if err != nil {
		return err
	}
	var head models.WorkspaceBindingHead
	if err := workspaceCurrentRead(tx).Take(&head, "sandbox_id = ?", op.SandboxID).Error; err != nil {
		return err
	}
	var native sandbox.WorkspaceBindingHead
	if json.Unmarshal([]byte(head.NativeHead), &native) != nil || !validBindingResource(resource, parent) || head.Fingerprint != bindingAggregate(parent, resource, native, op.AuthorizationFingerprint) {
		return ErrWorkspaceJobConflict
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
		return s.options.GuardTx(ctx, tx, "solo", op.SoloID, op.SandboxID)
	}
	return nil
}

func (s *WorkspaceBindingService) admitBindingJob(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceJobInvalid
	}
	proof, ok := ctx.Value(workspaceBindingAdmissionKey{}).(workspaceBindingAdmission)
	if !ok || proof.service != s || !bindingJobMatches(proof.row, job) || proof.row.SourceIdentity == "" {
		return ErrWorkspaceJobInvalid
	}
	var prior models.WorkspaceBindingOperation
	err := workspaceCurrentRead(tx).Take(&prior, "id = ?", job.ID).Error
	if err == nil {
		if prior.Fingerprint != proof.row.Fingerprint || !bindingJobMatches(prior, job) {
			return ErrWorkspaceJobConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row := proof.row
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	updated := tx.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ? AND binding_id = ? AND fingerprint = ? AND pending_operation_id = '' AND state = 'ready' AND lifecycle_epoch = ? AND lifecycle_attempt = ?", row.SandboxID, row.PreviousBindingID, row.ExpectedBinding, row.LifecycleEpoch, row.LifecycleAttempt).Updates(map[string]any{"state": "pending", "pending_operation_id": row.ID})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	return nil
}
