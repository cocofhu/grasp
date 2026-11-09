package services

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrWorkspaceReleaseInvalid     = errors.New("invalid environment release")
	ErrWorkspaceReleaseConflict    = errors.New("environment release scope changed")
	ErrWorkspaceReleaseBlocked     = errors.New("environment release has active dependencies")
	ErrWorkspaceReleaseUnsupported = errors.New("verified environment release unsupported")
	ErrWorkspaceReleaseUnknown     = errors.New("environment release result unconfirmed")
)

// WorkspaceReleaseUnsupportedError has a closed, non-sensitive product reason.
type WorkspaceReleaseUnsupportedError struct{ Reason string }

func (e *WorkspaceReleaseUnsupportedError) Error() string {
	return ErrWorkspaceReleaseUnsupported.Error()
}
func (e *WorkspaceReleaseUnsupportedError) Unwrap() error { return ErrWorkspaceReleaseUnsupported }
func workspaceReleaseUnsupported(reason string) error {
	switch reason {
	case "driver_not_supported", "resource_metadata_unavailable", "execution_lineage_unavailable", "provider_control_unavailable", "gateway_protocol_unavailable", "physical_storage_unsupported":
	default:
		reason = "driver_not_supported"
	}
	return &WorkspaceReleaseUnsupportedError{Reason: reason}
}

// Private scope metadata includes all canonical roots and independent Returns.
// A ready selected-directory snapshot is never proof of whole-disk coverage.
type WorkspaceReleaseBindingScope struct {
	Fingerprint  string
	Bindings     int
	ReturnPoints int
	SnapshotIDs  []string
	Blockers     []string
}
type WorkspaceReleaseBindings interface {
	ReleaseScope(context.Context, *gorm.DB, string, uint) (WorkspaceReleaseBindingScope, error)
	ReleasedTx(context.Context, *gorm.DB, models.WorkspaceRelease) error
}
type WorkspaceReleaseOptions struct {
	Lineage      string
	Prepare      func(context.Context) (context.Context, error)
	GuardTx      func(context.Context, *gorm.DB, models.WorkspaceRelease) error
	Bindings     WorkspaceReleaseBindings
	MaxDuration  time.Duration
	PollInterval time.Duration
	Provider     WorkspaceReleaseProvider
}
type WorkspaceReleasePreviewInput struct {
	ClientRequestID string `json:"clientRequestId"`
}
type WorkspaceReleaseConfirmInput struct {
	ClientRequestID string `json:"clientRequestId"`
	Confirmed       bool   `json:"confirmed"`
	LossAccepted    bool   `json:"lossAccepted"`
}
type WorkspaceReleaseView struct {
	ID                       string    `json:"id"`
	State                    string    `json:"state"`
	WholeEnvironment         bool      `json:"wholeEnvironment"`
	ChatsRetained            bool      `json:"chatsRetained"`
	SnapshotsRetained        int       `json:"snapshotsRetained"`
	WorkspacesAffected       int       `json:"workspacesAffected"`
	ReturnPointsRetained     int       `json:"returnPointsRetained"`
	LossConfirmationRequired bool      `json:"lossConfirmationRequired"`
	RequiresReconciliation   bool      `json:"requiresReconciliation"`
	Blockers                 []string  `json:"blockers,omitempty"`
	Error                    string    `json:"error,omitempty"`
	CreatedAt                time.Time `json:"createdAt"`
}
type WorkspaceReleaseProvider interface {
	Capabilities(context.Context, string) (sandbox.EnvironmentReleaseCapabilities, error)
	Preview(context.Context, string, sandbox.EnvironmentReleasePrepare) (sandbox.EnvironmentReleaseView, error)
	Confirm(context.Context, string, string, sandbox.EnvironmentReleaseConfirm) (sandbox.EnvironmentReleaseView, error)
	Renew(context.Context, string, string, sandbox.EnvironmentReleaseRenew) (sandbox.EnvironmentReleaseView, error)
	Get(context.Context, string, string) (sandbox.EnvironmentReleaseView, error)
}

type workspaceReleaseCapabilityKey struct{}
type workspaceReleaseCapability struct {
	ID        string
	SandboxID uint
}

// GuardWorkspaceReleaseTx is composed by all physical/job admission paths.
// Receipt reads bypass no guard; only this service creates the private context
// that may reconcile the same already-confirmed release.
func GuardWorkspaceReleaseTx(ctx context.Context, tx *gorm.DB, sandboxID uint) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrWorkspaceReleaseInvalid
	}
	// The shared physical row serializes this current read with Confirm. A
	// plain SELECT would be stale under MySQL repeatable-read isolation.
	var physical models.Sandbox
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Take(&physical, sandboxID).Error; err != nil {
		return err
	}
	var rows []models.WorkspaceRelease
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "sandbox_id").Where("sandbox_id = ? AND state IN ?", sandboxID, []string{"confirmed", "dispatching", "unknown", "released"}).Find(&rows).Error; err != nil {
		return err
	}
	allowed, _ := ctx.Value(workspaceReleaseCapabilityKey{}).(workspaceReleaseCapability)
	for _, row := range rows {
		if row.ID != allowed.ID || row.SandboxID != allowed.SandboxID {
			return ErrWorkspaceReleaseBlocked
		}
	}
	return nil
}
