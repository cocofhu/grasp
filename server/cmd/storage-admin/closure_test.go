package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type adminClosureFixture struct {
	db     *gorm.DB
	cfg    *config.Config
	opts   options
	mode   atomic.Int32 // 0 valid, 1 missing, 2 corrupt
	reads  atomic.Int32
	writes atomic.Int32
}

func newAdminClosureFixture(t *testing.T) *adminClosureFixture {
	t.Helper()
	f := &adminClosureFixture{}
	dir := t.TempDir()
	const body = "original history body"
	digest := sha256.Sum256([]byte(body))
	key := "/private/current/payloads/sha256/" + hex.EncodeToString(digest[:1]) + "/" + hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			f.writes.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		f.reads.Add(1)
		if f.mode.Load() == 1 || r.URL.Path != key {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		actual := body
		if f.mode.Load() == 2 {
			actual = strings.Repeat("x", len(actual))
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(actual)))
		_, _ = w.Write([]byte(actual))
	}))
	t.Cleanup(server.Close)
	f.cfg = &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "original.sqlite")},
		Storage: config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(dir, "blobs"), S3: config.S3StorageConfig{
			Endpoint: server.URL, Bucket: "private", Prefix: "current", AccessKeyID: "private-access-never-in-fingerprint", SecretAccessKey: "private-secret-never-in-fingerprint"}}}
	var err error
	f.db, err = database.OpenWithFiles(f.cfg.Database, filepath.Join(f.cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(f.db) })
	if err := f.db.AutoMigrate(&payloadstore.Location{}); err != nil {
		t.Fatal(err)
	}
	location, _ := payloadstore.PrimaryLocation(f.cfg.Storage)
	if err := f.db.Create(&location).Error; err != nil {
		t.Fatal(err)
	}
	files, _ := filestore.FromDB(f.db)
	ref, _, err := files.Put(t.Context(), strings.NewReader(body), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Table("solo_messages").Create(map[string]any{"id": "original-history", "content": ref}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&models.ExecutionDataset{ID: "primary", DatasetID: "dataset-unchanged", Lineage: "lineage-unchanged", ControlRevision: 12}).Error; err != nil {
		t.Fatal(err)
	}
	f.opts = options{mode: "certify-primary", offline: true, config: filepath.Join(dir, "application.yaml"), workdir: filepath.Join(dir, "certificate")}
	writeYAML(t, f.opts.config, f.cfg)
	return f
}

func TestPrimaryCertificationCLIManualFinalizeAndOriginalRestart(t *testing.T) {
	f := newAdminClosureFixture(t)
	ctx := context.Background()
	var out bytes.Buffer
	if err := run(ctx, f.opts, &out); err != nil || !strings.Contains(out.String(), `"maintenanceActive":true`) {
		t.Fatal(out.String(), err)
	}
	var checkpoint closureCheckpoint
	if raw, err := os.ReadFile(filepath.Join(f.opts.workdir, "primary-closure.json")); err != nil || json.Unmarshal(raw, &checkpoint) != nil {
		t.Fatal(err)
	}
	if err := storageguard.Check(ctx, f.db, checkpoint.ID); err != nil {
		t.Fatal("first successful command released maintenance", err)
	}
	complete := filepath.Join(f.opts.workdir, "primary-closure-complete.json")
	original, err := os.ReadFile(complete)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, f.opts, io.Discard); err == nil {
		t.Fatal("original workdir was adopted without resume")
	}
	f.opts.resume = true
	if err := run(ctx, f.opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	resumed, _ := os.ReadFile(complete)
	if !bytes.Equal(original, resumed) {
		t.Fatal("resume replaced the original receipt")
	}
	reads := f.reads.Load()
	f.opts.finalize = true
	if err := run(ctx, f.opts, io.Discard); err != nil || f.reads.Load() <= reads {
		t.Fatal("explicit finalize skipped original-object re-verification", err)
	}
	var row payloadstore.PrimaryClosure
	if err := f.db.First(&row).Error; err != nil || row.ID != checkpoint.ID || row.FinalizedAt == nil {
		t.Fatal(row, err)
	}
	var control storageguard.Control
	if err := f.db.First(&control).Error; err != nil || control.Owner != "" {
		t.Fatal(control, err)
	}
	var dataset models.ExecutionDataset
	if err := f.db.First(&dataset).Error; err != nil || dataset.DatasetID != "dataset-unchanged" || dataset.Lineage != "lineage-unchanged" || dataset.ControlRevision != 12 {
		t.Fatal("certification reinitialized execution", dataset, err)
	}
	// Simulate a lost final response/local-file publication. SQL already has
	// the original final stamp; retry must not clear a subsequent owner.
	if err := os.WriteFile(complete, original, 0o600); err != nil {
		t.Fatal(err)
	}
	next := uuid.NewString()
	if err := storageguard.Enter(ctx, f.db, next, nil); err != nil {
		t.Fatal(err)
	}
	reads = f.reads.Load()
	out.Reset()
	if err := run(ctx, f.opts, &out); err != nil || !strings.Contains(out.String(), `"originalReceipt":true`) || f.reads.Load() != reads {
		t.Fatal("lost reply did not read the original final receipt", out.String(), err)
	}
	if err := storageguard.Check(ctx, f.db, next); err != nil || f.writes.Load() != 0 {
		t.Fatal("retry cleared other maintenance or changed objects", err, f.writes.Load())
	}
}

