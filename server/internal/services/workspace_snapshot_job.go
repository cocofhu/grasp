package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrWorkspaceJobAuthority   = errors.New("workspace job execution authority is unavailable or changed")
	ErrWorkspaceJobInvalid     = errors.New("invalid workspace job intent")
	ErrWorkspaceJobOwner       = errors.New("workspace job owner lost")
	ErrWorkspaceJobConflict    = errors.New("workspace job request identity conflict")
	ErrWorkspaceJobUnknown     = errors.New("workspace job requires explicit reconciliation")
	ErrWorkspaceJobUnsupported = errors.New("workspace job kind is not available")
	workspaceJobID             = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)
)

// Authority is intentionally required and has no SQL/default/random-startup
// implementation. Startup/restore must supply a currently verified external
// execution lineage. Restoring SQL alone never activates old queued jobs.
type WorkspaceJobAuthority interface {
	Current(context.Context) (string, error)
	Verify(context.Context, string) error
}

type WorkspaceJobRemote interface {
	Capabilities(context.Context) (sandbox.WorkspaceJobCapabilities, error)
	Create(context.Context, sandbox.WorkspaceJobCreate) (sandbox.WorkspaceJobReceipt, error)
	Get(context.Context, string) (sandbox.WorkspaceJobReceipt, error)
	Renew(context.Context, string, sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error)
	Cancel(context.Context, string, sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error)
}

type WorkspaceJobInput struct {
	ClientRequestID string
	ScopeKind       string
	ScopeID         string
	SandboxID       uint
	Kind            string
	Body            json.RawMessage
	Duration        time.Duration
	NotAfter        time.Time `json:"notAfter,omitzero"`
}
type WorkspaceJobOptions struct {
	Authority WorkspaceJobAuthority
	// Authorize must inspect metadata only when given a transaction. It is
	// required both at admission and fenced dispatch/publication, not inferred
	// from a Run's CreatedBy or a caller-supplied actor field.
	Authorize func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob) error
	// PrepareAuthorization performs external proof reads before the outermost
	// SQL transaction. The resulting private context proof is consumed by pure
	// SQL Authorize/Guard callbacks; it must never refresh inside that TX.
	PrepareAuthorization func(context.Context) (context.Context, error)
	Validate             map[string]func(json.RawMessage) error
	Processors           map[string]WorkspaceJobProcessor
	// These hooks inspect and write metadata only in the existing job TX.
	AdmittedTx     func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob) error
	CompletedTx    func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob, sandbox.WorkspaceJobReceipt) error
	Remote         func(context.Context, uint) (WorkspaceJobRemote, error)
	LeaseTTL       time.Duration
	RenewInterval  time.Duration
	RemoteInterval time.Duration
	PollInterval   time.Duration
	MaxDuration    time.Duration
	MaxWorkers     int
}

type WorkspaceSnapshotJobs struct {
	db      *gorm.DB
	sandbox *SandboxService
	options WorkspaceJobOptions
	local   sync.Map
}

func NewWorkspaceSnapshotJobs(db *gorm.DB, sbx *SandboxService, options WorkspaceJobOptions) *WorkspaceSnapshotJobs {
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 45 * time.Second
	}
	if options.RenewInterval <= 0 {
		options.RenewInterval = options.LeaseTTL / 3
	}
	if options.RemoteInterval <= 0 {
		options.RemoteInterval = options.LeaseTTL / 6
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 250 * time.Millisecond
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 30 * time.Minute
	}
	if options.MaxWorkers <= 0 {
		options.MaxWorkers = 2
	}
	validators := map[string]func(json.RawMessage) error{}
	for kind, validate := range options.Validate {
		validators[kind] = validate
	}
	options.Validate = validators
	processors := map[string]WorkspaceJobProcessor{}
	for kind, processor := range options.Processors {
		if processor != nil {
			processors[kind] = processor
		}
	}
	options.Processors = processors
	s := &WorkspaceSnapshotJobs{db: db, sandbox: sbx, options: options}
	if s.options.Remote == nil && sbx != nil {
		s.options.Remote = func(ctx context.Context, id uint) (WorkspaceJobRemote, error) {
			host, port, password, err := sbx.acpConnection(ctx, id)
			if err != nil {
				return nil, err
			}
			return sandbox.WorkspaceJobClient{Host: host, Port: port, Password: password}, nil
		}
	}
	return s
}

