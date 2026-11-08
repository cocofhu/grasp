// storage-admin creates verified SQL/object replicas and activates them only
// while all Grasp writers are stopped. It never removes source data.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

type options struct {
	config, target, mode, workdir, stopped string
	offline, resume, activate              bool
}

type checkpoint struct {
	Header   payloadstore.ReplicaHeader  `json:"header"`
	Phase    string                      `json:"phase"`
	Switch   bool                        `json:"switch"`
	Summary  payloadstore.ReplicaSummary `json:"summary"`
	Database string                      `json:"database"`
}

func main() {
	var opts options
	flag.StringVar(&opts.config, "config", "config.yaml", "source server configuration")
	flag.StringVar(&opts.target, "target-storage", "", "private YAML containing destination storage config (no env overrides)")
	flag.StringVar(&opts.mode, "mode", "status", "status, copy, restore, verify, or release")
	flag.StringVar(&opts.workdir, "workdir", "", "private durable checkpoint and SQL backup directory")
	flag.StringVar(&opts.stopped, "acknowledge-stopped", "", "comma-separated exact IDs of registered processes you have confirmed stopped")
	flag.BoolVar(&opts.offline, "offline", false, "attest all servers, scripts and external SQL writers are stopped")
	flag.BoolVar(&opts.resume, "resume", false, "resume the same interrupted copy, rechecking copied bytes")
	flag.BoolVar(&opts.activate, "switch", false, "activate the verified destination after copy")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options, output io.Writer) error {
	if err := validateOptions(opts); err != nil {
		return err
	}
	// Unlike normal server startup, a mistyped maintenance config path must
	// never fall back to an empty default installation.
	if _, err := os.Stat(opts.config); err != nil {
		return err
	}
	if err := config.Load(opts.config); err != nil {
		return errors.New("could not load source configuration")
	}
	cfg := config.GetConfig()
	if cfg.Database.Driver != "mysql" {
		if cfg.Database.Path == ":memory:" || strings.HasPrefix(cfg.Database.Path, "file:") {
			return errors.New("storage administration requires an existing persistent SQLite file")
		}
		if _, err := os.Stat(cfg.Database.Path); err != nil {
			return errors.New("configured SQLite database does not exist")
		}
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()
	if opts.mode == "status" {
		status, err := storageguard.Inspect(ctx, db)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(status)
	}
	state, err := prepareCheckpoint(opts, cfg)
	if err != nil {
		return err
	}
	if cfg.Database.Driver != "mysql" {
		active, err := filepath.Abs(cfg.Database.Path)
		if err != nil {
			return err
		}
		backup, err := filepath.Abs(filepath.Join(opts.workdir, state.Database))
		if err != nil {
			return err
		}
		if active == backup {
			return errors.New("restore into a separate SQL database; the backup itself is immutable")
		}
	}
	// The workdir lock complements the durable SQL owner: accidental duplicate
	// invocations cannot truncate each other's manifest/checkpoint files.
	lock, err := os.OpenFile(filepath.Join(opts.workdir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("this replica work directory is already in use")
	}
	if opts.mode == "release" {
		return storageguard.Leave(ctx, db, state.Header.ID)
	}
	if opts.mode == "copy" && state.Phase == "complete" {
		return errors.New("copy is already complete; use verify/restore with an offline restored SQL database")
	}
	stopped := []string{}
	if opts.stopped != "" {
		stopped = strings.Split(opts.stopped, ",")
	}
	if err = storageguard.Enter(ctx, db, state.Header.ID, stopped); err != nil {
		return err
	}
	failure := func(err error) error {
		return fmt.Errorf("%w; maintenance %s remains active: resume this workdir or explicitly release it", err, state.Header.ID)
	}
	targetConfig, err := loadTarget(opts.target)
	if err != nil {
		return failure(err)
	}
	sourceLocation, destinationLocation, err := payloadstore.Locations(cfg.Storage, targetConfig)
	if err != nil {
		return failure(err)
	}
	if state.Header.Source.Fingerprint != sourceLocation.Fingerprint || state.Header.Destination.Fingerprint != destinationLocation.Fingerprint {
		return failure(errors.New("configuration differs from this replica checkpoint"))
	}
	destination, err := payloadstore.OpenObjects(ctx, targetConfig)
	if err != nil {
		return failure(err)
	}
	manifest := filepath.Join(opts.workdir, "manifest.jsonl")
	copiedNow := false
	_, statErr := os.Stat(manifest)
	if opts.mode == "copy" && errors.Is(statErr, os.ErrNotExist) {
		if err = copyReplica(ctx, db, cfg, destination, opts, &state); err != nil {
			return failure(err)
		}
		copiedNow = true
	} else if statErr != nil {
		return failure(statErr)
	}
	// A newly completed copy already performed all GETs and SQL verification.
	// Resuming a completed manifest or restoring always verifies it again.
	if !copiedNow {
		if err = verifyReplica(ctx, db, destination, opts.workdir, &state); err != nil {
			return failure(err)
		}
	}
	if opts.activate || opts.mode == "restore" {
		if err = payloadstore.ActivateReplica(ctx, db, state.Header, state.Summary); err != nil {
			return failure(err)
		}
	}
	if err = storageguard.Leave(ctx, db, state.Header.ID); err != nil {
		return failure(err)
	}
	if opts.mode == "copy" {
		state.Phase = "complete"
		if err = writeJSON(filepath.Join(opts.workdir, "checkpoint.json"), state); err != nil {
			return err
		}
	}
	return json.NewEncoder(output).Encode(map[string]any{
		"id": state.Header.ID, "verified": state.Summary,
		"activated": opts.activate || opts.mode == "restore", "sourceRetained": true,
		"database": state.Database, "manifest": "manifest.jsonl",
		"externalBackupRequired": []string{"secrets encryption key", "Agent/configuration files", "token-ledger journal", "sandbox workspaces"},
	})
}

func validateOptions(opts options) error {
	switch opts.mode {
	case "status":
		return nil
	case "copy", "restore", "verify", "release":
	default:
		return errors.New("unknown storage administration mode")
	}
	if !opts.offline || opts.workdir == "" {
		return errors.New("storage administration requires --offline and --workdir; stop all replicas and external writers first")
	}
	if opts.mode != "release" && opts.target == "" {
		return errors.New("--target-storage is required")
	}
	if opts.activate && opts.mode != "copy" {
		return errors.New("--switch is only used with copy; restore activates its verified replica")
	}
	return nil
}

func prepareCheckpoint(opts options, cfg *config.Config) (checkpoint, error) {
	var state checkpoint
	file := filepath.Join(opts.workdir, "checkpoint.json")
	raw, err := os.ReadFile(file)
	if err == nil {
		if opts.mode == "copy" && !opts.resume {
			return state, errors.New("replica checkpoint exists; use --resume")
		}
		if err = json.Unmarshal(raw, &state); err != nil || state.Header.Version != 1 || state.Header.ID == "" ||
			(state.Database != "database.sqlite" && state.Database != "database.sql") {
			return state, errors.New("invalid replica checkpoint")
		}
		if opts.mode == "copy" && state.Switch != opts.activate {
			return state, errors.New("resume must preserve the original --switch choice")
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) || opts.mode != "copy" || opts.resume {
		return state, errors.New("replica checkpoint is missing or unreadable")
	}
	target, err := loadTarget(opts.target)
	if err != nil {
		return state, err
	}
	source, destination, err := payloadstore.Locations(cfg.Storage, target)
	if err != nil {
		return state, err
	}
	if err = os.Mkdir(opts.workdir, 0o700); err != nil {
		return state, fmt.Errorf("replica requires a new private workdir: %w", err)
	}
	state = checkpoint{Header: payloadstore.ReplicaHeader{Version: 1, ID: uuid.NewString(),
		Source: source, Destination: destination, CreatedAt: time.Now().UTC()}, Phase: "preparing", Switch: opts.activate, Database: "database.sqlite"}
	if cfg.Database.Driver == "mysql" {
		state.Database = "database.sql"
	}
	err = writeJSON(file, state)
	return state, err
}

func loadTarget(path string) (config.StorageConfig, error) {
	var document struct {
		Storage config.StorageConfig `yaml:"storage"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return document.Storage, err
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		// YAML errors may echo a malformed secret-bearing line.
		return document.Storage, errors.New("invalid destination storage YAML")
	}
	return document.Storage, nil
}

func copyReplica(ctx context.Context, db *gorm.DB, cfg *config.Config, destination objectstore.Store, opts options, state *checkpoint) error {
	if err := payloadstore.CheckSource(ctx, db, state.Header.ID, state.Header.Source); err != nil {
		return err
	}
	storage, err := payloadstore.Open(ctx, db, cfg)
	if err != nil {
		return err
	}
	defer storage.Close()
	source := storage.Objects
	if source == nil {
		// Convert all legacy local bytes with the same immutable keys and IDs.
		// No location is switched and every source file remains in place.
		if err = storage.Files.SetObjectStore(destination); err != nil {
			return err
		}
		storage.Objects = destination
		storage.Blobs, err = blob.NewWithObjectStore(cfg, destination)
		if err != nil {
			return err
		}
		source = destination
	} else if err := objectstore.ProbeSeparation(ctx, source, destination, state.Header.Source.Prefix, state.Header.Destination.Prefix); err != nil {
		return err
	}
	if _, err = storage.ArchiveLegacy(ctx); err != nil {
		return err
	}
	if err = snapshotDatabase(ctx, db, cfg, opts.workdir, state); err != nil {
		return err
	}
	manifest := filepath.Join(opts.workdir, "manifest.jsonl")
	partial := manifest + ".partial"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	summary, err := payloadstore.WriteReplica(ctx, db, source, destination, state.Header, file)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(partial, manifest); err != nil {
		return err
	}
	if err = syncDirectory(opts.workdir); err != nil {
		return err
	}
	state.Summary, state.Phase = summary, "verified"
	if err = writeJSON(filepath.Join(opts.workdir, "checkpoint.json"), *state); err != nil {
		return err
	}
	return nil
}

func snapshotDatabase(ctx context.Context, db *gorm.DB, cfg *config.Config, dir string, state *checkpoint) error {
	destination := filepath.Join(dir, state.Database)
	if _, err := os.Stat(destination); errors.Is(err, os.ErrNotExist) {
		partial := destination + ".partial"
		// Only this operation's unpublished partial file can be replaced.
		if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = database.Snapshot(ctx, db, cfg.Database, state.Header.ID, partial); err != nil {
			return err
		}
		if err = os.Rename(partial, destination); err != nil {
			return err
		}
		if err = syncDirectory(dir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	digest, err := payloadstore.FileSHA256(destination)
	if err != nil {
		return err
	}
	if state.Header.DatabaseSHA256 != "" && digest != state.Header.DatabaseSHA256 {
		return errors.New("SQL backup checksum differs from checkpoint")
	}
	state.Header.DatabaseSHA256 = digest
	return writeJSON(filepath.Join(dir, "checkpoint.json"), *state)
}

func verifyReplica(ctx context.Context, db *gorm.DB, destination objectstore.Store, dir string, state *checkpoint) error {
	digest, err := payloadstore.FileSHA256(filepath.Join(dir, state.Database))
	if err != nil || digest != state.Header.DatabaseSHA256 {
		return errors.New("SQL backup is missing or has changed")
	}
	file, err := os.Open(filepath.Join(dir, "manifest.jsonl"))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	header, summary, err := payloadstore.VerifyReplica(ctx, db, destination, file)
	if err != nil {
		return err
	}
	if header.ID != state.Header.ID || header.Source.Fingerprint != state.Header.Source.Fingerprint ||
		header.Destination.Fingerprint != state.Header.Destination.Fingerprint || header.DatabaseSHA256 != digest {
		return errors.New("manifest identity differs from checkpoint")
	}
	state.Header, state.Summary = header, summary
	return nil
}

func writeJSON(path string, value any) error {
	file, err := os.OpenFile(path+".partial", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(value)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(path+".partial", path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
