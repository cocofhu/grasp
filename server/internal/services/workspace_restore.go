package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

// WorkspaceRestoreBinding comes from the canonical active-binding resolver.
// When called in a transaction it must lock the parent/binding before Sandbox.
type WorkspaceRestoreBinding struct {
	SandboxID   uint
	Fingerprint string
}

type WorkspaceRestoreOptions struct {
	CurrentBinding    func(context.Context, *gorm.DB, string, string) (WorkspaceRestoreBinding, error)
	AuthorizeSnapshot func(context.Context, *gorm.DB, string, models.WorkspaceSnapshot) error
	// Prepare obtains an external execution proof outside SQL. GuardTx consumes
	// that private proof for Restore/Job/Solo/Sandbox metadata without I/O.
	Prepare func(context.Context) (context.Context, error)
	GuardTx func(context.Context, *gorm.DB, models.WorkspaceRestore) error
}

type WorkspaceRestoreInput struct {
	ClientRequestID string        `json:"clientRequestId"`
	SoloID          string        `json:"soloId"`
	SnapshotID      string        `json:"snapshotId"`
	ExpectedBinding string        `json:"expectedBinding"`
	Duration        time.Duration `json:"duration"`
}

// WorkspaceRestoreService composes one existing B pipeline/worker. Its only
// native side effect creates an exclusive new directory; activation is separate.
type WorkspaceRestoreService struct {
	Pipeline  *WorkspaceSnapshotPipeline
	Jobs      *WorkspaceSnapshotJobs
	db        *gorm.DB
	files     *filestore.Store
	holds     *payloadhold.Manager
	limits    payloadhold.Limits
	options   WorkspaceRestoreOptions
	baseAuth  func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob) error
	completed func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob, sandbox.WorkspaceJobReceipt) error
}

func NewWorkspaceRestoreService(db *gorm.DB, sbx *SandboxService, holds *payloadhold.Manager, files *filestore.Store, limits payloadhold.Limits, jobs WorkspaceJobOptions, options WorkspaceRestoreOptions) (*WorkspaceRestoreService, error) {
	if db == nil || sbx == nil || holds == nil || files == nil || jobs.Authority == nil || jobs.Authorize == nil || options.CurrentBinding == nil || options.AuthorizeSnapshot == nil || options.Prepare == nil || options.GuardTx == nil {
		return nil, ErrWorkspaceJobAuthority
	}
	if limits == (payloadhold.Limits{}) {
		limits = payloadhold.DefaultLimits()
	}
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return nil, ErrWorkspaceJobInvalid
	}
	s := &WorkspaceRestoreService{db: db, files: files, holds: holds, limits: limits, options: options, baseAuth: jobs.Authorize, completed: jobs.CompletedTx}
	validators := map[string]func(json.RawMessage) error{}
	for kind, fn := range jobs.Validate {
		validators[kind] = fn
	}
	previousValidator := validators["restore_files"]
	validators["restore_files"] = func(body json.RawMessage) error {
		if err := validateWorkspaceRestoreSpec(body); err != nil {
			return err
		}
		if previousValidator != nil {
			return previousValidator(body)
		}
		return nil
	}
	jobs.Validate = validators
	processors := map[string]WorkspaceJobProcessor{}
	for kind, processor := range jobs.Processors {
		processors[kind] = processor
	}
	processors["restore_files"] = s
	jobs.Processors = processors
	prepare := jobs.PrepareAuthorization
	jobs.PrepareAuthorization = func(ctx context.Context) (context.Context, error) {
		var err error
		if prepare != nil {
			ctx, err = prepare(ctx)
			if err != nil {
				return ctx, err
			}
		}
		return options.Prepare(ctx)
	}
	admitted, completed := jobs.AdmittedTx, jobs.CompletedTx
	jobs.Authorize = s.authorizeJob
	jobs.AdmittedTx = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if err := s.admitted(ctx, tx, row); err != nil {
			return err
		}
		if admitted != nil {
			return admitted(ctx, tx, row)
		}
		return nil
	}
	jobs.CompletedTx = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob, receipt sandbox.WorkspaceJobReceipt) error {
		if row.Kind == "restore_files" {
			return ErrWorkspaceJobUnknown // A private verified completion is mandatory.
		}
		if completed != nil {
			return completed(ctx, tx, row, receipt)
		}
		return nil
	}
	pipeline, err := NewWorkspaceSnapshotPipeline(db, sbx, holds, files, limits, jobs)
	if err != nil {
		return nil, err
	}
	s.Pipeline, s.Jobs = pipeline, pipeline.Jobs
	return s, nil
}

