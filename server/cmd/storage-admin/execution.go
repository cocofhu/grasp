package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const executionTicketFile = "execution-ticket.json"

type initialization struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
}

func lockExecutionWorkdir(dir string, create bool) (*os.File, error) {
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("execution recovery requires a private workdir (mode 0700)")
	}
	file, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.New("execution recovery workdir is already in use")
	}
	return file, nil
}

func openExecutionControl(ctx context.Context, opts options, cfg *config.Config) (*executionlineage.Authority, error) {
	control, err := loadTarget(opts.executionControl)
	if err != nil {
		return nil, errors.New("could not load independent execution control YAML")
	}
	payloads := []config.StorageConfig{cfg.Storage}
	if opts.target != "" {
		target, err := loadTarget(opts.target)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, target)
	}
	protected := []string{opts.workdir}
	if cfg.Database.Driver != "mysql" {
		if cfg.Database.Path == ":memory:" || strings.HasPrefix(cfg.Database.Path, "file:") || cfg.Database.Path == "" {
			return nil, errors.New("execution recovery requires a persistent SQL database")
		}
		protected = append(protected, filepath.Dir(cfg.Database.Path))
	}
	return executionlineage.OpenConfigured(ctx, control, payloads, protected)
}

func stoppedRuntimes(opts options) []string {
	if opts.stopped == "" {
		return nil
	}
	return strings.Split(opts.stopped, ",")
}

func initializeExecution(ctx context.Context, opts options, cfg *config.Config, output io.Writer) error {
	lock, err := lockExecutionWorkdir(opts.workdir, true)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	file := filepath.Join(opts.workdir, "execution-initialization.json")
	operation := initialization{Version: 1, ID: uuid.NewString()}
	raw, err := os.ReadFile(file)
	existingInitialization := err == nil
	if err == nil {
		if json.Unmarshal(raw, &operation) != nil || operation.Version != 1 || uuid.Validate(operation.ID) != nil {
			return errors.New("invalid execution initialization checkpoint")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	operation, err = initializationAfterUpgrade(ctx, opts, cfg, operation, existingInitialization)
	if err != nil {
		return err
	}
	if !existingInitialization {
		if err := writeJSON(file, operation); err != nil {
			return err
		}
	}
	authority, err := openExecutionControl(ctx, opts, cfg)
	if err != nil {
		return err
	}
	ticket, err := authority.Initialize(ctx, operation.ID)
	if errors.Is(err, executionlineage.ErrConflict) {
		// A crash after activation but before removing SQL maintenance is
		// recoverable only with the exact locally persisted external ticket.
		ticket, err = readExecutionTicket(ctx, authority, opts.workdir)
	}
	if err != nil {
		return err
	}
	if ticket.Document().Marker.OperationID != operation.ID || ticket.Document().Marker.State != executionlineage.Initializing {
		return executionlineage.ErrConflict
	}
	if err := writeJSON(filepath.Join(opts.workdir, executionTicketFile), ticket.Document()); err != nil {
		return err // The independent marker deliberately stays inactive.
	}
	if cfg.Database.Driver != "mysql" {
		if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
			return err
		}
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()
	if err := storageguard.Enter(ctx, db, operation.ID, stoppedRuntimes(opts)); err != nil {
		return err
	}
	if err := initializeEmptyCore(ctx, db); err != nil {
		return err
	}
	return finalizeExecution(ctx, authority, db, operation.ID, ticket, output)
}

func initializeEmptyCore(ctx context.Context, db *gorm.DB) error {
	runs, states := db.Migrator().HasTable("runs"), db.Migrator().HasTable("state_runs")
	if runs && states {
		return nil // Explicitly adopt existing history; pending sources are quarantined.
	}
	if runs != states {
		return errors.New("incomplete core schema requires operator repair before execution initialization")
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return err
	}
	for _, table := range tables {
		if table != "payload_storage_control" && table != "payload_storage_runtimes" && table != "sqlite_sequence" {
			return errors.New("nonempty SQL without core tables cannot be treated as a new installation")
		}
	}
	// No application rows exist. The normal server migration/seeding remains
	// responsible for the rest of the schema after the offline barrier ends.
	return db.WithContext(ctx).AutoMigrate(&models.Run{}, &models.StateRun{})
}

func readExecutionTicket(ctx context.Context, authority *executionlineage.Authority, dir string) (executionlineage.Ticket, error) {
	raw, err := os.ReadFile(filepath.Join(dir, executionTicketFile))
	if err != nil {
		return executionlineage.Ticket{}, errors.New("restore requires its durable ticket; run restore-begin before importing SQL")
	}
	var document executionlineage.TicketDocument
	if len(raw) > 64<<10 || json.Unmarshal(raw, &document) != nil {
		return executionlineage.Ticket{}, errors.New("invalid durable execution recovery ticket")
	}
	return authority.Resume(ctx, document)
}

func finalizeExecution(ctx context.Context, authority *executionlineage.Authority, db *gorm.DB, owner string, ticket executionlineage.Ticket, output io.Writer) error {
	if err := executionlineage.EnsureRecoverySchema(ctx, db, owner); err != nil {
		return err
	}
	var binding models.ExecutionDataset
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		binding, err = authority.QuarantineTx(tx, owner, ticket)
		return err
	}); err != nil {
		return err
	}
	permit, err := authority.Activate(ctx, db, owner, ticket)
	if err != nil {
		return err
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]any{"active": true, "datasetId": permit.DatasetID(), "lineage": permit.Lineage(), "quarantined": binding.QuarantineCount, "historyRetained": true})
}

