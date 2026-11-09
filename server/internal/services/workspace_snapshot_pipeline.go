package services

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkspaceFileRemote interface {
	WorkspaceJobRemote
	FileCapabilities(context.Context) (sandbox.WorkspaceFileCapabilities, error)
	ReadWorkspaceChunk(context.Context, string, sandbox.WorkspaceJobRenew, string, int64, int) ([]byte, bool, error)
	WriteWorkspaceChunk(context.Context, string, sandbox.WorkspaceJobRenew, string, int64, []byte) error
	FinishWorkspaceTransfer(context.Context, string, sandbox.WorkspaceJobRenew) error
}

// WorkspaceSnapshotPipeline composes real collectors, immutable artifact reads,
// semantic validation and verified storage pins. Start Run with app lifetime;
// HTTP admission only creates durable intents. No runtime task is replayed.
type WorkspaceSnapshotPipeline struct {
	Jobs      *WorkspaceSnapshotJobs
	db        *gorm.DB
	holds     *payloadhold.Manager
	files     *filestore.Store
	limits    payloadhold.Limits
	authorize func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob) error
}

func NewWorkspaceSnapshotPipeline(db *gorm.DB, sbx *SandboxService, holds *payloadhold.Manager, files *filestore.Store, limits payloadhold.Limits, options WorkspaceJobOptions) (*WorkspaceSnapshotPipeline, error) {
	if db == nil || sbx == nil || holds == nil || files == nil || options.Authority == nil || options.Authorize == nil {
		return nil, ErrWorkspaceJobAuthority
	}
	if limits == (payloadhold.Limits{}) {
		limits = payloadhold.DefaultLimits()
	}
	if limits.MaxFileBytes < 1 || limits.MaxUncompressedBytes < 1 || limits.MaxArchiveBytes < 1 || limits.MaxManifestBytes < 1 || limits.MaxEntries < 1 {
		return nil, ErrWorkspaceJobInvalid
	}
	p := &WorkspaceSnapshotPipeline{db: db, holds: holds, files: files, limits: limits, authorize: options.Authorize}
	if options.Validate == nil {
		options.Validate = map[string]func(json.RawMessage) error{}
	}
	// Do not mutate caller-owned maps when composing this worker.
	validators := map[string]func(json.RawMessage) error{}
	for kind, fn := range options.Validate {
		validators[kind] = fn
	}
	validators["capture_files"] = workspacefiles.ValidateSpec
	validators["read_snapshot"] = func(body json.RawMessage) error {
		var value workspacefiles.ReadSpec
		if workspacefiles.Decode(body, &value) != nil || !workspaceJobID.MatchString(value.SourceJobID) || len(value.SourceSHA256) != 64 {
			return ErrWorkspaceJobInvalid
		}
		return nil
	}
	options.Validate = validators
	processors := map[string]WorkspaceJobProcessor{}
	for kind, processor := range options.Processors {
		processors[kind] = processor
	}
	processors["read_snapshot"] = p
	options.Processors = processors
	admitted, completed := options.AdmittedTx, options.CompletedTx
	options.Authorize = p.authorizeJob
	options.AdmittedTx = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if admitted != nil {
			if err := admitted(ctx, tx, row); err != nil {
				return err
			}
		}
		return p.admitted(ctx, tx, row)
	}
	options.CompletedTx = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob, receipt sandbox.WorkspaceJobReceipt) error {
		if completed != nil {
			if err := completed(ctx, tx, row, receipt); err != nil {
				return err
			}
		}
		return p.captured(ctx, tx, row, receipt)
	}
	p.Jobs = NewWorkspaceSnapshotJobs(db, sbx, options)
	return p, nil
}