func workspaceRestoreIdentity(actor, solo, nonce string) string {
	digest := sha256.Sum256([]byte(actor + "\x00solo\x00" + solo + "\x00" + nonce))
	return hex.EncodeToString(digest[:])
}

func restoreInputFingerprint(in WorkspaceRestoreInput) string {
	in.ClientRequestID = ""
	data, _ := json.Marshal(in)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type workspaceRestoreAdmissionKey struct{}
type workspaceRestoreAdmission struct {
	service *WorkspaceRestoreService
	row     models.WorkspaceRestore
}

// Get is a receipt read. A changed current binding or withdrawn execution grant
// must not hide an already accepted operation, nor grant it permission to run.
func (s *WorkspaceRestoreService) Get(ctx context.Context, actor, id string) (models.WorkspaceRestore, error) {
	var row models.WorkspaceRestore
	if actor == "" || !workspaceJobID.MatchString(id) {
		return row, gorm.ErrRecordNotFound
	}
	err := s.db.WithContext(ctx).Where("id = ? AND actor_id = ?", id, actor).Take(&row).Error
	if err != nil || row.State == "restored" {
		return row, err
	}
	var job struct{ State, Error string }
	err = s.db.WithContext(ctx).Table("workspace_snapshot_jobs").Select("state", "error").Where("id = ? AND actor_id = ?", row.JobID, actor).Take(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row.State, row.Error = "unknown", "restore_job_missing"
		return row, nil
	}
	if err != nil {
		return row, err
	}
	row.State, row.Error = job.State, job.Error
	if row.State == "executed" {
		row.State, row.Error = "unknown", "restore_receipt_unconfirmed"
	}
	return row, nil
}

func (s *WorkspaceRestoreService) Admit(ctx context.Context, actor string, in WorkspaceRestoreInput) (models.WorkspaceRestore, error) {
	var zero models.WorkspaceRestore
	if actor == "" || len(actor) > 191 || strings.ContainsRune(actor, 0) || !workspaceJobID.MatchString(in.ClientRequestID) || in.SoloID == "" || len(in.SoloID) > 191 || strings.ContainsRune(in.SoloID, 0) || !workspaceJobID.MatchString(in.SnapshotID) || in.ExpectedBinding == "" || len(in.ExpectedBinding) > 128 || strings.ContainsRune(in.ExpectedBinding, 0) || in.Duration <= 0 || in.Duration > s.Jobs.options.MaxDuration {
		return zero, ErrWorkspaceJobInvalid
	}
	id, fingerprint := workspaceRestoreIdentity(actor, in.SoloID, in.ClientRequestID), restoreInputFingerprint(in)
	prior, err := s.Get(ctx, actor, id)
	if err == nil {
		if prior.SoloID != in.SoloID || prior.Fingerprint != fingerprint {
			return zero, ErrWorkspaceJobConflict
		}
		return prior, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, err
	}
	ctx, err = s.options.Prepare(ctx)
	if err != nil {
		return zero, err
	}
	var solo models.SoloSession
	if err := s.db.WithContext(ctx).Select("id", "user_id", "sandbox_id", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action").Where("id = ? AND user_id = ?", in.SoloID, actor).Take(&solo).Error; err != nil {
		return zero, err
	}
	source, spec, receipt, sourceHash, err := s.source(ctx, s.db.WithContext(ctx), actor, in.SnapshotID)
	if err != nil {
		return zero, err
	}
	row := models.WorkspaceRestore{ID: id, ActorID: actor, SoloID: in.SoloID, SnapshotID: source.ID, JobID: id, TargetSandboxID: solo.SandboxID, ExpectedBinding: in.ExpectedBinding, LifecycleEpoch: solo.LifecycleEpoch, LifecycleAttempt: solo.LifecycleAttemptID, SourceIdentity: sourceHash, SourceSpec: spec, SourceReceipt: receipt, Fingerprint: fingerprint, ConfirmNonce: in.ClientRequestID, State: "queued"}
	if err := s.target(ctx, s.db.WithContext(ctx), row); err != nil {
		return zero, err
	}
	var native workspaceRestoreSpec
	if workspacefiles.Decode([]byte(row.SourceReceipt), &native.Receipt) != nil {
		return zero, ErrWorkspaceJobInvalid
	}
	body, err := json.Marshal(native)
	if err != nil {
		return zero, err
	}
	ctx = context.WithValue(ctx, workspaceRestoreAdmissionKey{}, workspaceRestoreAdmission{s, row})
	job, err := s.Jobs.Admit(ctx, actor, WorkspaceJobInput{ClientRequestID: in.ClientRequestID, ScopeKind: "solo", ScopeID: in.SoloID, SandboxID: row.TargetSandboxID, Kind: "restore_files", Body: body, Duration: in.Duration})
	if err != nil {
		return zero, err
	}
	return s.Get(ctx, actor, job.ID)
}

func (s *WorkspaceRestoreService) Reconcile(ctx context.Context, actor, id string) (models.WorkspaceRestore, error) {
	row, err := s.Get(ctx, actor, id)
	if err != nil || row.State == "restored" {
		return row, err
	}
	ctx, err = s.options.Prepare(ctx)
	if err != nil {
		return row, err
	}
	_, err = s.Jobs.Reconcile(ctx, actor, row.JobID)
	if err != nil {
		return row, err
	}
	return s.Get(ctx, actor, id)
}

func (s *WorkspaceRestoreService) target(ctx context.Context, tx *gorm.DB, row models.WorkspaceRestore) error {
	binding, err := s.options.CurrentBinding(ctx, tx, row.ActorID, row.SoloID)
	if err != nil {
		return err
	}
	var solo models.SoloSession
	if err := tx.Select("id", "user_id", "sandbox_id", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action").Where("id = ? AND user_id = ?", row.SoloID, row.ActorID).Take(&solo).Error; err != nil {
		return err
	}
	if row.TargetSandboxID == 0 || binding.SandboxID != row.TargetSandboxID || binding.Fingerprint == "" || binding.Fingerprint != row.ExpectedBinding || solo.SandboxID != row.TargetSandboxID || solo.Status != models.SoloStatusRunning || solo.LifecycleAction != "" || solo.LifecycleEpoch != row.LifecycleEpoch || solo.LifecycleAttemptID != row.LifecycleAttempt {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (s *WorkspaceRestoreService) authorizeJob(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob) error {
	if job.Kind != "restore_files" {
		return s.baseAuth(ctx, tx, job)
	}
	var row models.WorkspaceRestore
	err := tx.Where("id = ? AND actor_id = ?", job.ID, job.ActorID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		proof, ok := ctx.Value(workspaceRestoreAdmissionKey{}).(workspaceRestoreAdmission)
		if !ok || proof.service != s || proof.row.ID != job.ID {
			return ErrWorkspaceJobInvalid
		}
		row = proof.row
		row.Lineage = job.Lineage
	} else if err != nil {
		return err
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
		if err := s.options.GuardTx(ctx, tx, row); err != nil {
			return err
		}
	}
	if row.JobID != job.ID || row.ActorID != job.ActorID || row.SoloID != job.ScopeID || job.ScopeKind != "solo" || row.TargetSandboxID != job.SandboxID || row.ConfirmNonce != job.Nonce || row.Lineage != job.Lineage || (row.State != "queued" && !(row.State == "restored" && job.State == "executed")) {
		return ErrWorkspaceJobConflict
	}
	var native workspaceRestoreSpec
	var expected workspacefiles.Receipt
	if workspacefiles.Decode([]byte(job.Body), &native) != nil || workspacefiles.Decode([]byte(row.SourceReceipt), &expected) != nil || expected != native.Receipt {
		return ErrWorkspaceJobConflict
	}
	if err := s.baseAuth(ctx, tx, job); err != nil {
		return err
	}
	if err := s.target(ctx, tx, row); err != nil {
		return err
	}
	_, spec, receipt, identity, err := s.source(ctx, tx, row.ActorID, row.SnapshotID)
	if err != nil {
		return err
	}
	if identity != row.SourceIdentity || spec != row.SourceSpec || receipt != row.SourceReceipt {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (s *WorkspaceRestoreService) admitted(ctx context.Context, tx *gorm.DB, job models.WorkspaceSnapshotJob) error {
	if job.Kind != "restore_files" {
		return nil
	}
	if err := workspaceRestoreTransaction(tx); err != nil {
		return err
	}
	proof, ok := ctx.Value(workspaceRestoreAdmissionKey{}).(workspaceRestoreAdmission)
	if !ok || proof.service != s || proof.row.ID != job.ID {
		return ErrWorkspaceJobInvalid
	}
	var prior models.WorkspaceRestore
	if err := tx.Where("id = ?", job.ID).Take(&prior).Error; err == nil {
		if prior.Fingerprint != proof.row.Fingerprint || prior.ActorID != job.ActorID {
			return ErrWorkspaceJobConflict
		}
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row := proof.row
	row.Lineage = job.Lineage
	if err := s.authorizeJob(ctx, tx, job); err != nil {
		return err
	}
	return tx.Create(&row).Error
}

func workspaceRestoreTransaction(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil {
		return ErrWorkspaceJobInvalid
	}
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceJobInvalid
	}
	return nil
}
