package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"gorm.io/gorm"
)

func (a *WorkspaceAuthorization) JobOptions(options WorkspaceJobOptions) (WorkspaceJobOptions, error) {
	priorPrepare, priorAuthorize := options.PrepareJobAuthorization, options.Authorize
	priorGuard := options.GuardJobPublication
	priorRead := options.ReadScope
	options.ReadScope = func(ctx context.Context, db *gorm.DB, actor, kind, scope string) error {
		if err := a.ReadScope(ctx, db, actor, kind, scope); err != nil {
			return err
		}
		if priorRead != nil {
			return priorRead(ctx, db, actor, kind, scope)
		}
		return nil
	}
	options.PrepareJobAuthorization = func(ctx context.Context, row models.WorkspaceSnapshotJob) (context.Context, error) {
		var err error
		if priorPrepare != nil {
			ctx, err = priorPrepare(ctx, row)
			if err != nil {
				return ctx, err
			}
		}
		return a.PrepareJob(ctx, row)
	}
	options.Authorize = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if err := a.AuthorizeJob(ctx, tx, row); err != nil {
			return err
		}
		if priorAuthorize != nil {
			return priorAuthorize(ctx, tx, row)
		}
		return nil
	}
	options.GuardJobPublication = func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if err := a.AuthorizeJob(ctx, tx, row); err != nil {
			return err
		}
		if priorGuard != nil {
			return priorGuard(ctx, tx, row)
		}
		return nil
	}
	return WithWorkspaceJobExecution(options, a.execution)
}

func (a *WorkspaceAuthorization) HoldOptions(options payloadhold.Options) (payloadhold.Options, error) {
	priorPrepare, priorAuthorize := options.PrepareHoldAuthorization, options.Authorize
	options.PrepareHoldAuthorization = func(ctx context.Context, id payloadhold.Identity) (context.Context, error) {
		var err error
		if priorPrepare != nil {
			ctx, err = priorPrepare(ctx, id)
			if err != nil {
				return ctx, err
			}
		}
		return a.PrepareHold(ctx, id)
	}
	options.Authorize = func(ctx context.Context, tx *gorm.DB, id payloadhold.Identity) error {
		if err := a.AuthorizeHold(ctx, tx, id); err != nil {
			return err
		}
		if priorAuthorize != nil {
			return priorAuthorize(ctx, tx, id)
		}
		return nil
	}
	return WithPayloadHoldExecution(options, a.execution)
}

// CurrentGrant is used by canonical binding resolution before SQL, never by a
// transaction callback. It resolves all selected P1 credentials without storing
// plaintext and returns only the composite dependency/fingerprint identity.
func (a *WorkspaceAuthorization) CurrentGrant(ctx context.Context, actor, kind, scope string, sandboxID uint) (string, error) {
	if err := messageOutsideTransaction(a.db); err != nil {
		return "", err
	}
	if err := a.ReadScope(ctx, a.db, actor, kind, scope); err != nil {
		return "", err
	}
	resource, err := workspaceResource(a.db.WithContext(ctx), sandboxID)
	if err != nil {
		return "", err
	}
	if kind == "solo" && (resource.Purpose != SandboxPurposeSolo || resource.ThreadID != scope) {
		return "", gorm.ErrRecordNotFound
	}
	if kind == "run" && (resource.Purpose != "run" || resource.RunID != scope) {
		return "", gorm.ErrRecordNotFound
	}
	grant, err := a.prepareGrant(ctx, resource)
	if err != nil {
		return "", err
	}
	if resource.Purpose == SandboxPurposeSolo && len(grant.Fingerprints) != 1 {
		return "", ErrWorkspaceJobConflict
	}
	return authorizationDigest(grant), nil
}
