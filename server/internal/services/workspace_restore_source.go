package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type workspaceRestoreSpec struct {
	Receipt workspacefiles.Receipt `json:"receipt"`
}

func validateWorkspaceRestoreSpec(body json.RawMessage) error {
	var spec workspaceRestoreSpec
	if workspacefiles.Decode(body, &spec) != nil || spec.Receipt.Version != 1 || spec.Receipt.Consistency != "verified_copy" || spec.Receipt.Archive.Name != "workspace.tar.gz" || spec.Receipt.Manifest.Name != "manifest.json" || !restoreDigest(spec.Receipt.Archive.SHA256) || !restoreDigest(spec.Receipt.Manifest.SHA256) || spec.Receipt.Archive.Bytes <= 0 || spec.Receipt.Manifest.Bytes <= 0 || spec.Receipt.Files < 0 || spec.Receipt.Bytes < 0 {
		return ErrWorkspaceJobInvalid
	}
	return nil
}

func restoreDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

// An explicit raw projection has no payload tags or serializers. This method
// is also used under final publication locks: object access is forbidden here.
type workspaceRestoreSource struct {
	ID, ActorID, ScopeKind, ScopeID, RunID, NodeID, JobID, PublicationJobID, Lineage, State, PolicyVersion string
	SourceSandboxID, StateRunID                                                                            uint
	Iteration, RunAttempt, ProviderAttempt                                                                 int
	SourceGeneration                                                                                       uint64
	ArchiveRef, ManifestRef, ArchiveSHA256, ManifestSHA256                                                 string
	Bytes, Entries                                                                                         int64
	VerifiedAt                                                                                             *time.Time
	RetainUntil                                                                                            *time.Time
}

func (v workspaceRestoreSource) snapshot() models.WorkspaceSnapshot {
	return models.WorkspaceSnapshot{ID: v.ID, ActorID: v.ActorID, ScopeKind: v.ScopeKind, ScopeID: v.ScopeID, RunID: v.RunID, StateRunID: v.StateRunID, NodeID: v.NodeID, Iteration: v.Iteration, RunAttempt: v.RunAttempt, ProviderAttempt: v.ProviderAttempt, SourceSandboxID: v.SourceSandboxID, SourceGeneration: v.SourceGeneration, JobID: v.JobID, PublicationJobID: v.PublicationJobID, Lineage: v.Lineage, State: v.State, PolicyVersion: v.PolicyVersion, ArchiveRef: v.ArchiveRef, ManifestRef: v.ManifestRef, ArchiveSHA256: v.ArchiveSHA256, ManifestSHA256: v.ManifestSHA256, Bytes: v.Bytes, Entries: v.Entries, VerifiedAt: v.VerifiedAt, RetainUntil: v.RetainUntil}
}

func (s *WorkspaceRestoreService) source(ctx context.Context, tx *gorm.DB, actor, id string) (models.WorkspaceSnapshot, string, string, string, error) {
	var raw workspaceRestoreSource
	query := tx.Table("workspace_snapshots").Select("id", "actor_id", "scope_kind", "scope_id", "run_id", "state_run_id", "node_id", "iteration", "run_attempt", "provider_attempt", "source_sandbox_id", "source_generation", "job_id", "publication_job_id", "lineage", "state", "policy_version", "archive_ref", "manifest_ref", "archive_sha256", "manifest_sha256", "bytes", "entries", "verified_at", "retain_until").Where("id = ? AND actor_id = ?", id, actor)
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(&raw).Error; err != nil {
		return models.WorkspaceSnapshot{}, "", "", "", err
	}
	snapshot := raw.snapshot()
	if err := s.options.AuthorizeSnapshot(ctx, tx, actor, snapshot); err != nil {
		return snapshot, "", "", "", err
	}
	if raw.State != "ready" || raw.VerifiedAt == nil || raw.PolicyVersion != workspacefiles.PolicyVersion || raw.Bytes < 0 || raw.Bytes > s.limits.MaxUncompressedBytes || raw.Entries < 0 || raw.Entries > s.limits.MaxEntries {
		return snapshot, "", "", "", ErrWorkspaceJobConflict
	}
	for _, artifact := range []struct{ ref, encoding, digest string }{{raw.ArchiveRef, filestore.Binary, raw.ArchiveSHA256}, {raw.ManifestRef, filestore.JSON, raw.ManifestSHA256}} {
		encoding, digest, err := filestore.Parse(artifact.ref)
		if err != nil || encoding != artifact.encoding || !restoreDigest(artifact.digest) || digest != artifact.digest {
			return snapshot, "", "", "", ErrWorkspaceJobConflict
		}
	}
	var capture, publication models.WorkspaceSnapshotJob
	if err := tx.Where("id = ?", raw.JobID).Take(&capture).Error; err != nil {
		return snapshot, "", "", "", err
	}
	if err := tx.Where("id = ?", raw.PublicationJobID).Take(&publication).Error; err != nil {
		return snapshot, "", "", "", err
	}
	var spec workspacefiles.Spec
	var read workspacefiles.ReadSpec
	var receipt workspacefiles.Receipt
	if capture.ActorID != actor || capture.ScopeKind != raw.ScopeKind || capture.ScopeID != raw.ScopeID || capture.SandboxID != raw.SourceSandboxID || capture.Lineage != raw.Lineage || capture.Kind != "capture_files" || capture.State != "executed" || capture.RuntimeGeneration != raw.SourceGeneration || !restoreDigest(capture.RuntimeSHA256) || workspacefiles.ValidateSpec([]byte(capture.Body)) != nil || workspacefiles.Decode([]byte(capture.Body), &spec) != nil || workspacefiles.Decode([]byte(capture.Result), &receipt) != nil {
		return snapshot, "", "", "", ErrWorkspaceJobConflict
	}
	if publication.Kind != "read_snapshot" || publication.State != "executed" || publication.ActorID != actor || publication.ScopeKind != raw.ScopeKind || publication.ScopeID != raw.ScopeID || publication.SandboxID != raw.SourceSandboxID || publication.Lineage != raw.Lineage || workspacefiles.Decode([]byte(publication.Body), &read) != nil || read.SourceJobID != capture.ID || read.SourceSHA256 != capture.RuntimeSHA256 || read.Receipt != receipt {
		return snapshot, "", "", "", ErrWorkspaceJobConflict
	}
	native, _ := json.Marshal(workspaceRestoreSpec{receipt})
	if validateWorkspaceRestoreSpec(native) != nil || receipt.Archive.SHA256 != raw.ArchiveSHA256 || receipt.Manifest.SHA256 != raw.ManifestSHA256 || int64(receipt.Files) != raw.Entries || receipt.Bytes != raw.Bytes || receipt.Archive.Bytes > s.limits.MaxArchiveBytes || receipt.Manifest.Bytes > s.limits.MaxManifestBytes {
		return snapshot, "", "", "", ErrWorkspaceJobConflict
	}
	specJSON, _ := json.Marshal(spec)
	receiptJSON, _ := json.Marshal(receipt)
	raw.RetainUntil = nil // Current retention policy is authorized, not frozen file identity.
	identity, _ := json.Marshal(struct {
		Source  workspaceRestoreSource
		Spec    string
		Receipt string
	}{raw, string(specJSON), string(receiptJSON)})
	digest := sha256.Sum256(identity)
	return snapshot, string(specJSON), string(receiptJSON), hex.EncodeToString(digest[:]), nil
}