func canonicalWorkspaceJob(body json.RawMessage) (json.RawMessage, error) {
	if len(body) == 0 {
		body = json.RawMessage(`{}`)
	}
	if len(body) > 64<<10 {
		return nil, ErrWorkspaceJobInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, errors.Join(ErrWorkspaceJobInvalid, err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrWorkspaceJobInvalid
	}
	normalized, err := json.Marshal(value)
	if len(normalized) > 64<<10 {
		return nil, ErrWorkspaceJobInvalid
	}
	return normalized, err
}

func (s *WorkspaceSnapshotJobs) verify(ctx context.Context, lineage string) error {
	if s.options.Authority == nil || s.options.Authorize == nil || s.sandbox == nil || s.options.Remote == nil {
		return ErrWorkspaceJobAuthority
	}
	if err := s.options.Authority.Verify(ctx, lineage); err != nil {
		return errors.Join(ErrWorkspaceJobAuthority, err)
	}
	return nil
}

func (s *WorkspaceSnapshotJobs) prepareAuthorization(ctx context.Context) (context.Context, error) {
	if s.options.PrepareAuthorization != nil {
		return s.options.PrepareAuthorization(ctx)
	}
	return ctx, nil
}

func (s *WorkspaceSnapshotJobs) Admit(ctx context.Context, actor string, in WorkspaceJobInput) (models.WorkspaceSnapshotJob, error) {
	var row models.WorkspaceSnapshotJob
	if actor == "" || !workspaceJobID.MatchString(in.ClientRequestID) || in.ScopeID == "" || len(in.ScopeID) > 191 || (in.ScopeKind != "solo" && in.ScopeKind != "run") || in.SandboxID == 0 || in.Duration <= 0 || in.Duration > s.options.MaxDuration {
		return row, ErrWorkspaceJobInvalid
	}
	validator := s.options.Validate[in.Kind]
	if validator == nil {
		return row, ErrWorkspaceJobUnsupported
	}
	body, err := canonicalWorkspaceJob(in.Body)
	if err != nil {
		return row, err
	}
	if err := validator(body); err != nil {
		return row, errors.Join(ErrWorkspaceJobInvalid, err)
	}
	if s.options.Authority == nil {
		return row, ErrWorkspaceJobAuthority
	}
	lineage, err := s.options.Authority.Current(ctx)
	if err != nil || !workspaceJobID.MatchString(lineage) {
		return row, ErrWorkspaceJobAuthority
	}
	if err := s.verify(ctx, lineage); err != nil {
		return row, err
	}
	identity := sha256.Sum256([]byte(actor + "\x00" + in.ScopeKind + "\x00" + in.ScopeID + "\x00" + in.ClientRequestID))
	input := in
	input.Body = body
	input.ClientRequestID = ""
	data, err := json.Marshal(input)
	if err != nil {
		return row, err
	}
	fingerprint := sha256.Sum256(data)
	row = models.WorkspaceSnapshotJob{ID: hex.EncodeToString(identity[:]), ActorID: actor, ScopeKind: in.ScopeKind, ScopeID: in.ScopeID, SandboxID: in.SandboxID, Kind: in.Kind, Body: string(body), Nonce: in.ClientRequestID, Lineage: lineage, Fingerprint: hex.EncodeToString(fingerprint[:]), State: "queued", Deadline: time.Now().UTC().Add(in.Duration)}
	if !in.NotAfter.IsZero() {
		if !in.NotAfter.After(time.Now()) || in.NotAfter.After(row.Deadline) {
			return models.WorkspaceSnapshotJob{}, ErrWorkspaceJobInvalid
		}
		row.Deadline = in.NotAfter.UTC()
	}
	ctx, err = s.prepareAuthorization(ctx)
	if err != nil {
		return models.WorkspaceSnapshotJob{}, err
	}
	if err := s.options.Authorize(ctx, s.db.WithContext(ctx), row); err != nil {
		return models.WorkspaceSnapshotJob{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.options.Authorize(ctx, tx, row); err != nil {
			return err
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			var prior models.WorkspaceSnapshotJob
			if err := tx.First(&prior, "id = ?", row.ID).Error; err != nil {
				return err
			}
			if prior.Fingerprint != row.Fingerprint || prior.ActorID != actor || prior.Lineage != lineage {
				return ErrWorkspaceJobConflict
			}
			row = prior
		}
		if s.options.AdmittedTx != nil {
			return s.options.AdmittedTx(ctx, tx, row)
		}
		return nil
	})
	return row, err
}

func (s *WorkspaceSnapshotJobs) Get(ctx context.Context, actor, id string) (models.WorkspaceSnapshotJob, error) {
	var row models.WorkspaceSnapshotJob
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND actor_id = ?", id, actor).Error; err != nil {
		return row, err
	}
	if s.options.Authorize == nil {
		return row, ErrWorkspaceJobAuthority
	}
	return row, s.options.Authorize(ctx, s.db.WithContext(ctx), row)
}