func (p *WorkspaceSnapshotPipeline) authorizeJob(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
	if err := p.authorize(ctx, tx, row); err != nil {
		return err
	}
	if row.Kind != "capture_files" && row.Kind != "read_snapshot" {
		return nil
	}
	var snapshot models.WorkspaceSnapshot
	id := row.ID
	if row.Kind == "read_snapshot" {
		var body workspacefiles.ReadSpec
		if workspacefiles.Decode([]byte(row.Body), &body) != nil {
			return ErrWorkspaceJobInvalid
		}
		id = body.SourceJobID
	}
	current := func() *gorm.DB {
		query := tx.Session(&gorm.Session{})
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		return query
	}
	err := current().Take(&snapshot, "id = ?", id).Error
	missing := errors.Is(err, gorm.ErrRecordNotFound) && row.Kind == "capture_files" && row.DispatchedAt == nil
	if err != nil && !missing {
		return err
	}
	// Lock immutable source metadata before the physical sandbox row. These
	// current reads also avoid stale MVCC provenance under MySQL RR.
	var source models.Sandbox
	if err := current().Take(&source, row.SandboxID).Error; err != nil {
		return err
	}
	if row.ScopeKind == "run" && (source.StateRunID == 0 || source.RunID != row.ScopeID) {
		return ErrWorkspaceJobUnsupported
	}
	if missing {
		return nil
	}
	if snapshot.SourceSandboxID != row.SandboxID || snapshot.ActorID != row.ActorID || snapshot.ScopeKind != row.ScopeKind || snapshot.ScopeID != row.ScopeID || snapshot.Lineage != row.Lineage || snapshot.RunID != source.RunID || snapshot.StateRunID != source.StateRunID || snapshot.NodeID != source.NodeID || snapshot.Iteration != source.Iteration || snapshot.RunAttempt != source.RunAttempt || snapshot.ProviderAttempt != source.ProviderAttempt {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (p *WorkspaceSnapshotPipeline) admitted(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
	switch row.Kind {
	case "capture_files":
		var source models.Sandbox
		if err := tx.Take(&source, row.SandboxID).Error; err != nil {
			return err
		}
		var spec workspacefiles.Spec
		if workspacefiles.Decode([]byte(row.Body), &spec) != nil {
			return ErrWorkspaceJobInvalid
		}
		snapshot := models.WorkspaceSnapshot{ID: row.ID, JobID: row.ID, ActorID: row.ActorID, ScopeKind: row.ScopeKind, ScopeID: row.ScopeID, SourceSandboxID: row.SandboxID, RunID: source.RunID, StateRunID: source.StateRunID, NodeID: source.NodeID, Iteration: source.Iteration, RunAttempt: source.RunAttempt, ProviderAttempt: source.ProviderAttempt, Lineage: row.Lineage, State: "capturing", PolicyVersion: spec.PolicyVersion}
		if spec.Binding != nil {
			proof, ok := ctx.Value(workspaceCaptureAdmissionKey{}).(workspaceCaptureAdmission)
			if !ok || proof.pipeline != p || proof.binding != *spec.Binding || !restoreDigest(proof.fingerprint) {
				return ErrWorkspaceJobAuthority
			}
			snapshot.SourceBindingFingerprint = proof.fingerprint
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&snapshot).Error; err != nil {
			return err
		}
		return p.authorizeJob(ctx, tx, row)
	case "read_snapshot":
		snapshot, _, _, err := p.publicationSource(ctx, tx, row)
		if err != nil {
			return err
		}
		if snapshot.PublicationJobID == row.ID {
			return nil
		}
		result := tx.Model(&models.WorkspaceSnapshot{}).Where("id = ? AND state = ? AND (publication_job_id = '' OR publication_job_id IS NULL)", snapshot.ID, "captured").Updates(map[string]any{"publication_job_id": row.ID, "state": "publishing"})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWorkspaceJobConflict
		}
	}
	return nil
}

func (p *WorkspaceSnapshotPipeline) captured(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob, receipt sandbox.WorkspaceJobReceipt) error {
	if row.Kind != "capture_files" {
		return nil
	}
	var artifacts workspacefiles.Receipt
	if workspacefiles.Decode(receipt.Result, &artifacts) != nil || artifacts.Version != 1 || artifacts.Consistency != "verified_copy" {
		return ErrWorkspaceJobUnknown
	}
	if err := p.authorizeJob(ctx, tx, row); err != nil {
		return err
	}
	result := tx.Model(&models.WorkspaceSnapshot{}).Where("id = ? AND job_id = ? AND lineage = ? AND state = ?", row.ID, row.ID, row.Lineage, "capturing").Updates(map[string]any{"state": "captured", "source_generation": row.RuntimeGeneration})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (p *WorkspaceSnapshotPipeline) publicationSource(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) (models.WorkspaceSnapshot, models.WorkspaceSnapshotJob, workspacefiles.ReadSpec, error) {
	var snapshot models.WorkspaceSnapshot
	var capture models.WorkspaceSnapshotJob
	var body workspacefiles.ReadSpec
	if row.Kind != "read_snapshot" || workspacefiles.Decode([]byte(row.Body), &body) != nil {
		return snapshot, capture, body, ErrWorkspaceJobInvalid
	}
	if err := tx.Take(&snapshot, "id = ?", body.SourceJobID).Error; err != nil {
		return snapshot, capture, body, err
	}
	if err := tx.Take(&capture, "id = ?", snapshot.JobID).Error; err != nil {
		return snapshot, capture, body, err
	}
	var receipt workspacefiles.Receipt
	if capture.Kind != "capture_files" || capture.State != "executed" || capture.RuntimeSHA256 != body.SourceSHA256 || capture.RuntimeGeneration != snapshot.SourceGeneration || snapshot.SourceSandboxID != row.SandboxID || snapshot.ActorID != row.ActorID || snapshot.ScopeKind != row.ScopeKind || snapshot.ScopeID != row.ScopeID || snapshot.Lineage != row.Lineage || capture.Lineage != row.Lineage || workspacefiles.Decode([]byte(capture.Result), &receipt) != nil || receipt != body.Receipt || (snapshot.PublicationJobID != "" && snapshot.PublicationJobID != row.ID) {
		return snapshot, capture, body, ErrWorkspaceJobConflict
	}
	// The capture is immutable evidence, not a second execution. The private
	// current-grant proof belongs to this read_snapshot operation only.
	if err := p.authorizeJob(ctx, tx, row); err != nil {
		return snapshot, capture, body, err
	}
	return snapshot, capture, body, nil
}

// Run dispatches each never-sent intent once and queues publication only for a
// completed immutable capture. Unknown dispatches/holds are never auto-retried.
func (p *WorkspaceSnapshotPipeline) Run(ctx context.Context) error {
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Jobs.Run(work) }()
	tick := time.NewTicker(p.Jobs.options.PollInterval)
	defer tick.Stop()
	cursor := ""
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			cancel()
			<-done
			return context.Cause(ctx)
		case <-tick.C:
			cursor = p.discoverPublication(work, cursor)
		}
	}
}

