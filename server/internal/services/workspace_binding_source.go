package services

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

type workspaceBindingAdmissionKey struct{}
type workspaceBindingAdmission struct {
	service *WorkspaceBindingService
	row     models.WorkspaceBindingOperation
}
type workspaceBindingAcceptedKey struct{}
type workspaceBindingAccepted struct {
	service                            *WorkspaceBindingService
	operationID, jobIdentity, expected string
	expires                            time.Time
}

func bindingKind(action string) string {
	if action == "activate" {
		return "activate_workspace"
	}
	if action == "return" {
		return "return_workspace"
	}
	return ""
}
func bindingJobMatches(op models.WorkspaceBindingOperation, job models.WorkspaceSnapshotJob) bool {
	return op.ID == job.ID && op.JobID == job.ID && op.ActorID == job.ActorID && op.SoloID == job.ScopeID && job.ScopeKind == "solo" && op.SandboxID == job.SandboxID && bindingKind(op.Action) == job.Kind && op.NativeSpec == job.Body && op.ClientNonce == job.Nonce && op.Lineage == job.Lineage
}

func bindingOperationMetadata(ctx context.Context, db *gorm.DB, job models.WorkspaceSnapshotJob) (models.WorkspaceBindingOperation, error) {
	if admission, ok := ctx.Value(workspaceBindingAdmissionKey{}).(workspaceBindingAdmission); ok && admission.service != nil && bindingJobMatches(admission.row, job) {
		return admission.row, nil
	}
	var row models.WorkspaceBindingOperation
	if err := workspaceCurrentRead(db.WithContext(ctx)).Take(&row, "id = ? AND actor_id = ? AND solo_id = ?", job.ID, job.ActorID, job.ScopeID).Error; err != nil {
		return row, err
	}
	if !bindingJobMatches(row, job) {
		return row, ErrWorkspaceJobConflict
	}
	return row, nil
}

// WorkspaceBindingJobSources is the strict business factory's metadata-only
// source adapter. It supports only this service's full original operation;
// generic low-level job admission cannot invent its source ownership.
func WorkspaceBindingJobSources(ctx context.Context, db *gorm.DB, job models.WorkspaceSnapshotJob) ([]models.WorkspaceSnapshot, error) {
	if job.Kind != "activate_workspace" && job.Kind != "return_workspace" {
		return nil, ErrWorkspaceJobUnsupported
	}
	op, err := bindingOperationMetadata(ctx, db, job)
	if err != nil {
		return nil, err
	}
	var spec sandbox.WorkspaceBindingSpec
	if workspacefiles.Decode([]byte(op.NativeSpec), &spec) != nil || spec.Version != 1 || spec.ReturnSnapshotID != op.ReturnSnapshotID || spec.Authorization != op.AuthorizationFingerprint || !sandbox.ValidWorkspaceBindingReference(spec.Expected) {
		return nil, ErrWorkspaceJobConflict
	}
	ids := []string{op.ReturnSnapshotID}
	if op.Action == "activate" {
		var restore models.WorkspaceRestore
		if err := workspaceCurrentRead(db).Take(&restore, "id = ? AND actor_id = ? AND solo_id = ?", op.RestoreID, op.ActorID, op.SoloID).Error; err != nil {
			return nil, err
		}
		if restore.State != "restored" || restore.TargetSandboxID != op.SandboxID || restore.Lineage != op.Lineage || restore.Destination == "" || restore.Destination != restore.JobID || spec.Target.Kind != "restore" || spec.Target.ID != restore.JobID {
			return nil, ErrWorkspaceJobConflict
		}
		ids = append(ids, restore.SnapshotID)
	} else {
		var target models.WorkspaceBinding
		if err := workspaceCurrentRead(db).Take(&target, "id = ? AND actor_id = ? AND solo_id = ? AND sandbox_id = ?", op.TargetBindingID, op.ActorID, op.SoloID, op.SandboxID).Error; err != nil {
			return nil, err
		}
		if target.State == "released" || target.Lineage != op.Lineage || spec.Target.Kind != "binding" || spec.Target.ID != target.NativeID || target.SnapshotID == "" {
			return nil, ErrWorkspaceJobConflict
		}
		ids = append(ids, target.SnapshotID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var sources []models.WorkspaceSnapshot
	for _, id := range ids {
		// Explicit columns do not select serialized object payloads.
		var source models.WorkspaceSnapshot
		if err := workspaceCurrentRead(db).Select("id", "actor_id", "scope_kind", "scope_id", "state", "source_sandbox_id", "source_binding_fingerprint", "job_id", "lineage", "verified_at").Take(&source, "id = ?", id).Error; err != nil {
			return nil, err
		}
		if source.State != "ready" || source.VerifiedAt == nil {
			return nil, ErrWorkspaceJobConflict
		}
		if id == op.ReturnSnapshotID && (source.ActorID != op.ActorID || source.ScopeKind != "solo" || source.ScopeID != op.SoloID || source.SourceSandboxID != op.SandboxID || source.SourceBindingFingerprint != op.ExpectedBinding) {
			return nil, ErrWorkspaceJobConflict
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func (s *WorkspaceBindingService) bindingSourcesIdentity(ctx context.Context, db *gorm.DB, op models.WorkspaceBindingOperation) (string, error) {
	operations := s.operations.Load()
	if operations == nil || operations.restore == nil {
		return "", ErrWorkspaceJobUnsupported
	}
	job := models.WorkspaceSnapshotJob{ID: op.JobID, ActorID: op.ActorID, ScopeKind: "solo", ScopeID: op.SoloID, SandboxID: op.SandboxID, Kind: bindingKind(op.Action), Body: op.NativeSpec, Nonce: op.ClientNonce, Lineage: op.Lineage}
	ctx = context.WithValue(ctx, workspaceBindingAdmissionKey{}, workspaceBindingAdmission{s, op})
	sources, err := WorkspaceBindingJobSources(ctx, db, job)
	if err != nil {
		return "", err
	}
	type identity struct{ ID, Digest, Binding string }
	values := make([]identity, 0, len(sources))
	for _, source := range sources {
		_, specJSON, _, digest, err := operations.restore.source(ctx, db, op.ActorID, source.ID)
		if err != nil {
			return "", err
		}
		if digest == "" {
			return "", ErrWorkspaceJobConflict
		}
		if source.ID == op.ReturnSnapshotID {
			var capture workspacefiles.Spec
			var expected sandbox.WorkspaceBindingSpec
			if workspacefiles.Decode([]byte(specJSON), &capture) != nil || workspacefiles.Decode([]byte(op.NativeSpec), &expected) != nil || capture.Binding == nil || *capture.Binding != expected.Expected {
				return "", ErrWorkspaceJobConflict
			}
		}
		values = append(values, identity{source.ID, digest, source.SourceBindingFingerprint})
	}
	if len(values) == 0 {
		return "", errors.New("workspace source proof missing")
	}
	return bindingDigest(values), nil
}
