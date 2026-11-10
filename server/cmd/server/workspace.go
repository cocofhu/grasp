package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

type productionWorkspaceServices struct {
	Authorization *services.WorkspaceAuthorization
	Bindings      *services.WorkspaceBindingService
	Catalog       *services.WorkspaceSnapshotCatalog
	Capture       *services.WorkspaceCaptureService
	Restore       *services.WorkspaceRestoreService
	Releases      *services.WorkspaceReleaseService
}

func workspaceRuntimeConfiguration(ctx context.Context, limits config.WorkspaceOperationsConfig, execution *executionlineage.Controller) (*sandbox.WorkspaceRuntimeConfig, error) {
	if execution.DatasetID() == "" {
		return nil, nil
	}
	lineage, err := execution.Current(ctx)
	if err != nil {
		return nil, err
	}
	return &sandbox.WorkspaceRuntimeConfig{Lineage: lineage, PrivateRoot: limits.PrivateRoot, Limits: sandbox.WorkspaceFileLimits{MaxFileBytes: limits.MaxFileBytes, MaxBytes: limits.MaxUncompressedBytes - limits.MaxGitBytes, MaxGitBytes: limits.MaxGitBytes, MaxArchiveBytes: limits.MaxArchiveBytes, MaxManifestBytes: limits.MaxManifestBytes, MaxSpoolBytes: limits.MaxSpoolBytes, MaxEntries: int(limits.MaxEntries), MaxRepositories: 64, MaxScanEntries: 500000}}, nil
}

func currentWorkspaceAccount(actor string) bool {
	current := config.GetConfig()
	if current == nil {
		return false
	}
	for _, account := range current.Auth.Users {
		if account.Username == actor && account.PasswordHash != "" {
			return true
		}
	}
	return false
}

func workspaceLineageGuard(execution *executionlineage.Controller) func(context.Context, *gorm.DB, string, string, uint) error {
	return func(ctx context.Context, tx *gorm.DB, kind, scope string, sandboxID uint) error {
		if err := execution.GuardContextTx(ctx, tx); err != nil {
			return err
		}
		entities := []executionlineage.Entity{{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(sandboxID), 10)}}
		switch kind {
		case "solo":
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: scope}, executionlineage.Entity{Kind: executionlineage.EntityQueue, ID: (chatsession.QueueScope{Surface: "solo", Key: scope}).ID()})
		case "run":
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityRun, ID: scope})
			var run models.Run
			if err := tx.Select("id", "origin_solo_id").Take(&run, "id = ?", scope).Error; err != nil {
				return err
			}
			if run.OriginSoloID != "" {
				entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
			}
		default:
			return services.ErrWorkspaceJobInvalid
		}
		if err := execution.GuardContextTx(tx.Statement.Context, tx, entities...); err != nil {
			return err
		}
		return services.GuardWorkspaceReleaseTx(ctx, tx, sandboxID)
	}
}

// A workspace worker owns native file operations which can outlive one SQL
// transaction. Stop must join its cancellation before SQL runtime presence is
// removed; sql.DB drain alone cannot prove that these publishers have stopped.
type workspaceBackgroundWorker struct {
	cancel context.CancelFunc
	done   chan error
}

func startWorkspaceWorker(parent context.Context, run func(context.Context) error) *workspaceBackgroundWorker {
	ctx, cancel := context.WithCancel(parent)
	worker := &workspaceBackgroundWorker{cancel: cancel, done: make(chan error, 1)}
	if run == nil {
		worker.done <- nil
	} else {
		go func() { worker.done <- run(ctx) }()
	}
	return worker
}

