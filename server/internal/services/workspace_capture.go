package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

type workspaceCaptureAdmissionKey struct{}
type workspaceCaptureAdmission struct {
	pipeline    *WorkspaceSnapshotPipeline
	fingerprint string
	binding     sandbox.WorkspaceBindingReference
}

func captureRequestID(actor, kind, scope, nonce string) string {
	digest := sha256.Sum256([]byte(actor + "\x00" + kind + "\x00" + scope + "\x00" + nonce))
	return hex.EncodeToString(digest[:])
}

func validateCaptureInput(actor string, in WorkspaceCaptureInput) error {
	if !snapshotCatalogCoordinate(actor, workspaceKeyMax) || !snapshotCatalogCoordinate(in.ScopeID, workspaceKeyMax) || (in.ScopeKind != "solo" && in.ScopeKind != "run") || !workspaceJobID.MatchString(in.ClientRequestID) || in.SandboxID == 0 || in.Duration <= 0 {
		return ErrWorkspaceJobInvalid
	}
	if in.ExpectedBinding == "" && in.Binding == nil {
		return nil // Compatibility for an explicitly unbound original runtime.
	}
	if !restoreDigest(in.ExpectedBinding) {
		return ErrWorkspaceJobInvalid
	}
	if in.Binding != nil && (!workspaceJobID.MatchString(in.Binding.ID) || in.Binding.Revision == 0 || !restoreDigest(in.Binding.Fingerprint)) {
		return ErrWorkspaceJobInvalid
	}
	return nil
}