func (p *WorkspaceSnapshotPipeline) discoverPublication(ctx context.Context, cursor string) string {
	p.Jobs.operations.Add(1)
	defer p.Jobs.operations.Add(-1)
	if p.Jobs.paused.Load() {
		return cursor
	}
	lineage, err := p.Jobs.options.Authority.Current(ctx)
	if err != nil || p.Jobs.verify(ctx, lineage) != nil {
		return cursor
	}
	var snapshots []models.WorkspaceSnapshot
	if p.db.WithContext(ctx).Where("lineage = ? AND state = ? AND id > ?", lineage, "captured", cursor).Order("id").Limit(p.Jobs.options.MaxWorkers).Find(&snapshots).Error != nil {
		return cursor
	}
	if len(snapshots) == 0 {
		return ""
	}
	for _, snapshot := range snapshots {
		// Failed authorization for an older source must not starve later
		// captures. Discovery never replays a dispatched unknown operation.
		cursor = snapshot.ID
		_ = p.QueuePublication(ctx, snapshot.ActorID, snapshot.ID)
	}
	return cursor
}

func (p *WorkspaceSnapshotPipeline) QueuePublication(ctx context.Context, actor, id string) error {
	var snapshot models.WorkspaceSnapshot
	if err := p.db.WithContext(ctx).Take(&snapshot, "id = ? AND actor_id = ?", id, actor).Error; err != nil {
		return err
	}
	capture, err := p.Jobs.Get(ctx, actor, snapshot.JobID)
	if err != nil {
		return err
	}
	var receipt workspacefiles.Receipt
	if capture.State != "executed" || workspacefiles.Decode([]byte(capture.Result), &receipt) != nil {
		return ErrWorkspaceJobUnknown
	}
	body, err := json.Marshal(workspacefiles.ReadSpec{SourceJobID: capture.ID, SourceSHA256: capture.RuntimeSHA256, Receipt: receipt})
	if err != nil {
		return err
	}
	_, err = p.Jobs.Admit(ctx, actor, WorkspaceJobInput{ClientRequestID: "publish-" + capture.ID, ScopeKind: capture.ScopeKind, ScopeID: capture.ScopeID, SandboxID: capture.SandboxID, Kind: "read_snapshot", Body: body, Duration: p.Jobs.options.MaxDuration, NotAfter: capture.Deadline})
	return err
}

type WorkspaceCaptureInput struct {
	ClientRequestID, ScopeKind, ScopeID string
	ExpectedBinding                     string
	Binding                             *sandbox.WorkspaceBindingReference
	SandboxID                           uint
	Paths, IncludeIgnored               []string
	Duration                            time.Duration
}

func (p *WorkspaceSnapshotPipeline) Capture(ctx context.Context, actor string, in WorkspaceCaptureInput) (models.WorkspaceSnapshotJob, error) {
	return p.capture(ctx, actor, in)
}
