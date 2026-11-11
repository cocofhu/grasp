package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

const (
	executionTicketFile    = "execution-ticket.json"
	sqlMaintenanceProtocol = "grasp-sql-execution-maintenance/v1"
	sqlTicketProtocol      = "grasp-sql-execution-ticket/v1"
)

type initialization struct {
	Version             int    `json:"version"`
	Protocol            string `json:"protocol"`
	ID                  string `json:"id"`
	Workdir             string `json:"workdir"`
	ConfigurationSHA256 string `json:"configurationSHA256"`
	TicketSHA256        string `json:"ticketSHA256,omitempty"`
}

type executionRestore struct {
	Version                   int    `json:"version"`
	Protocol                  string `json:"protocol"`
	ID                        string `json:"id"`
	Workdir                   string `json:"workdir"`
	BackupSHA256              string `json:"backupSHA256"`
	SourceDatabaseSHA256      string `json:"sourceDatabaseSHA256"`
	TargetDatabaseSHA256      string `json:"targetDatabaseSHA256,omitempty"`
	TicketSHA256              string `json:"ticketSHA256,omitempty"`
	TargetRegistrationStarted bool   `json:"targetRegistrationStarted,omitempty"`
}

type executionCompletion struct {
	Protocol      string `json:"protocol"`
	ID            string `json:"id"`
	DatasetID     string `json:"datasetId"`
	Lineage       string `json:"lineage"`
	BindingSHA256 string `json:"bindingSHA256"`
}

func executionBindingFingerprint(binding models.ExecutionDataset) (string, error) {
	// BoundAt has driver-specific timestamp precision. Freeze the comparison
	// metadata, including the actual imported source and quarantine closure.
	return executionFingerprint(struct {
		ID, NamespaceID, DatasetID, Lineage, OperationID string
		Revision                                         uint64
		SourceDatasetID, SourceLineage, BackupSHA256     string
		QuarantineCount                                  int64
		QuarantineSHA256                                 string
	}{binding.ID, binding.NamespaceID, binding.DatasetID, binding.Lineage, binding.OperationID, binding.ControlRevision,
		binding.SourceDatasetID, binding.SourceLineage, binding.BackupSHA256, binding.QuarantineCount, binding.QuarantineSHA256})
}