type workspaceJobLease struct {
	id, owner, lineage string
	generation         uint64
	until              time.Time
}

func (s *WorkspaceSnapshotJobs) claim(ctx context.Context, id string, reconcile bool) (models.WorkspaceSnapshotJob, workspaceJobLease, error) {
	var row models.WorkspaceSnapshotJob
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return row, workspaceJobLease{}, err
	}
	if err := s.verify(ctx, row.Lineage); err != nil {
		return row, workspaceJobLease{}, err
	}
	ctx, err := s.prepareAuthorization(ctx)
	if err != nil {
		return row, workspaceJobLease{}, err
	}
	if err := s.options.Authorize(ctx, s.db.WithContext(ctx), row); err != nil {
		return row, workspaceJobLease{}, err
	}
	now := time.Now().UTC()
	owner := uuid.NewString()
	until := now.Add(s.options.LeaseTTL)
	if !reconcile && row.Deadline.Before(until) {
		until = row.Deadline
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.options.Authorize(ctx, tx, row); err != nil {
			return err
		}
		claimed := tx.Model(&models.WorkspaceSnapshotJob{}).Where("id = ? AND lineage = ? AND (owner = '' OR owner IS NULL OR expires_at < ?)", id, row.Lineage, time.Now().UTC())
		if reconcile {
			claimed = claimed.Where("state = ?", "unknown")
		} else {
			claimed = claimed.Where("state = ? AND dispatched_at IS NULL AND deadline > ?", "queued", time.Now().UTC())
		}
		updated := claimed.Updates(map[string]any{"owner": owner, "generation": gorm.Expr("generation + 1"), "expires_at": until})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrWorkspaceJobOwner
		}
		return nil
	})
	if err != nil {
		return row, workspaceJobLease{}, err
	}
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND owner = ?", id, owner).Error; err != nil {
		return row, workspaceJobLease{}, err
	}
	return row, workspaceJobLease{id: id, owner: owner, lineage: row.Lineage, generation: row.Generation, until: until}, nil
}

