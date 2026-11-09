package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func statusOptions(opts options) options {
	return options{config: opts.config, executionControl: opts.executionControl, mode: "execution-status"}
}

func statusResult(t *testing.T, opts options) (executionlineage.Observation, error) {
	t.Helper()
	var output bytes.Buffer
	err := run(context.Background(), opts, &output)
	var view executionlineage.Observation
	if decodeErr := json.Unmarshal(output.Bytes(), &view); decodeErr != nil {
		t.Fatalf("status did not return its machine-readable diagnostic: %s (%v, %v)", output.String(), err, decodeErr)
	}
	return view, err
}

func controlFiles(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = sha256.Sum256(raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestExecutionStatusReadsInitializedConfigWithoutChangingControlOrSQL(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := run(context.Background(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	control, err := loadTarget(opts.executionControl)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExecutionControl = &control
	writeYAML(t, opts.config, cfg)
	before := controlFiles(t, control.BlobsRoot)
	sqlBefore, err := os.ReadFile(cfg.Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	status := statusOptions(opts)
	status.executionControl = "" // Normal pod configuration, including env overrides.
	view, err := statusResult(t, status)
	if err != nil || view.Status != "active" || !view.Active || !view.SQLMatches || !view.SQLDatasetPresent || view.Revision < 2 || view.Lineage == "" {
		t.Fatal("initialized installation was not verified", view, err)
	}
	if after := controlFiles(t, control.BlobsRoot); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only status mutated control objects or created a CAS probe", before, after)
	}
	sqlAfter, _ := os.ReadFile(cfg.Database.Path)
	if !bytes.Equal(sqlBefore, sqlAfter) {
		t.Fatal("status changed application SQL")
	}
	db, err := connectExecutionStatus(context.Background(), cfg.Database)
	if err != nil || db == nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	defer func() { _ = conn.Close() }()
	if err := db.Exec("CREATE TABLE forbidden_status_write (id INTEGER)").Error; err == nil {
		t.Fatal("SQLite diagnostic connection permitted schema writes")
	}
}

func TestExecutionStatusEmptyInstallationCreatesNothing(t *testing.T) {
	opts, cfg := executionOptions(t)
	view, err := statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrUninitialized) || view.Status != "uninitialized" || view.Active || view.SQLDatasetPresent {
		t.Fatal(view, err)
	}
	for _, path := range []string{cfg.Database.Path, filepath.Dir(cfg.Database.Path), cfg.Storage.BlobsRoot, filepath.Join(filepath.Dir(opts.config), "independent-control"), opts.workdir} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("status created a database, directory, lock or marker", path, err)
		}
	}
}

func TestExecutionStatusOldSQLDoesNotMigrateAndMissingMarkerPreservesDatasetEvidence(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(cfg.Database.Path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE existing_history (id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	_ = conn.Close()
	view, err := statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrUninitialized) || view.SQLDatasetPresent {
		t.Fatal(view, err)
	}
	db, err = connectExecutionStatus(context.Background(), cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	tables, _ := db.Migrator().GetTables()
	conn, _ = db.DB()
	_ = conn.Close()
	if !reflect.DeepEqual(tables, []string{"existing_history"}) {
		t.Fatal("old schema was migrated by status", tables)
	}
	if err := run(context.Background(), opts, io.Discard); err == nil {
		// This deliberately incomplete legacy schema is not an adoptable install.
		t.Fatal("initialization unexpectedly adopted incomplete old SQL")
	}
	// Use a separate properly initialized installation to test a lost marker.
	opts, cfg = executionOptions(t)
	if err := run(context.Background(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(filepath.Dir(opts.config), "independent-control", "execution-namespace.json")
	if err := os.Rename(marker, marker+".retained"); err != nil {
		t.Fatal(err)
	}
	view, err = statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrUninitialized) || view.Status != "uninitialized" || !view.SQLDatasetPresent || view.Active {
		t.Fatal("lost control marker hid the existing SQL dataset", view, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status recreated a missing marker", err)
	}
	_ = cfg
}

func TestExecutionStatusActiveMarkerWithMissingSQLRefusesAndRejectsMutationFlags(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := run(context.Background(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cfg.Database.Path, cfg.Database.Path+".retained"); err != nil {
		t.Fatal(err)
	}
	view, err := statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrUnbound) || view.Status != "unbound" || view.Active || view.SQLDatasetPresent {
		t.Fatal("existing external authority admitted absent SQL", view, err)
	}
	if _, err := os.Stat(cfg.Database.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status created missing SQL", err)
	}
	invalid := []options{{offline: true}, {activate: true}, {resume: true}, {stopped: "process"}, {target: "target.yaml"}}
	for _, candidate := range invalid {
		candidate.mode = "execution-status"
		if validateOptions(candidate) == nil {
			t.Fatal("read-only status accepted mutation intent", candidate)
		}
	}
	if _, err := connectExecutionStatus(context.Background(), config.DatabaseConfig{Path: ":memory:"}); err == nil {
		t.Fatal("transient database admitted as durable execution status")
	}
}

func TestExecutionStatusInterruptedOfflineFinalizationIsInactiveWithoutClearingMaintenance(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := run(context.Background(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	view, err := statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrInactive) || view.Status != "inactive" || view.Active || !view.SQLMatches || !view.SQLDatasetPresent || !view.MaintenanceActive {
		t.Fatal("interrupted activation was declared ready", view, err)
	}
	if err := storageguard.Check(context.Background(), db, owner); err != nil {
		t.Fatal("read-only status cleared maintenance", err)
	}
	if err := storageguard.Leave(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	view, err = statusResult(t, statusOptions(opts))
	if err != nil || !view.Active || view.MaintenanceActive {
		t.Fatal("explicit finalization did not permit status", view, err)
	}
}
