package main

import (
	"context"
	"strconv"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

// Construction is deliberately before Engine.Start, while callbacks resolve
// only the fully installed production factory. No temporary allow callback is
// used to break the Engine/Solo composition cycle.
func newProductionWorkflowCheckpoints(db *gorm.DB, execution *executionlineage.Controller, limits config.WorkspaceOperationsConfig, workspace *productionWorkspaceServices) (*services.WorkflowWorkspaceCheckpoints, error) {
	duration, err := time.ParseDuration(limits.MaxDuration)
	if err != nil {
		return nil, err
	}
	return services.NewWorkflowWorkspaceCheckpoints(db, services.WorkflowCheckpointOptions{Enabled: *limits.Enabled, Execution: execution, AccountExists: currentWorkspaceAccount, MaxDuration: duration, ReadScope: func(ctx context.Context, db *gorm.DB, actor, kind, id string) error {
		if workspace.Authorization == nil {
			return services.ErrWorkspaceJobUnsupported
		}
		return workspace.Authorization.ReadScope(ctx, db, actor, kind, id)
	}, ResolveBinding: func(ctx context.Context, actor, kind, id string, sandboxID uint) (services.WorkspaceActiveBinding, error) {
		if workspace.Bindings == nil {
			return services.WorkspaceActiveBinding{}, services.ErrWorkspaceJobUnsupported
		}
		return workspace.Bindings.ResolveSandbox(ctx, actor, kind, id, sandboxID)
	}, Capture: func(ctx context.Context, actor string, input services.WorkspaceCaptureInput) (services.WorkspaceSnapshotMetadata, error) {
		if workspace.Capture == nil {
			return services.WorkspaceSnapshotMetadata{}, services.ErrWorkspaceJobUnsupported
		}
		return workspace.Capture.Admit(ctx, actor, input)
	}, Reconcile: func(ctx context.Context, actor, kind, scope, id string) (services.WorkspaceSnapshotMetadata, error) {
		if workspace.Capture == nil {
			return services.WorkspaceSnapshotMetadata{}, services.ErrWorkspaceJobUnsupported
		}
		return workspace.Capture.Reconcile(ctx, actor, kind, scope, id)
	}})
}

func attachProductionWorkspaceOperations(db *gorm.DB, solo *services.SoloService, sbx *services.SandboxService, execution *executionlineage.Controller, runtimeConfig *sandbox.WorkspaceRuntimeConfig, workspace *productionWorkspaceServices) error {
	if workspace.Bindings == nil || runtimeConfig == nil {
		return nil
	}
	if err := workspace.Bindings.AttachOperations(workspace.Restore, solo); err != nil {
		return err
	}
	releases, err := services.NewWorkspaceReleaseService(db, solo, sbx, services.WorkspaceReleaseOptions{Lineage: runtimeConfig.Lineage, Prepare: execution.PrepareContext, Bindings: workspace.Bindings, GuardTx: func(ctx context.Context, tx *gorm.DB, row models.WorkspaceRelease) error {
		if !currentWorkspaceAccount(row.ActorID) {
			return gorm.ErrRecordNotFound
		}
		if row.SoloID == "" || row.SandboxID == 0 {
			return services.ErrWorkspaceReleaseInvalid
		}
		entities := []executionlineage.Entity{{Kind: executionlineage.EntitySolo, ID: row.SoloID}, {Kind: executionlineage.EntityQueue, ID: (chatsession.QueueScope{Surface: "solo", Key: row.SoloID}).ID()}, {Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(row.SandboxID), 10)}}
		if err := execution.GuardContextTx(ctx, tx, entities...); err != nil {
			return err
		}
		// This is a read preflight, not a CAS. The release repository itself
		// locks Compute→Queue→Solo→canonical metadata→Sandbox and rechecks the
		// exact lifecycle tuple in prepareScope before each physical change.
		// Avoid taking an application-row lock before that established order.
		var parent struct{ ID string }
		return tx.Table("solo_sessions").Select("id").Take(&parent, "id = ? AND user_id = ?", row.SoloID, row.ActorID).Error
	}})
	if err != nil {
		return err
	}
	workspace.Releases = releases
	return nil
}