func (s *WorkspaceSnapshotJobs) renew(ctx context.Context, lease workspaceJobLease, deadline time.Time) (time.Time, error) {
	if err := s.verify(ctx, lease.lineage); err != nil {
		return time.Time{}, errors.Join(ErrWorkspaceJobOwner, err)
	}
	ctx, err := s.prepareAuthorization(ctx)
	if err != nil {
		return time.Time{}, errors.Join(ErrWorkspaceJobOwner, err)
	}
	now := time.Now().UTC()
	until := now.Add(s.options.LeaseTTL)
	if !deadline.IsZero() && deadline.Before(until) {
		until = deadline
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.WorkspaceSnapshotJob
		if err := tx.Take(&row, "id = ?", lease.id).Error; err != nil {
			return err
		}
		if err := s.options.Authorize(ctx, tx, row); err != nil {
			return err
		}
		updated := tx.Model(&models.WorkspaceSnapshotJob{}).Where("id = ? AND owner = ? AND generation = ? AND lineage = ? AND expires_at > ?", lease.id, lease.owner, lease.generation, lease.lineage, time.Now().UTC()).Update("expires_at", until)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrWorkspaceJobOwner
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return until, nil
}

func (s *WorkspaceSnapshotJobs) owned(ctx context.Context, lease workspaceJobLease, reconcile bool, fn func(context.Context, models.WorkspaceSnapshotJob, workspaceJobLease) error) error {
	defer s.release(lease)
	row := models.WorkspaceSnapshotJob{}
	if err := s.db.WithContext(ctx).First(&row, "id = ?", lease.id).Error; err != nil {
		return err
	}
	deadline := row.Deadline
	if reconcile {
		deadline = time.Now().Add(s.options.MaxDuration)
	}
	work, cancelDeadline := context.WithDeadline(ctx, deadline)
	defer cancelDeadline()
	work, cancel := context.WithCancelCause(work)
	defer cancel(context.Canceled)
	watch := make(chan struct{})
	go func() {
		defer close(watch)
		err := workspaceJobMaintain(work, lease.until, s.options.RenewInterval, nil, func(ctx context.Context) (time.Time, error) { return s.renew(ctx, lease, deadline) })
		cancel(err)
	}()
	defer func() { cancel(context.Canceled); <-watch }()
	operation, release, err := s.sandbox.acquireSandboxOperation(work, row.SandboxID, "workspace_"+row.Kind)
	if err != nil {
		return err
	}
	defer release()
	return fn(operation, row, lease)
}

func (s *WorkspaceSnapshotJobs) release(lease workspaceJobLease) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.db.WithContext(ctx).Model(&models.WorkspaceSnapshotJob{}).Where("id = ? AND owner = ? AND generation = ?", lease.id, lease.owner, lease.generation).Updates(map[string]any{"owner": "", "expires_at": nil}).Error
}

func (s *WorkspaceSnapshotJobs) publish(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease, states []string, updates map[string]any) error {
	return s.publishCompletion(ctx, row, lease, states, updates, nil)
}

func (s *WorkspaceSnapshotJobs) publishCompletion(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease, states []string, updates map[string]any, completion WorkspaceJobCompletion) error {
	if err := s.verify(ctx, row.Lineage); err != nil {
		return err
	}
	ctx, err := s.prepareAuthorization(ctx)
	if err != nil {
		return err
	}
	return s.sandbox.commitSandboxOperationPrepared(ctx, row.SandboxID, func(tx *gorm.DB) error {
		if err := s.options.Authorize(ctx, tx, row); err != nil {
			return err
		}
		if preparer, ok := completion.(interface {
			PrepareTx(context.Context, *gorm.DB) error
		}); ok {
			return preparer.PrepareTx(ctx, tx)
		}
		return nil
	}, func(tx *gorm.DB) error {
		result := tx.Model(&models.WorkspaceSnapshotJob{}).Where("id = ? AND owner = ? AND generation = ? AND lineage = ? AND expires_at > ? AND state IN ?", row.ID, lease.owner, lease.generation, lease.lineage, time.Now().UTC(), states).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWorkspaceJobOwner
		}
		if completion != nil {
			if err := completion.Commit(ctx, tx); err != nil {
				return err
			}
			// Pure SQL publication must also finish before the job lease ends.
			var current models.WorkspaceSnapshotJob
			if err := tx.Select("id").Take(&current, "id = ? AND owner = ? AND generation = ? AND lineage = ? AND expires_at > ?", row.ID, lease.owner, lease.generation, lease.lineage, time.Now().UTC()).Error; err != nil {
				return errors.Join(ErrWorkspaceJobOwner, err)
			}
		}
		return nil
	})
}