func executionFingerprint(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func validExecutionSHA(value string) bool {
	body, err := hex.DecodeString(value)
	return err == nil && len(body) == sha256.Size && hex.EncodeToString(body) == value
}

func executionWorkdir(dir string) (string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func readExecutionJSON(path string, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 64<<10 {
		return errors.New("execution checkpoint must be a private regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("invalid execution maintenance checkpoint")
	}
	return nil
}

func legacyExecutionError() error {
	return errors.New("legacy external execution recovery checkpoint is not supported; complete its original offline operation with the original tool before upgrading; do not remove tickets or release maintenance to bypass it")
}

func rejectLegacyExecutionTicket(dir string) error {
	var header struct {
		Version  int    `json:"version"`
		Protocol string `json:"protocol"`
	}
	err := readExecutionJSON(filepath.Join(dir, executionTicketFile), &header)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if header.Version != 2 || header.Protocol != sqlTicketProtocol {
		return legacyExecutionError()
	}
	return nil
}

func lockExecutionWorkdir(dir string, create bool) (*os.File, error) {
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("execution recovery requires a private workdir (mode 0700)")
	}
	path := filepath.Join(dir, ".lock")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("invalid execution workdir lock")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.New("execution recovery workdir is already in use")
	}
	return file, nil
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
	if err := rejectLegacyExecutionTicket(opts.workdir); err != nil {
		return err
	}
	dir, err := executionWorkdir(opts.workdir)
	if err != nil {
		return err
	}
	fingerprint, err := executionFingerprint(struct {
		Database config.DatabaseConfig
		Storage  config.StorageConfig
	}{cfg.Database, cfg.Storage})
	if err != nil {
		return err
	}
	file := filepath.Join(opts.workdir, "execution-initialization.json")
	operation := initialization{Version: 2, Protocol: sqlMaintenanceProtocol, ID: uuid.NewString(), Workdir: dir, ConfigurationSHA256: fingerprint}
	err = readExecutionJSON(file, &operation)
	existing := err == nil
	if existing {
		if operation.Version != 2 || operation.Protocol != sqlMaintenanceProtocol {
			return legacyExecutionError()
		}
		if uuid.Validate(operation.ID) != nil || operation.Workdir != dir || operation.ConfigurationSHA256 != fingerprint {
			return errors.New("execution initialization checkpoint belongs to another workdir or configuration")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	operation, err = initializationAfterUpgrade(ctx, opts, cfg, operation, existing)
	if err != nil {
		return err
	}
	if !existing {
		if _, err := os.Stat(filepath.Join(opts.workdir, "upgrade.json")); errors.Is(err, os.ErrNotExist) {
			entries, err := os.ReadDir(opts.workdir)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if entry.Name() != ".lock" {
					return errors.New("execution initialization requires a new private workdir or its verified original upgrade workdir")
				}
			}
		} else if err != nil {
			return err
		}
		if err := writeJSON(file, operation); err != nil {
			return err
		}
	}
	if cfg.Database.Driver != "mysql" {
		if cfg.Database.Path == "" || cfg.Database.Path == ":memory:" || strings.HasPrefix(cfg.Database.Path, "file:") {
			return errors.New("execution maintenance requires a persistent SQL database")
		}
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
	if err := executionlineage.EnsureRecoverySchema(ctx, db, operation.ID); err != nil {
		return err
	}
	authority, err := executionlineage.OpenSQL(ctx, db)
	if err != nil {
		return err
	}
	var ticket executionlineage.Ticket
	if operation.TicketSHA256 != "" {
		ticket, err = readExecutionTicket(ctx, authority, opts.workdir)
	} else {
		ticket, err = authority.Initialize(ctx, operation.ID)
		if err == nil {
			err = writeJSON(filepath.Join(opts.workdir, executionTicketFile), ticket.Document())
			if err == nil {
				operation.TicketSHA256, err = payloadstore.FileSHA256(filepath.Join(opts.workdir, executionTicketFile))
			}
			if err == nil {
				err = writeJSON(file, operation)
			}
		}
	}
	if err != nil {
		return err
	}
	return finalizeExecution(ctx, authority, db, operation.ID, ticket, opts.workdir, output)
}

func initializeEmptyCore(ctx context.Context, db *gorm.DB) error {
	runs, states := db.Migrator().HasTable("runs"), db.Migrator().HasTable("state_runs")
	if runs && states {
		return nil
	}
	if runs != states {
		return errors.New("incomplete core schema requires operator repair before execution initialization")
	}
	fresh, err := database.CaptureFreshOrigin(ctx, db)
	if err != nil {
		return err
	}
	if !fresh.Empty() {
		return errors.New("nonempty SQL without core tables cannot be treated as a new installation")
	}
	if err := db.WithContext(ctx).AutoMigrate(&models.Run{}, &models.StateRun{}); err != nil {
		return err
	}
	return database.SealFreshOrigin(ctx, db, fresh, "execution-init")
}

func readExecutionTicketDocument(dir string) (executionlineage.TicketDocument, error) {
	var ticket executionlineage.TicketDocument
	if err := readExecutionJSON(filepath.Join(dir, executionTicketFile), &ticket); err != nil {
		return executionlineage.TicketDocument{}, errors.New("execution recovery requires its original private durable ticket")
	}
	if err := rejectLegacyExecutionTicket(dir); err != nil {
		return executionlineage.TicketDocument{}, err
	}
	canonical, err := executionWorkdir(dir)
	if err != nil {
		return executionlineage.TicketDocument{}, err
	}
	var expected, owner string
	var init initialization
	if err := readExecutionJSON(filepath.Join(dir, "execution-initialization.json"), &init); err == nil {
		if init.Version != 2 || init.Protocol != sqlMaintenanceProtocol {
			return executionlineage.TicketDocument{}, legacyExecutionError()
		}
		if init.Workdir != canonical {
			return executionlineage.TicketDocument{}, executionlineage.ErrConflict
		}
		expected, owner = init.TicketSHA256, init.ID
	} else if !errors.Is(err, os.ErrNotExist) {
		return executionlineage.TicketDocument{}, err
	} else {
		var restore executionRestore
		if err := readExecutionJSON(filepath.Join(dir, "execution-restore.json"), &restore); err != nil {
			return executionlineage.TicketDocument{}, err
		}
		if restore.Version != 2 || restore.Protocol != sqlMaintenanceProtocol {
			return executionlineage.TicketDocument{}, legacyExecutionError()
		}
		if restore.Workdir != canonical || restore.BackupSHA256 != ticket.Marker.BackupSHA256 {
			return executionlineage.TicketDocument{}, executionlineage.ErrConflict
		}
		expected, owner = restore.TicketSHA256, restore.ID
	}
	digest, err := payloadstore.FileSHA256(filepath.Join(dir, executionTicketFile))
	if err != nil || !validExecutionSHA(expected) || digest != expected || owner != ticket.Marker.OperationID {
		return executionlineage.TicketDocument{}, executionlineage.ErrConflict
	}
	return ticket, nil
}

func readExecutionTicket(ctx context.Context, authority *executionlineage.SQLAuthority, dir string) (executionlineage.Ticket, error) {
	document, err := readExecutionTicketDocument(dir)
	if err != nil {
		return executionlineage.Ticket{}, err
	}
	return authority.Resume(ctx, document)
}

func finalizeExecution(ctx context.Context, authority *executionlineage.SQLAuthority, db *gorm.DB, owner string, ticket executionlineage.Ticket, dir string, output io.Writer) error {
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
	if err := db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error; err != nil {
		return err
	}
	bindingSHA, err := executionBindingFingerprint(binding)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "execution-complete.json"), executionCompletion{Protocol: sqlMaintenanceProtocol, ID: owner, DatasetID: permit.DatasetID(), Lineage: permit.Lineage(), BindingSHA256: bindingSHA}); err != nil {
		return err
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]any{"active": true, "datasetId": permit.DatasetID(), "lineage": permit.Lineage(), "quarantined": binding.QuarantineCount, "historyRetained": true})
}

