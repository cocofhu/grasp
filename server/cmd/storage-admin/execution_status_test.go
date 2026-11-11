package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
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
	return options{config: opts.config, mode: "execution-status"}
}

func statusResult(t *testing.T, opts options) (executionlineage.Observation, error) {
	t.Helper()
	var output bytes.Buffer
	err := run(context.Background(), opts, &output)
	var view executionlineage.Observation
	if decodeErr := json.Unmarshal(output.Bytes(), &view); decodeErr != nil {
		t.Fatalf("status did not return machine-readable diagnostic: %s (%v, %v)", output.String(), err, decodeErr)
	}
	return view, err
}

func TestExecutionStatusReadsOnlySQLWithoutObjectRequests(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg.Storage = config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(t.TempDir(), "must-not-create"), S3: config.S3StorageConfig{Endpoint: server.URL, Bucket: "payloads", Prefix: "body"}}
	writeYAML(t, opts.config, cfg)
	sqlBefore, err := os.ReadFile(cfg.Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	view, err := statusResult(t, statusOptions(opts))
	if err != nil || view.Status != "active" || !view.Active || !view.SQLMatches || !view.SQLDatasetPresent || view.Revision < 2 || view.Lineage == "" {
		t.Fatal(view, err)
	}
	sqlAfter, _ := os.ReadFile(cfg.Database.Path)
	if !bytes.Equal(sqlBefore, sqlAfter) {
		t.Fatal("read-only status changed SQL")
	}
	if requests.Load() != 0 {
		t.Fatal("execution status depended on payload/object S3", requests.Load())
	}
	if _, err := os.Stat(cfg.Storage.BlobsRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status attached a payload store", err)
	}
	db, err := connectExecutionStatus(t.Context(), cfg.Database)
	if err != nil || db == nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	defer func() { _ = conn.Close() }()
	if err := db.Exec("CREATE TABLE forbidden_status_write (id INTEGER)").Error; err == nil {
		t.Fatal("SQLite status permitted writes")
	}
}

func TestExecutionStatusEmptyInstallationCreatesNothing(t *testing.T) {
	opts, cfg := executionOptions(t)
	view, err := statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrUninitialized) || view.Status != "uninitialized" || view.Active || view.SQLDatasetPresent {
		t.Fatal(view, err)
	}
	for _, path := range []string{cfg.Database.Path, filepath.Dir(cfg.Database.Path), cfg.Storage.BlobsRoot, opts.workdir} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("status created SQL, payloads or workdir", path, err)
		}
	}
}

func TestExecutionStatusOldSQLDoesNotMigrate(t *testing.T) {
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
	db, err = connectExecutionStatus(t.Context(), cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	tables, _ := db.Migrator().GetTables()
	conn, _ = db.DB()
	_ = conn.Close()
	if !reflect.DeepEqual(tables, []string{"existing_history"}) {
		t.Fatal("status migrated old SQL", tables)
	}
	if err := run(t.Context(), opts, io.Discard); err == nil {
		t.Fatal("initialization silently adopted incomplete history")
	}
}

func TestExecutionStatusMissingSQLAndMutationFlags(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cfg.Database.Path, cfg.Database.Path+".retained"); err != nil {
		t.Fatal(err)
	}
	view, err := statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrUninitialized) || view.Active || view.SQLDatasetPresent {
		t.Fatal(view, err)
	}
	if _, err := os.Stat(cfg.Database.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status recreated SQL", err)
	}
	for _, invalid := range []options{{offline: true}, {activate: true}, {resume: true}, {stopped: "process"}, {target: "target.yaml"}, {executionControl: "must-not-read.yaml"}} {
		invalid.mode = "execution-status"
		if validateOptions(invalid) == nil {
			t.Fatal("status accepted mutation or external control", invalid)
		}
	}
	if _, err := connectExecutionStatus(t.Context(), config.DatabaseConfig{Path: ":memory:"}); err == nil {
		t.Fatal("transient status database accepted")
	}
}

func TestExecutionStatusInterruptedMaintenanceIsInactiveWithoutUnlocking(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	owner := uuid.NewString()
	if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	view, err := statusResult(t, statusOptions(opts))
	if !errors.Is(err, executionlineage.ErrInactive) || view.Status != "inactive" || view.Active || !view.SQLDatasetPresent || !view.MaintenanceActive {
		t.Fatal(view, err)
	}
	if err := storageguard.Check(t.Context(), db, owner); err != nil {
		t.Fatal("status cleared maintenance", err)
	}
	if err := storageguard.Leave(t.Context(), db, owner); err != nil {
		t.Fatal(err)
	}
	view, err = statusResult(t, statusOptions(opts))
	if err != nil || !view.Active || view.MaintenanceActive {
		t.Fatal(view, err)
	}
}
