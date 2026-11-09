package services

import (
	"context"
	"strconv"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

func workspaceExecutionEntities(row models.WorkspaceSnapshotJob) ([]executionlineage.Entity, error) {
	entities := []executionlineage.Entity{}
	switch row.ScopeKind {
	case "solo":
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: row.ScopeID}, executionlineage.Entity{Kind: executionlineage.EntityQueue, ID: soloQueueScope(row.ScopeID).ID()})
	case "run":
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityRun, ID: row.ScopeID})
	default:
		return nil, ErrWorkspaceJobInvalid
	}
	if row.ID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityWorkspaceJob, ID: row.ID})
	}
	if row.SandboxID != 0 {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(row.SandboxID), 10)})
	}
	if row.Kind == "read_snapshot" {
		var source workspacefiles.ReadSpec
		if workspacefiles.Decode([]byte(row.Body), &source) != nil || source.SourceJobID == "" {
			return nil, ErrWorkspaceJobInvalid
		}
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityWorkspaceJob, ID: source.SourceJobID})
	}
	return entities, nil
}

func workspaceExecutionParents(db *gorm.DB, row models.WorkspaceSnapshotJob, entities []executionlineage.Entity) ([]executionlineage.Entity, error) {
	if row.ScopeKind == "run" {
		var run models.Run
		if err := db.Select("id", "origin_solo_id").Take(&run, "id = ?", row.ScopeID).Error; err != nil {
			return nil, err
		}
		if run.OriginSoloID != "" {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
		}
	}
	if row.SandboxID != 0 {
		var resource models.Sandbox
		if err := db.Select("id", "purpose", "thread_id", "run_id", "state_run_id").Take(&resource, row.SandboxID).Error; err != nil {
			return nil, err
		}
		if resource.Purpose == SandboxPurposeSolo {
			if resource.ThreadID == "" {
				return nil, executionlineage.ErrConflict
			}
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: resource.ThreadID}, executionlineage.Entity{Kind: executionlineage.EntityQueue, ID: soloQueueScope(resource.ThreadID).ID()})
		}
		if resource.Purpose == "run" {
			if resource.RunID == "" {
				return nil, executionlineage.ErrConflict
			}
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityRun, ID: resource.RunID})
			if resource.StateRunID != 0 {
				entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(resource.StateRunID), 10)})
			}
		}
	}
	return entities, nil
}

// WithWorkspaceJobExecution adds independent execution fencing to an existing
// strict business authorizer. It never substitutes lineage for ownership,
// current credential grants, immutable source checks or physical job fencing.
func WithWorkspaceJobExecution(options WorkspaceJobOptions, execution *executionlineage.Controller) (WorkspaceJobOptions, error) {
	if execution == nil || options.Authorize == nil {
		return options, ErrWorkspaceJobAuthority
	}
	authorize, prepare := options.Authorize, options.PrepareAuthorization
	options.Authority = execution
	options.execution = execution
	options.PrepareAuthorization = func(ctx context.Context) (context.Context, error) {
		var err error
		if prepare != nil {
			ctx, err = prepare(ctx)
			if err != nil {
				return ctx, err
			}
		}
		return execution.PrepareContext(ctx)
	}
	options.Authorize = func(ctx context.Context, db *gorm.DB, row models.WorkspaceSnapshotJob) error {
		entities, err := workspaceExecutionEntities(row)
		if err != nil {
			return err
		}
		if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
			if err = execution.GuardContextTx(db.Statement.Context, db); err == nil {
				entities, err = workspaceExecutionParents(db, row, entities)
			}
			if err == nil {
				err = execution.GuardContextTx(db.Statement.Context, db, entities...)
			}
		} else if execution.HasPreparedContext(ctx) {
			entities, err = workspaceExecutionParents(db, row, entities)
			if err == nil {
				err = execution.CheckPrepared(ctx, db, entities...)
			}
		}
		if err != nil {
			return err
		}
		return authorize(ctx, db, row)
	}
	return options, nil
}

// guardExecutionPublication rechecks the original private proof immediately
// before commit. It performs no external reads and cannot extend an expired
// permit while waiting for a row lock.
func (s *WorkspaceSnapshotJobs) guardExecutionPublication(tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
	if s.options.execution == nil {
		return nil
	}
	entities, err := workspaceExecutionEntities(row)
	if err != nil {
		return err
	}
	entities, err = workspaceExecutionParents(tx, row, entities)
	if err != nil {
		return err
	}
	return s.options.execution.GuardContextTx(tx.Statement.Context, tx, entities...)
}

func (s *WorkspaceSnapshotJobs) metadataTransaction(ctx context.Context, row models.WorkspaceSnapshotJob, body func(*gorm.DB) error) error {
	if err := s.verify(ctx, row.Lineage); err != nil {
		return err
	}
	ctx, err := s.prepareAuthorization(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.options.Authorize(ctx, tx, row); err != nil {
			return err
		}
		if err := body(tx); err != nil {
			return err
		}
		return s.guardExecutionPublication(tx, row)
	})
}

// WithPayloadHoldExecution keeps the upload protocol's ordinary ownership
// callback and adds a fresh private permit to each short metadata publication.
// The snapshot service still commits pin+ready+final job in one outer TX.
func WithPayloadHoldExecution(options payloadhold.Options, execution *executionlineage.Controller) (payloadhold.Options, error) {
	if execution == nil || options.Authorize == nil {
		return options, ErrWorkspaceJobAuthority
	}
	authorize, prepare := options.Authorize, options.PrepareAuthorization
	options.Authority = execution
	options.PrepareAuthorization = func(ctx context.Context) (context.Context, error) {
		var err error
		if prepare != nil {
			ctx, err = prepare(ctx)
			if err != nil {
				return ctx, err
			}
		}
		return execution.PrepareContext(ctx)
	}
	options.Authorize = func(ctx context.Context, db *gorm.DB, id payloadhold.Identity) error {
		entities, err := workspaceExecutionEntities(models.WorkspaceSnapshotJob{ID: id.OperationID, ScopeKind: id.ScopeKind, ScopeID: id.ScopeID})
		if err != nil {
			return err
		}
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityHold, ID: payloadhold.HoldID(id)})
		if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
			err = execution.GuardContextTx(db.Statement.Context, db, entities...)
		} else if execution.HasPreparedContext(ctx) {
			err = execution.CheckPrepared(ctx, db, entities...)
		}
		if err != nil {
			return err
		}
		return authorize(ctx, db, id)
	}
	return options, nil
}