// capturePrior reads the original complete request before any execution or
// provider proof. A current metadata scope check still applies. The original
// duration, selected paths, source environment and logical binding must match.
func (p *WorkspaceSnapshotPipeline) capturePrior(ctx context.Context, actor string, in WorkspaceCaptureInput) (models.WorkspaceSnapshotJob, bool, error) {
	id := captureRequestID(actor, in.ScopeKind, in.ScopeID, in.ClientRequestID)
	var prior models.WorkspaceSnapshotJob
	err := p.db.WithContext(ctx).Take(&prior, "id = ? AND actor_id = ? AND scope_kind = ? AND scope_id = ?", id, actor, in.ScopeKind, in.ScopeID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return prior, false, nil
	}
	if err != nil {
		return prior, false, err
	}
	if p.Jobs.options.ReadScope != nil {
		if err := p.Jobs.options.ReadScope(ctx, p.db.WithContext(ctx), actor, in.ScopeKind, in.ScopeID); err != nil {
			return models.WorkspaceSnapshotJob{}, true, err
		}
	} else {
		// Older isolated compositions retain their explicit business policy.
		// Production installs the separate required metadata read policy.
		ctx, err = p.Jobs.prepareJobAuthorization(ctx, prior)
		if err != nil {
			return models.WorkspaceSnapshotJob{}, true, err
		}
		if err := p.authorize(ctx, p.db.WithContext(ctx), prior); err != nil {
			return models.WorkspaceSnapshotJob{}, true, err
		}
	}
	var frozen workspacefiles.Spec
	if prior.Kind != "capture_files" || prior.SandboxID != in.SandboxID || workspacefiles.Decode([]byte(prior.Body), &frozen) != nil || !slices.Equal(frozen.Paths, in.Paths) || !slices.Equal(frozen.IncludeIgnored, in.IncludeIgnored) || in.Binding != nil && (frozen.Binding == nil || *frozen.Binding != *in.Binding) {
		return models.WorkspaceSnapshotJob{}, true, ErrWorkspaceJobConflict
	}
	var source struct{ SourceBindingFingerprint string }
	if err := p.db.WithContext(ctx).Table("workspace_snapshots").Select("source_binding_fingerprint").Take(&source, "id = ? AND job_id = ?", prior.ID, prior.ID).Error; err != nil {
		return models.WorkspaceSnapshotJob{}, true, err
	}
	if source.SourceBindingFingerprint != in.ExpectedBinding {
		return models.WorkspaceSnapshotJob{}, true, ErrWorkspaceJobConflict
	}
	body, err := canonicalWorkspaceJob([]byte(prior.Body))
	if err != nil {
		return models.WorkspaceSnapshotJob{}, true, err
	}
	input := WorkspaceJobInput{ScopeKind: in.ScopeKind, ScopeID: in.ScopeID, SandboxID: in.SandboxID, Kind: "capture_files", Body: body, Duration: in.Duration}
	encoded, err := json.Marshal(input)
	if err != nil {
		return models.WorkspaceSnapshotJob{}, true, err
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != prior.Fingerprint {
		return models.WorkspaceSnapshotJob{}, true, ErrWorkspaceJobConflict
	}
	return prior, true, nil
}

func (p *WorkspaceSnapshotPipeline) capture(ctx context.Context, actor string, in WorkspaceCaptureInput) (models.WorkspaceSnapshotJob, error) {
	if err := validateCaptureInput(actor, in); err != nil || in.Duration > p.Jobs.options.MaxDuration {
		return models.WorkspaceSnapshotJob{}, ErrWorkspaceJobInvalid
	}
	in.Paths, in.IncludeIgnored = slices.Clone(in.Paths), slices.Clone(in.IncludeIgnored)
	slices.Sort(in.Paths)
	slices.Sort(in.IncludeIgnored)
	if prior, exists, err := p.capturePrior(ctx, actor, in); err != nil || exists {
		return prior, err
	}
	if in.ExpectedBinding != "" && in.Binding == nil {
		return models.WorkspaceSnapshotJob{}, ErrWorkspaceJobInvalid
	}
	probe := models.WorkspaceSnapshotJob{ActorID: actor, ScopeKind: in.ScopeKind, ScopeID: in.ScopeID, SandboxID: in.SandboxID, Kind: "capture_files"}
	ctx, err := p.Jobs.prepareJobAuthorization(ctx, probe)
	if err != nil {
		return models.WorkspaceSnapshotJob{}, err
	}
	if err := p.authorize(ctx, p.db.WithContext(ctx), probe); err != nil {
		return models.WorkspaceSnapshotJob{}, err
	}
	remote, err := p.Jobs.options.Remote(ctx, in.SandboxID)
	if err != nil {
		return models.WorkspaceSnapshotJob{}, err
	}
	files, ok := remote.(WorkspaceFileRemote)
	if !ok {
		return models.WorkspaceSnapshotJob{}, ErrWorkspaceJobUnsupported
	}
	caps, err := files.FileCapabilities(ctx)
	if err != nil {
		return models.WorkspaceSnapshotJob{}, err
	}
	if caps.Version != 1 || !caps.Capture || !caps.Read || caps.PolicyVersion != workspacefiles.PolicyVersion || caps.Consistency != "verified_copy" || p.Jobs.verify(ctx, caps.Jobs.Lineage) != nil {
		return models.WorkspaceSnapshotJob{}, ErrWorkspaceJobUnsupported
	}
	body, err := json.Marshal(workspacefiles.Spec{Binding: in.Binding, PolicyVersion: caps.PolicyVersion, PolicySHA256: caps.PolicySHA256, Limits: caps.Limits, Paths: in.Paths, IncludeIgnored: in.IncludeIgnored})
	if err != nil {
		return models.WorkspaceSnapshotJob{}, err
	}
	if in.Binding != nil {
		ctx = context.WithValue(ctx, workspaceCaptureAdmissionKey{}, workspaceCaptureAdmission{p, in.ExpectedBinding, *in.Binding})
	}
	return p.Jobs.Admit(ctx, actor, WorkspaceJobInput{ClientRequestID: in.ClientRequestID, ScopeKind: in.ScopeKind, ScopeID: in.ScopeID, SandboxID: in.SandboxID, Kind: "capture_files", Body: body, Duration: in.Duration})
}

type WorkspaceCaptureOptions struct {
	ResolveBinding    func(context.Context, string, string, string, uint) (WorkspaceActiveBinding, error)
	AdmissionDisabled bool
}

type WorkspaceCaptureService struct {
	db       *gorm.DB
	pipeline *WorkspaceSnapshotPipeline
	catalog  *WorkspaceSnapshotCatalog
	options  WorkspaceCaptureOptions
}

func NewWorkspaceCaptureService(db *gorm.DB, pipeline *WorkspaceSnapshotPipeline, catalog *WorkspaceSnapshotCatalog, options WorkspaceCaptureOptions) (*WorkspaceCaptureService, error) {
	if db == nil || pipeline == nil || catalog == nil || options.ResolveBinding == nil || pipeline.Jobs.options.ReadScope == nil || messageOutsideTransaction(db) != nil {
		return nil, ErrWorkspaceJobAuthority
	}
	return &WorkspaceCaptureService{db: db, pipeline: pipeline, catalog: catalog, options: options}, nil
}

func (s *WorkspaceCaptureService) Admit(ctx context.Context, actor string, in WorkspaceCaptureInput) (WorkspaceSnapshotMetadata, error) {
	if err := s.catalog.AuthorizeScope(ctx, actor, in.ScopeKind, in.ScopeID); err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	if err := validateCaptureInput(actor, in); err != nil || in.ExpectedBinding == "" || in.Binding != nil {
		return WorkspaceSnapshotMetadata{}, ErrWorkspaceJobInvalid
	}
	in.Paths, in.IncludeIgnored = slices.Clone(in.Paths), slices.Clone(in.IncludeIgnored)
	slices.Sort(in.Paths)
	slices.Sort(in.IncludeIgnored)
	if prior, exists, err := s.pipeline.capturePrior(ctx, actor, in); err != nil || exists {
		if err != nil {
			return WorkspaceSnapshotMetadata{}, err
		}
		return s.catalog.Get(ctx, actor, in.ScopeKind, in.ScopeID, prior.ID)
	}
	if s.options.AdmissionDisabled {
		return WorkspaceSnapshotMetadata{}, ErrWorkspaceJobUnsupported
	}
	binding, err := s.options.ResolveBinding(ctx, actor, in.ScopeKind, in.ScopeID, in.SandboxID)
	if err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	if binding.SandboxID != in.SandboxID || binding.Fingerprint != in.ExpectedBinding {
		return WorkspaceSnapshotMetadata{}, ErrWorkspaceJobConflict
	}
	in.Binding = &binding.Reference
	job, err := s.pipeline.Capture(ctx, actor, in)
	if err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	return s.catalog.Get(ctx, actor, in.ScopeKind, in.ScopeID, job.ID)
}

// Reconcile only observes an original dispatched job. It never submits a new
// capture or materializes a directory, including after a lost response.
func (s *WorkspaceCaptureService) Reconcile(ctx context.Context, actor, kind, scope, id string) (WorkspaceSnapshotMetadata, error) {
	if err := s.catalog.AuthorizeScope(ctx, actor, kind, scope); err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	var source struct{ JobID, PublicationJobID string }
	if err := s.db.WithContext(ctx).Table("workspace_snapshots").Select("job_id", "publication_job_id").Take(&source, "id = ? AND actor_id = ? AND scope_kind = ? AND scope_id = ?", id, actor, kind, scope).Error; err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	jobID := source.JobID
	if source.PublicationJobID != "" {
		jobID = source.PublicationJobID
	}
	if _, err := s.pipeline.Jobs.Reconcile(ctx, actor, jobID); err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	return s.catalog.Get(ctx, actor, kind, scope, id)
}

func (s *WorkspaceCaptureService) Resolve(ctx context.Context, actor, kind, scope, nonce string) (WorkspaceSnapshotMetadata, error) {
	if !workspaceJobID.MatchString(nonce) {
		return WorkspaceSnapshotMetadata{}, gorm.ErrRecordNotFound
	}
	return s.catalog.Get(ctx, actor, kind, scope, captureRequestID(actor, kind, scope, nonce))
}