func restoreExecution(ctx context.Context, opts options, cfg *config.Config, db *gorm.DB, output io.Writer) error {
	state, err := prepareCheckpoint(opts, cfg)
	if err != nil {
		return err
	}
	lock, err := lockExecutionWorkdir(opts.workdir, false)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	activeSQL, err := filepath.Abs(cfg.Database.Path)
	if err != nil {
		return err
	}
	backupSQL, err := filepath.Abs(filepath.Join(opts.workdir, state.Database))
	if err != nil || cfg.Database.Driver != "mysql" && activeSQL == backupSQL {
		return errors.New("SQL backup is immutable; restore into a separate SQL database")
	}
	if digest, err := payloadstore.FileSHA256(backupSQL); err != nil || digest != state.Header.DatabaseSHA256 || state.Header.DatabaseSHA256 == "" {
		return errors.New("SQL backup checksum does not match the replica checkpoint")
	}
	authority, err := openExecutionControl(ctx, opts, cfg)
	if err != nil {
		return err
	}
	if err := storageguard.Enter(ctx, db, state.Header.ID, stoppedRuntimes(opts)); err != nil {
		return err
	}
	if opts.mode == "restore-begin" {
		// Persist the local intent first, so an interrupted ticket write cannot
		// make the generic release command overlook an inactive external fence.
		if err := writeJSON(filepath.Join(opts.workdir, "execution-restore.json"), map[string]any{"version": 1, "id": state.Header.ID, "backupSHA256": state.Header.DatabaseSHA256}); err != nil {
			return err
		}
		ticket, err := authority.BeginRestore(ctx, state.Header.ID, state.Header.DatabaseSHA256)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(opts.workdir, executionTicketFile), ticket.Document()); err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(map[string]any{"state": "restoring", "executionDisabled": true, "ticket": executionTicketFile, "next": "import SQL into a separate database, then run restore-finalize with the same workdir and execution-control"})
	}
	ticket, err := readExecutionTicket(ctx, authority, opts.workdir)
	if err != nil {
		return err
	}
	marker := ticket.Document().Marker
	if marker.State != executionlineage.Restoring || marker.OperationID != state.Header.ID || marker.BackupSHA256 != state.Header.DatabaseSHA256 {
		return executionlineage.ErrConflict
	}
	target, err := loadTarget(opts.target)
	if err != nil {
		return err
	}
	source, destination, err := payloadstore.Locations(cfg.Storage, target)
	if err != nil || source.Fingerprint != state.Header.Source.Fingerprint || destination.Fingerprint != state.Header.Destination.Fingerprint {
		return errors.New("restore storage configuration differs from its replica checkpoint")
	}
	objects, err := payloadstore.OpenObjects(ctx, target)
	if err != nil {
		return err
	}
	if err := verifyReplica(ctx, db, objects, opts.workdir, &state); err != nil {
		return err
	}
	if err := payloadstore.ActivateReplica(ctx, db, state.Header, state.Summary); err != nil {
		return err
	}
	return finalizeExecution(ctx, authority, db, state.Header.ID, ticket, output)
}

func checkExecutionRelease(ctx context.Context, opts options, cfg *config.Config, db *gorm.DB) error {
	_, ticketErr := os.Stat(filepath.Join(opts.workdir, executionTicketFile))
	_, intentErr := os.Stat(filepath.Join(opts.workdir, "execution-restore.json"))
	if errors.Is(ticketErr, os.ErrNotExist) && errors.Is(intentErr, os.ErrNotExist) && opts.executionControl == "" {
		return nil // A legacy payload copy that never began execution recovery.
	}
	if opts.executionControl == "" {
		return errors.New("execution recovery cannot be released without --execution-control and a matching active SQL binding")
	}
	authority, err := openExecutionControl(ctx, opts, cfg)
	if err != nil {
		return err
	}
	permit, err := authority.ReadActive(ctx)
	if err != nil {
		return err
	}
	return authority.CheckBinding(ctx, db, permit)
}
