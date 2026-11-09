package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
)

func executionOptions(t *testing.T) (options, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "sql", "application.db")}, Storage: config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "payloads")}}
	opts := options{config: filepath.Join(dir, "config.yaml"), mode: "execution-init", offline: true, workdir: filepath.Join(dir, "initialization"), executionControl: filepath.Join(dir, "control.yaml")}
	writeYAML(t, opts.config, cfg)
	writeYAML(t, opts.executionControl, &config.Config{Storage: config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "independent-control")}})
	return opts, cfg
}

func TestExecutionCLIInitializesOnlyByExplicitOfflineCommandAndRetriesExactTicket(t *testing.T) {
	opts, cfg := executionOptions(t)
	bad := opts
	bad.executionControl = ""
	if err := run(context.Background(), bad, io.Discard); err == nil {
		t.Fatal("initialization without independent authority accepted")
	}
	if _, err := os.Stat(cfg.Database.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid initialization already mutated SQL", err)
	}
	if err := run(context.Background(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), opts, io.Discard); err != nil {
		t.Fatal("exact initialized ticket could not reconcile", err)
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	authority, err := openExecutionControl(context.Background(), opts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := authority.ReadActive(context.Background())
	if err != nil || authority.CheckBinding(context.Background(), db, permit) != nil {
		t.Fatal("explicit initialized database is not bound", err)
	}
	status, err := storageguard.Inspect(context.Background(), db)
	if err != nil || status.MaintenanceID != "" {
		t.Fatal("successful activation retained SQL maintenance", err)
	}
}

func TestExecutionCLIAdoptsHistoryWithoutReplayingUnknownAndRequiresTicketBeforeRestore(t *testing.T) {
	ctx := context.Background()
	opts, cfg := executionOptions(t)
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("runs").Create(map[string]any{"id": "old-run", "status": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("chat_queue_turns").Create(map[string]any{"id": "old-turn", "queue_id": "queue", "state": "unknown", "payload": "immutable-user-body"}).Error; err != nil {
		t.Fatal(err)
	}
	_ = database.Close(db)
	if err := run(ctx, opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	db, err = database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	var binding models.ExecutionDataset
	if err := db.First(&binding).Error; err != nil || binding.QuarantineCount != 2 {
		t.Fatal("old pending work was not quarantined", binding, err)
	}
	var turn struct{ State, Payload string }
	if err := db.Table("chat_queue_turns").Select("state", "payload").Take(&turn, "id = ?", "old-turn").Error; err != nil || turn.State != "unknown" || turn.Payload != "immutable-user-body" {
		t.Fatal("adoption changed unknown dispatch or user body", turn, err)
	}
	// A replica checkpoint alone is insufficient to authorize rollback. The
	// independent marker must be rotated by restore-begin before SQL import.
	opts.target = filepath.Join(filepath.Dir(opts.config), "replica.yaml")
	writeYAML(t, opts.target, &config.Config{Storage: config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "http://objects.invalid", Bucket: "backup", Prefix: "replica"}}})
	opts.workdir = filepath.Join(filepath.Dir(opts.config), "replica")
	opts.mode = "copy"
	state, err := prepareCheckpoint(opts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(opts.workdir, state.Database)
	if err := os.WriteFile(backup, []byte("immutable SQL backup fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.Header.DatabaseSHA256, _ = payloadstore.FileSHA256(backup)
	state.Phase = "complete"
	if err := writeJSON(filepath.Join(opts.workdir, "checkpoint.json"), state); err != nil {
		t.Fatal(err)
	}
	opts.mode = "restore-finalize"
	if err := restoreExecution(ctx, opts, cfg, db, io.Discard); err == nil {
		t.Fatal("SQL checkpoint without external restore-begin ticket accepted")
	}
	authority, err := openExecutionControl(ctx, opts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := authority.Current(ctx)
	opts.mode = "restore-begin"
	if err := restoreExecution(ctx, opts, cfg, db, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Current(ctx); !errors.Is(err, executionlineage.ErrInactive) {
		t.Fatal("restore-begin did not revoke old process authority before SQL import", err)
	}
	if err := checkExecutionRelease(ctx, opts, cfg, db); !errors.Is(err, executionlineage.ErrInactive) {
		t.Fatal("release bypassed inactive execution recovery", err)
	}
	if err := restoreExecution(ctx, opts, cfg, db, io.Discard); err != nil {
		t.Fatal("interrupted restore-begin was not idempotent", err)
	}
	raw, _ := os.ReadFile(filepath.Join(opts.workdir, executionTicketFile))
	var ticket executionlineage.TicketDocument
	if json.Unmarshal(raw, &ticket) != nil || ticket.Marker.Lineage == before {
		t.Fatal("restore did not create a new external lineage")
	}
	ticket.Marker.DatasetID = binding.DatasetID // An operator JSON edit is not authority.
	if err := writeJSON(filepath.Join(opts.workdir, executionTicketFile), ticket); err != nil {
		t.Fatal(err)
	}
	if _, err := readExecutionTicket(ctx, authority, opts.workdir); !errors.Is(err, executionlineage.ErrConflict) {
		t.Fatal("forged recovery ticket was accepted", err)
	}
}