func (w *workspaceBackgroundWorker) stop(ctx context.Context) error {
	w.cancel()
	select {
	case err := <-w.done:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newProductionWorkspaceServices(db *gorm.DB, sbx *services.SandboxService, storage *payloadstore.Storage, execution *executionlineage.Controller, limits config.WorkspaceOperationsConfig, runtimeConfig *sandbox.WorkspaceRuntimeConfig) (productionWorkspaceServices, error) {
	var result productionWorkspaceServices
	var bindings *services.WorkspaceBindingService
	// These closures are required even when execution is unavailable. They
	// refuse new work, while metadata scope authorization remains usable.
	authorization, err := services.NewWorkspaceAuthorization(db, sbx, execution, services.WorkspaceAuthorizationOptions{
		AccountExists: currentWorkspaceAccount,
		ResolveBinding: func(ctx context.Context, actor, kind, scope string, id uint) (services.WorkspaceRestoreBinding, error) {
			if bindings == nil || runtimeConfig == nil {
				return services.WorkspaceRestoreBinding{}, services.ErrWorkspaceJobUnsupported
			}
			resolved, err := bindings.ResolveSandbox(ctx, actor, kind, scope, id)
			if err != nil {
				return services.WorkspaceRestoreBinding{}, err
			}
			if resolved.Lineage != runtimeConfig.Lineage {
				return services.WorkspaceRestoreBinding{}, services.ErrWorkspaceJobUnsupported
			}
			return services.WorkspaceRestoreBinding{SandboxID: resolved.SandboxID, Fingerprint: resolved.Fingerprint}, nil
		},
		CurrentBinding: func(ctx context.Context, tx *gorm.DB, actor, kind, scope string, id uint) (services.WorkspaceRestoreBinding, error) {
			if bindings == nil || runtimeConfig == nil {
				return services.WorkspaceRestoreBinding{}, services.ErrWorkspaceJobUnsupported
			}
			return bindings.CurrentSandboxBinding(ctx, tx, actor, kind, scope, id)
		},
	})
	if err != nil {
		return result, err
	}
	result.Authorization = authorization
	if runtimeConfig != nil {
		bindings, err = services.NewWorkspaceBindingService(db, sbx, services.WorkspaceBindingOptions{ExpectedLineage: runtimeConfig.Lineage, FileOperationsEnabled: *limits.Enabled, Prepare: execution.PrepareContext, ReadScope: authorization.ReadScope, CurrentAuthorization: authorization.CurrentGrant, GuardTx: workspaceLineageGuard(execution)})
		if err != nil {
			return result, err
		}
		sbx.SetWorkspaceBindings(bindings)
		result.Bindings = bindings
	}
	result.Catalog, err = services.NewWorkspaceSnapshotCatalog(db, authorization.ReadScope)
	if err != nil {
		return result, err
	}
	payloadLimits := payloadhold.Limits{MaxFileBytes: limits.MaxFileBytes, MaxUncompressedBytes: limits.MaxUncompressedBytes, MaxArchiveBytes: limits.MaxArchiveBytes, MaxManifestBytes: limits.MaxManifestBytes, MaxEntries: limits.MaxEntries}
	duration, err := time.ParseDuration(limits.MaxDuration)
	if err != nil {
		return result, err
	}
	holdOptions, err := authorization.HoldOptions(payloadhold.Options{StorageFingerprint: storage.Location.Fingerprint, Limits: payloadLimits, MaxReservedBytes: limits.MaxReservedBytes, MaxScopeReservedBytes: limits.MaxScopeReservedBytes, MaxDuration: duration})
	if err != nil {
		return result, err
	}
	holds, err := payloadhold.New(db, storage.Files, holdOptions)
	if err != nil {
		return result, err
	}
	jobs, err := authorization.JobOptions(services.WorkspaceJobOptions{MaxWorkers: limits.MaxWorkers, MaxDuration: duration, PollInterval: 2 * time.Second})
	if err != nil {
		return result, err
	}
	if bindings != nil {
		jobs, err = bindings.JobOptions(jobs)
		if err != nil {
			return result, err
		}
	}
	restoreOptions := authorization.RestoreOptions()
	restoreOptions.AdmissionDisabled = !*limits.Enabled
	result.Restore, err = services.NewWorkspaceRestoreService(db, sbx, holds, storage.Files, payloadLimits, jobs, restoreOptions)
	if err != nil {
		return result, err
	}
	result.Capture, err = services.NewWorkspaceCaptureService(db, result.Restore.Pipeline, result.Catalog, services.WorkspaceCaptureOptions{AdmissionDisabled: !*limits.Enabled, ResolveBinding: func(ctx context.Context, actor, kind, scope string, sandboxID uint) (services.WorkspaceActiveBinding, error) {
		if bindings == nil || runtimeConfig == nil {
			return services.WorkspaceActiveBinding{}, services.ErrWorkspaceJobUnsupported
		}
		return bindings.ResolveSandbox(ctx, actor, kind, scope, sandboxID)
	}})
	return result, err
}