func TestPrimaryCertificationCLIResumesMissingObjectsWithoutLocalFallback(t *testing.T) {
	f := newAdminClosureFixture(t)
	f.mode.Store(1)
	if err := run(t.Context(), f.opts, io.Discard); err == nil {
		t.Fatal("primary missing object passed through retained local bytes")
	}
	var control storageguard.Control
	if err := f.db.First(&control).Error; err != nil || uuid.Validate(control.Owner) != nil {
		t.Fatal("failure lost durable original maintenance", err)
	}
	f.opts.resume = true
	f.mode.Store(0)
	if err := run(t.Context(), f.opts, io.Discard); err != nil {
		t.Fatal("same intent could not resume after object repair", err)
	}
	f.opts.finalize = true
	f.mode.Store(2)
	if err := run(t.Context(), f.opts, io.Discard); err == nil {
		t.Fatal("corruption at explicit finalize was ignored")
	}
	if err := storageguard.Check(t.Context(), f.db, control.Owner); err != nil {
		t.Fatal(err)
	}
	f.mode.Store(0)
	if err := run(t.Context(), f.opts, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestPrimaryCertificationCLIRequiresOriginalCompletionBeforeFinalize(t *testing.T) {
	f := newAdminClosureFixture(t)
	if err := run(t.Context(), f.opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	complete := filepath.Join(f.opts.workdir, "primary-closure-complete.json")
	original, _ := os.ReadFile(complete)
	f.opts.resume, f.opts.finalize = true, true
	if err := os.Remove(complete); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), f.opts, io.Discard); err == nil {
		t.Fatal("missing original file receipt was silently reconstructed during finalize")
	}
	var row payloadstore.PrimaryClosure
	if err := json.Unmarshal(original, &row); err != nil {
		t.Fatal(err)
	}
	row.ReferenceSHA256 = strings.Repeat("a", 64)
	if err := writeJSON(complete, row); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), f.opts, io.Discard); err == nil {
		t.Fatal("changed completed receipt authorized maintenance release")
	}
	if err := os.WriteFile(complete, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), f.opts, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestPrimaryCertificationIntentExcludesCredentialsButFreezesDatabaseAndPhysicalPrefix(t *testing.T) {
	f := newAdminClosureFixture(t)
	lock, err := lockExecutionWorkdir(f.opts.workdir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	state, err := prepareClosure(f.opts, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(f.opts.workdir, "primary-closure.json"))
	if strings.Contains(string(raw), "private-secret") || strings.Contains(string(raw), "private-access") {
		t.Fatal("credentials entered the durable intent")
	}
	f.opts.resume = true
	f.cfg.Storage.S3.AccessKeyID, f.cfg.Storage.S3.SecretAccessKey = "rotated", "changed"
	if got, err := prepareClosure(f.opts, f.cfg); err != nil || got != state {
		t.Fatal("credential rotation changed storage identity", err)
	}
	f.cfg.Storage.S3.Prefix = "another-physical-primary"
	if _, err := prepareClosure(f.opts, f.cfg); err == nil {
		t.Fatal("different physical primary reused original maintenance")
	}
	f.cfg.Storage.S3.Prefix = "current"
	f.cfg.Database.Path = filepath.Join(t.TempDir(), "different.sqlite")
	if _, err := prepareClosure(f.opts, f.cfg); err == nil {
		t.Fatal("different SQL database reused original intent")
	}
	a, err := closureDatabaseIdentity(config.DatabaseConfig{Driver: "mysql", DSN: "first:one@tcp(db:3306)/app?parseTime=true"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := closureDatabaseIdentity(config.DatabaseConfig{Driver: "mysql", DSN: "second:two@tcp(db:3306)/app?parseTime=true"})
	if err != nil || a != b {
		t.Fatal("SQL credentials entered its identity", err)
	}
	if _, err := closureDatabaseIdentity(config.DatabaseConfig{Driver: "mysql", DSN: "invalid"}); err == nil {
		t.Fatal("invalid SQL identity accepted")
	}
}

func TestPrimaryCertificationModeRejectsUnsafeFlags(t *testing.T) {
	valid := options{mode: "certify-primary", offline: true, workdir: "private"}
	if err := validateOptions(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*options){
		func(o *options) { o.offline = false },
		func(o *options) { o.workdir = "" },
		func(o *options) { o.target = "destination" },
		func(o *options) { o.activate = true },
		func(o *options) { o.executionControl = "control.yaml" },
		func(o *options) { o.finalize = true },
		func(o *options) { o.mode, o.finalize = "upgrade", true },
	} {
		bad := valid
		mutate(&bad)
		if err := validateOptions(bad); err == nil {
			t.Fatal("unsafe certificate mode accepted", bad)
		}
	}
	valid.resume, valid.finalize = true, true
	if err := validateOptions(valid); err != nil {
		t.Fatal(err)
	}
}
