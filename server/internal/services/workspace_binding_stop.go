package services

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// workspaceStoppedHeadIdentity binds a provider stop observation to the same
// logical root. A legacy missing head is also an identity, not permission to
// clear connections belonging to a subsequently installed head. Aggregate
// fingerprints and timestamps are derived metadata and can change with this
// stop's own desired-state update.
func workspaceStoppedHeadIdentity(ctx context.Context, tx *gorm.DB, sandboxID uint) (string, error) {
	var head models.WorkspaceBindingHead
	err := workspaceCurrentRead(tx.WithContext(ctx)).Select("sandbox_id", "scope_kind", "scope_id", "resource", "lineage", "binding_id", "native_id", "revision", "native_fingerprint", "native_head", "state", "pending_operation_id", "last_operation_id", "released_at").Take(&head, "sandbox_id = ?", sandboxID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	// These fields use json:"-" on the database model, so hash an explicit
	// projection rather than the model's empty JSON representation.
	return bindingDigest(struct {
		ScopeKind, ScopeID, Resource, Lineage, BindingID, NativeID string
		Revision                                                   uint64
		NativeFingerprint, NativeHead, State, Pending, Last        string
		Released                                                   *time.Time
	}{head.ScopeKind, head.ScopeID, head.Resource, head.Lineage, head.BindingID, head.NativeID,
		head.Revision, head.NativeFingerprint, head.NativeHead, head.State, head.PendingOperationID, head.LastOperationID, head.ReleasedAt}), nil
}

// Caller must prove the original physical environment stopped and publish
// under its parent/head and operation fences. A failed or unknown stop never
// reaches this helper. It needs no runtime connection or enabled feature.
func closeStoppedWorkspaceConsumersTx(ctx context.Context, tx *gorm.DB, sandboxID uint) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceJobInvalid
	}
	return tx.WithContext(ctx).Model(&models.WorkspaceBindingUse{}).Where("sandbox_id = ? AND state <> ?", sandboxID, "closed").Update("state", "closed").Error
}