// A completed local receipt is an irreversible boundary. A subsequent SQL
// import must not reuse it to activate the same generation for a second time.
// Matching completed retries only finish their own Leave or read their result.
func reconcileCompletedExecution(ctx context.Context, authority *executionlineage.SQLAuthority, db *gorm.DB, document executionlineage.TicketDocument, dir string, output io.Writer) (bool, error) {
	var completion executionCompletion
	err := readExecutionJSON(filepath.Join(dir, "execution-complete.json"), &completion)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	marker := document.Marker
	if completion.Protocol != sqlMaintenanceProtocol || completion.ID != marker.OperationID || completion.DatasetID != marker.DatasetID || completion.Lineage != marker.Lineage {
		return true, executionlineage.ErrConflict
	}
	permit, err := authority.ReadActive(ctx)
	if err != nil {
		return true, err
	}
	if permit.DatasetID() != completion.DatasetID || permit.Lineage() != completion.Lineage || permit.NamespaceID() != marker.NamespaceID {
		return true, errors.New("completed recovery no longer matches SQL; use a new verified backup and recovery workdir instead of reusing its completed ticket")
	}
	var binding models.ExecutionDataset
	if err := db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error; err != nil {
		return true, err
	}
	bindingSHA, err := executionBindingFingerprint(binding)
	if err != nil {
		return true, err
	}
	if binding.OperationID != marker.OperationID || binding.ControlRevision != marker.Revision+1 || binding.BackupSHA256 != marker.BackupSHA256 || !validExecutionSHA(completion.BindingSHA256) || bindingSHA != completion.BindingSHA256 {
		return true, executionlineage.ErrConflict
	}
	var saved models.ExecutionRecovery
	if err := db.WithContext(ctx).Take(&saved, "id = ?", marker.OperationID).Error; err != nil {
		return true, errors.Join(executionlineage.ErrConflict, err)
	}
	digest, err := executionFingerprint(document)
	if err != nil {
		return true, err
	}
	markerJSON, err := json.Marshal(marker)
	if err != nil {
		return true, err
	}
	if saved.Protocol != sqlTicketProtocol || saved.State != "finalized" || saved.FinalizedAt == nil || saved.TicketSHA256 != digest || saved.MarkerJSON != string(markerJSON) {
		return true, executionlineage.ErrConflict
	}
	if err := authority.CheckBinding(ctx, db, permit); err != nil {
		return true, err
	}
	var control storageguard.Control
	if err := db.WithContext(ctx).Take(&control, "name = ?", "primary").Error; err != nil {
		return true, err
	}
	if control.Owner != "" && control.Owner != marker.OperationID {
		return true, executionlineage.ErrInactive
	}
	if control.Owner == marker.OperationID {
		// Recheck the original offline barrier, including zero registered
		// runtimes, before completing an interrupted Leave.
		if err := storageguard.Enter(ctx, db, marker.OperationID, nil); err != nil {
			return true, err
		}
		if err := storageguard.Leave(ctx, db, marker.OperationID); err != nil {
			return true, err
		}
	}
	return true, json.NewEncoder(output).Encode(map[string]any{"active": true, "datasetId": permit.DatasetID(), "lineage": permit.Lineage(), "historyRetained": true, "originalReceipt": true})
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
	if err := rejectLegacyExecutionTicket(opts.workdir); err != nil {
		return err
	}
	activeSQL, err := filepath.Abs(cfg.Database.Path)
	if err != nil {
		return err
	}
	backupSQL, err := filepath.Abs(filepath.Join(opts.workdir, state.Database))
	if err != nil || cfg.Database.Driver != "mysql" && activeSQL == backupSQL {
		return errors.New("SQL backup is immutable; restore into a separate SQL database")
	}
	if digest, err := payloadstore.FileSHA256(backupSQL); err != nil || digest != state.Header.DatabaseSHA256 || !validExecutionSHA(state.Header.DatabaseSHA256) {
		return errors.New("SQL backup checksum does not match the replica checkpoint")
	}
	dir, err := executionWorkdir(opts.workdir)
	if err != nil {
		return err
	}
	databaseSHA, err := executionFingerprint(cfg.Database)
	if err != nil {
		return err
	}
	intentFile := filepath.Join(opts.workdir, "execution-restore.json")
	var intent executionRestore
	err = readExecutionJSON(intentFile, &intent)
	if errors.Is(err, os.ErrNotExist) && opts.mode == "restore-begin" && !state.ExecutionRecovery {
		intent = executionRestore{Version: 2, Protocol: sqlMaintenanceProtocol, ID: state.Header.ID, Workdir: dir, BackupSHA256: state.Header.DatabaseSHA256, SourceDatabaseSHA256: databaseSHA}
	} else if err != nil {
		return errors.New("restore requires its original SQL maintenance intent; run restore-begin before importing SQL")
	}
	if intent.Version != 2 || intent.Protocol != sqlMaintenanceProtocol {
		return legacyExecutionError()
	}
	if intent.ID != state.Header.ID || intent.Workdir != dir || intent.BackupSHA256 != state.Header.DatabaseSHA256 || !validExecutionSHA(intent.SourceDatabaseSHA256) {
		return executionlineage.ErrConflict
	}
	if opts.mode == "restore-begin" {
		if intent.SourceDatabaseSHA256 != databaseSHA {
			return errors.New("restore-begin must use its original source SQL configuration")
		}
	} else {
		if !state.ExecutionRecovery {
			return errors.New("restore-finalize requires its original restore-begin checkpoint")
		}
		if intent.TargetRegistrationStarted && !validExecutionSHA(intent.TargetDatabaseSHA256) {
			return executionlineage.ErrConflict
		}
		if intent.SourceDatabaseSHA256 == databaseSHA {
			return errors.New("restore-finalize requires a separate restored SQL database")
		}
		if intent.TargetDatabaseSHA256 != "" && intent.TargetDatabaseSHA256 != databaseSHA {
			return errors.New("restore target SQL differs from the original finalize attempt")
		}
		intent.TargetDatabaseSHA256 = databaseSHA
		if err := writeJSON(intentFile, intent); err != nil {
			return err
		}
		document, err := readExecutionTicketDocument(opts.workdir)
		if err != nil {
			return err
		}
		authority, err := executionlineage.OpenSQL(ctx, db)
		if err != nil {
			return err
		}
		if completed, err := reconcileCompletedExecution(ctx, authority, db, document, opts.workdir, output); completed || err != nil {
			return err
		}
	}
	if err := storageguard.Enter(ctx, db, state.Header.ID, stoppedRuntimes(opts)); err != nil {
		return err
	}
	if err := executionlineage.EnsureRecoverySchema(ctx, db, state.Header.ID); err != nil {
		return err
	}
	authority, err := executionlineage.OpenSQL(ctx, db)
	if err != nil {
		return err
	}
	if opts.mode == "restore-begin" {
		// Both the copy checkpoint and intent survive SQL import. Neither may
		// be deleted to make an unfinished restore look like a generic copy.
		state.ExecutionRecovery = true
		if err := writeJSON(filepath.Join(opts.workdir, "checkpoint.json"), state); err != nil {
			return err
		}
		if err := writeJSON(intentFile, intent); err != nil {
			return err
		}
		var ticket executionlineage.Ticket
		if intent.TicketSHA256 != "" {
			// Once the durable file hash is published, losing or changing the
			// ticket is an error, not permission to reconstruct a different file.
			ticket, err = readExecutionTicket(ctx, authority, opts.workdir)
		} else {
			// A SQL commit can precede the local ticket write. The original
			// private intent permits only the same operation's persisted ticket.
			ticket, err = authority.BeginRestore(ctx, state.Header.ID, state.Header.DatabaseSHA256)
		}
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(opts.workdir, executionTicketFile), ticket.Document()); err != nil {
			return err
		}
		intent.TicketSHA256, err = payloadstore.FileSHA256(filepath.Join(opts.workdir, executionTicketFile))
		if err != nil {
			return err
		}
		if err := writeJSON(intentFile, intent); err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(map[string]any{"state": "restoring", "executionDisabled": true, "ticket": executionTicketFile, "next": "import the verified SQL backup into a separate database, then run restore-finalize with this original private workdir"})
	}
	document, err := readExecutionTicketDocument(opts.workdir)
	if err != nil {
		return err
	}
	marker := document.Marker
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
	var ticket executionlineage.Ticket
	if intent.TargetRegistrationStarted {
		// Registration was durably acknowledged before any activation. Losing
		// its SQL row now means a rollback, never a fresh imported target.
		ticket, err = authority.Resume(ctx, document)
	} else {
		ticket, err = authority.ResumeImported(ctx, document, state.Header.DatabaseSHA256)
		if err == nil {
			// SQL intent commits first. A crash before this file write retries
			// the same SQL row; no quarantine or activation has happened yet.
			intent.TargetRegistrationStarted = true
			err = writeJSON(intentFile, intent)
		}
	}
	if err != nil {
		return err
	}
	if err := payloadstore.ActivateReplica(ctx, db, state.Header, state.Summary); err != nil {
		return err
	}
	return finalizeExecution(ctx, authority, db, state.Header.ID, ticket, opts.workdir, output)
}