func (s *WorkspaceSnapshotJobs) controlFence(ctx context.Context, row models.WorkspaceSnapshotJob, lease workspaceJobLease) (sandbox.WorkspaceJobFence, error) {
	if err := s.verify(ctx, row.Lineage); err != nil {
		return sandbox.WorkspaceJobFence{}, errors.Join(ErrWorkspaceJobOwner, err)
	}
	ctx, err := s.prepareAuthorization(ctx)
	if err != nil {
		return sandbox.WorkspaceJobFence{}, errors.Join(ErrWorkspaceJobOwner, err)
	}
	if err := s.options.Authorize(ctx, s.db.WithContext(ctx), row); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return sandbox.WorkspaceJobFence{}, ErrWorkspaceJobOwner
		}
		return sandbox.WorkspaceJobFence{}, err
	}
	var job models.WorkspaceSnapshotJob
	if err := s.db.WithContext(ctx).Select("expires_at").Where("id = ? AND owner = ? AND generation = ? AND expires_at > ?", row.ID, lease.owner, lease.generation, time.Now().UTC()).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return sandbox.WorkspaceJobFence{}, ErrWorkspaceJobOwner
		}
		return sandbox.WorkspaceJobFence{}, err
	}
	var held *sandboxOperation
	for op, _ := ctx.Value(sandboxOperationKey{}).(*sandboxOperation); op != nil; op = op.parent {
		if op.service == s.sandbox && op.id == row.SandboxID {
			held = op
			break
		}
	}
	if held == nil || job.ExpiresAt == nil {
		return sandbox.WorkspaceJobFence{}, ErrWorkspaceJobOwner
	}
	var physical models.Sandbox
	err = s.db.WithContext(ctx).Select("generation", "operation_expires_at").Where("id = ? AND operation_owner = ? AND generation = ? AND operation_expires_at > ?", row.SandboxID, held.owner, held.generation, time.Now().UTC()).First(&physical).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && physical.OperationExpiresAt == nil) {
		return sandbox.WorkspaceJobFence{}, ErrWorkspaceJobOwner
	}
	if err != nil {
		return sandbox.WorkspaceJobFence{}, err
	}
	until := *physical.OperationExpiresAt
	if job.ExpiresAt.Before(until) {
		until = *job.ExpiresAt
	}
	if row.Deadline.Before(until) {
		until = row.Deadline
	}
	if !until.After(time.Now()) {
		return sandbox.WorkspaceJobFence{}, ErrWorkspaceJobOwner
	}
	return sandbox.WorkspaceJobFence{Owner: lease.owner, Generation: physical.Generation, ExpiresAt: until}, nil
}

func runtimeJobSpec(row models.WorkspaceSnapshotJob, resource string) (sandbox.WorkspaceJobSpec, string, error) {
	spec := sandbox.WorkspaceJobSpec{ID: row.ID, Kind: row.Kind, Resource: resource, Lineage: row.Lineage, Nonce: row.Nonce, Deadline: row.Deadline.UTC(), Body: json.RawMessage(row.Body)}
	data, err := json.Marshal(spec)
	if err != nil {
		return spec, "", err
	}
	sum := sha256.Sum256(data)
	return spec, hex.EncodeToString(sum[:]), nil
}

func validateRuntimeJob(row models.WorkspaceSnapshotJob, value sandbox.WorkspaceJobReceipt) error {
	if value.Version != sandbox.WorkspaceJobVersion || value.ID != row.ID || value.Kind != row.Kind || value.SHA256 != row.RuntimeSHA256 || value.Resource != row.RuntimeResource || value.Lineage != row.Lineage || value.Generation != row.RuntimeGeneration || !value.Deadline.Equal(row.Deadline) {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func supportsRuntimeJob(row models.WorkspaceSnapshotJob, caps sandbox.WorkspaceJobCapabilities, resource string) bool {
	return caps.Version == sandbox.WorkspaceJobVersion && caps.Resource == resource && caps.Lineage == row.Lineage && caps.RenewableLease && caps.PersistentGeneration && caps.MaxLeaseMillis > 0 && slices.Contains(caps.Kinds, row.Kind)
}
