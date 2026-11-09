package services

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// These explicit projections intentionally have no json:"-" or payload
// serializer. A digest of the public model DTO would omit the actual identity.
type bindingReleaseHead struct {
	SandboxID                                                                                                                                                                    uint
	ScopeKind, ScopeID, Resource, Lineage, BindingID, NativeID, NativeFingerprint, Fingerprint, NativeHead, AuthorizationFingerprint, State, PendingOperationID, LastOperationID string
	Revision, LifecycleEpoch                                                                                                                                                     uint64
	LifecycleAttempt                                                                                                                                                             string
	ReleasedAt                                                                                                                                                                   *time.Time
}
type bindingReleaseRoot struct {
	ID, ActorID, SoloID, Resource, Lineage, NativeID, NativeFingerprint, NativeHead, SelectorKind, SelectorID, ParentBindingID, RestoreID, SnapshotID, State string
	SandboxID                                                                                                                                                uint
	Revision                                                                                                                                                 uint64
	ReleasedAt                                                                                                                                               *time.Time
}
type bindingReleaseReturn struct {
	ID, ActorID, SoloID, OperationID, PreviousBindingID, LeavingBindingID, SnapshotID, State string
	SandboxID                                                                                uint
}
type bindingReleaseUse struct {
	ID, BindingID, Kind, Owner, State string
	SandboxID                         uint
}
type bindingReleaseSnapshot struct {
	ID, State, JobID, PublicationJobID, Lineage, ArchiveRef, ManifestRef, ArchiveSHA256, ManifestSHA256, SourceBindingFingerprint string
	VerifiedAt                                                                                                                    *time.Time
}
type bindingReleaseOperation struct{ ID, JobID, Action, State, PreviousBindingID, TargetBindingID, RestoreID, ReturnSnapshotID, SourceIdentity, Fingerprint string }

func (s *WorkspaceBindingService) ReleaseScope(ctx context.Context, tx *gorm.DB, soloID string, sandboxID uint) (WorkspaceReleaseBindingScope, error) {
	var out WorkspaceReleaseBindingScope
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return out, ErrWorkspaceJobInvalid
	}
	current := func() *gorm.DB { return workspaceCurrentRead(tx.WithContext(ctx)) }
	var head bindingReleaseHead
	err := current().Table("workspace_binding_heads").Take(&head, "sandbox_id = ? AND scope_kind = 'solo' AND scope_id = ?", sandboxID, soloID).Error
	legacy := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !legacy {
		return out, err
	}
	if !legacy && (head.PendingOperationID != "" || head.State == "pending") {
		out.Blockers = append(out.Blockers, "workspace_switch_pending")
	}
	var roots []bindingReleaseRoot
	if err := current().Table("workspace_bindings").Where("sandbox_id = ? AND solo_id = ?", sandboxID, soloID).Order("id").Find(&roots).Error; err != nil {
		return out, err
	}
	var returns []bindingReleaseReturn
	if err := current().Table("workspace_return_points").Where("sandbox_id = ? AND solo_id = ?", sandboxID, soloID).Order("id").Find(&returns).Error; err != nil {
		return out, err
	}
	var uses []bindingReleaseUse
	if err := current().Table("workspace_binding_uses").Where("sandbox_id = ? AND state <> ?", sandboxID, "closed").Order("id").Find(&uses).Error; err != nil {
		return out, err
	}
	if len(uses) > 0 {
		out.Blockers = append(out.Blockers, "workspace_consumers_busy")
	}
	var operations []bindingReleaseOperation
	if err := current().Table("workspace_binding_operations").Where("sandbox_id = ? AND solo_id = ?", sandboxID, soloID).Order("id").Find(&operations).Error; err != nil {
		return out, err
	}
	for _, op := range operations {
		if op.State != "activated" && op.State != "returned" && op.State != "failed" && op.State != "cancelled" {
			out.Blockers = append(out.Blockers, "workspace_switch_pending")
			break
		}
	}
	ids := []string{}
	for _, root := range roots {
		if root.SnapshotID != "" {
			ids = append(ids, root.SnapshotID)
		}
	}
	for _, point := range returns {
		if point.SnapshotID != "" {
			ids = append(ids, point.SnapshotID)
		}
	}
	for _, op := range operations {
		if op.ReturnSnapshotID != "" {
			ids = append(ids, op.ReturnSnapshotID)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var snapshots []bindingReleaseSnapshot
	if len(ids) > 0 {
		if err := current().Table("workspace_snapshots").Select("id", "state", "job_id", "publication_job_id", "lineage", "archive_ref", "manifest_ref", "archive_sha256", "manifest_sha256", "source_binding_fingerprint", "verified_at").Where("id IN ?", ids).Order("id").Find(&snapshots).Error; err != nil {
			return out, err
		}
	}
	out.Bindings = len(roots)
	if legacy && out.Bindings == 0 {
		out.Bindings = 1
	}
	out.ReturnPoints = len(returns)
	for _, snapshot := range snapshots {
		if snapshot.State == "ready" && snapshot.VerifiedAt != nil {
			out.SnapshotIDs = append(out.SnapshotIDs, snapshot.ID)
		}
	}
	slices.Sort(out.Blockers)
	out.Blockers = slices.Compact(out.Blockers)
	out.Fingerprint = bindingDigest(struct {
		SoloID     string
		SandboxID  uint
		Legacy     bool
		Head       bindingReleaseHead
		Roots      []bindingReleaseRoot
		Returns    []bindingReleaseReturn
		Uses       []bindingReleaseUse
		Operations []bindingReleaseOperation
		Snapshots  []bindingReleaseSnapshot
	}{soloID, sandboxID, legacy, head, roots, returns, uses, operations, snapshots})
	return out, nil
}

func (s *WorkspaceBindingService) ReleasedTx(ctx context.Context, tx *gorm.DB, release models.WorkspaceRelease) error {
	scope, err := s.ReleaseScope(ctx, tx, release.SoloID, release.SandboxID)
	if err != nil {
		return err
	}
	if scope.Fingerprint != release.BindingsFingerprint || len(scope.Blockers) > 0 {
		return ErrWorkspaceReleaseConflict
	}
	now := time.Now().UTC()
	if err := tx.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ? AND pending_operation_id = ''", release.SandboxID).Updates(map[string]any{"state": "released", "released_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&models.WorkspaceBinding{}).Where("sandbox_id = ? AND solo_id = ?", release.SandboxID, release.SoloID).Updates(map[string]any{"state": "released", "released_at": now}).Error
}

// The caller must first prove the exact physical environment has stopped.
// Browser flags or an expired server connection lease are not such proof.
func (s *WorkspaceBindingService) EnvironmentStoppedTx(ctx context.Context, tx *gorm.DB, sandboxID uint) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceJobInvalid
	}
	return tx.WithContext(ctx).Model(&models.WorkspaceBindingUse{}).Where("sandbox_id = ? AND state <> ?", sandboxID, "closed").Update("state", "closed").Error
}