func checkExecutionRelease(ctx context.Context, opts options, cfg *config.Config, db *gorm.DB) error {
	// Always consult SQL: local ticket loss must not turn pending SQL
	// initialization/restore into an ordinary releasable payload copy.
	authority, err := executionlineage.OpenSQL(ctx, db)
	if err != nil {
		return err
	}
	permit, err := authority.ReadActive(ctx)
	state, stateErr := prepareCheckpoint(opts, cfg)
	if stateErr != nil {
		return stateErr
	}
	_, ticketErr := os.Lstat(filepath.Join(opts.workdir, executionTicketFile))
	_, intentErr := os.Lstat(filepath.Join(opts.workdir, "execution-restore.json"))
	recovery := state.ExecutionRecovery || !errors.Is(ticketErr, os.ErrNotExist) || !errors.Is(intentErr, os.ErrNotExist)
	if errors.Is(err, executionlineage.ErrUninitialized) && !recovery {
		return nil
	}
	if err != nil {
		return err
	}
	if err := authority.CheckBinding(ctx, db, permit); err != nil {
		return err
	}
	if !recovery {
		return nil
	}
	if _, err := readExecutionTicket(ctx, authority, opts.workdir); err != nil {
		return err
	}
	var completion executionCompletion
	if err := readExecutionJSON(filepath.Join(opts.workdir, "execution-complete.json"), &completion); err != nil {
		return errors.New("execution recovery is incomplete; resume its original private workdir")
	}
	if completion.Protocol != sqlMaintenanceProtocol || completion.ID != state.Header.ID || completion.DatasetID != permit.DatasetID() || completion.Lineage != permit.Lineage() {
		return executionlineage.ErrConflict
	}
	var binding models.ExecutionDataset
	if err := db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error; err != nil {
		return err
	}
	bindingSHA, err := executionBindingFingerprint(binding)
	if err != nil {
		return err
	}
	if !validExecutionSHA(completion.BindingSHA256) || bindingSHA != completion.BindingSHA256 {
		return executionlineage.ErrConflict
	}
	return nil
}
