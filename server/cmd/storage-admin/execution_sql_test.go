package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSQLExecutionRejectsObsoleteControlBeforeReadingAnyConfiguration(t *testing.T) {
	opts, cfg := executionOptions(t)
	opts.config = filepath.Join(t.TempDir(), "absent.yaml")
	opts.executionControl = filepath.Join(t.TempDir(), "must-not-open.yaml")
	err := run(t.Context(), opts, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--execution-control is no longer supported") {
		t.Fatal("obsolete control flag did not reject before configuration I/O", err)
	}
	if _, err := os.Stat(cfg.Database.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected external control created SQL", err)
	}
}

func TestSQLExecutionInitializationNeverContactsPayloadS3(t *testing.T) {
	opts, cfg := executionOptions(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg.Storage.Driver = "s3"
	cfg.Storage.S3 = config.S3StorageConfig{Endpoint: server.URL, Bucket: "business-payloads", Prefix: "history"}
	writeYAML(t, opts.config, cfg)
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("SQL initialization depended on object storage", err)
	}
	if requests.Load() != 0 {
		t.Fatal("initialization issued independent or payload object requests", requests.Load())
	}
	var operation initialization
	if err := readExecutionJSON(filepath.Join(opts.workdir, "execution-initialization.json"), &operation); err != nil || operation.Version != 2 || !validExecutionSHA(operation.TicketSHA256) {
		t.Fatal("initialization did not persist its SQL-only checkpoint", operation, err)
	}
}

func TestSQLExecutionRejectsLegacyTicketsWithoutCreatingSQL(t *testing.T) {
	for _, name := range []string{executionTicketFile, "execution-initialization.json"} {
		t.Run(name, func(t *testing.T) {
			opts, cfg := executionOptions(t)
			if err := os.Mkdir(opts.workdir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(opts.workdir, name)
			if err := writeJSON(path, map[string]any{"version": 1, "id": uuid.NewString()}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			err := run(t.Context(), opts, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "original tool") {
				t.Fatal("legacy workdir silently adopted", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("legacy recovery evidence was rewritten")
			}
			if _, err := os.Stat(cfg.Database.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("legacy workdir rejection changed SQL", err)
			}
		})
	}
}

func TestSQLExecutionRecoversOnlyUnpublishedTicketFromOriginalIntent(t *testing.T) {
	opts, cfg := executionOptions(t)
	if err := config.Load(opts.config); err != nil {
		t.Fatal(err)
	}
	cfg = config.GetConfig() // Freeze the same normalized configuration as run.
	if err := os.MkdirAll(opts.workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, _ := executionWorkdir(opts.workdir)
	fingerprint, _ := executionConfigurationFingerprint(cfg)
	operation := initialization{Version: 2, Protocol: sqlMaintenanceProtocol, ID: uuid.NewString(), Workdir: dir, ConfigurationSHA256: fingerprint}
	if err := writeJSON(filepath.Join(opts.workdir, "execution-initialization.json"), operation); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Enter(t.Context(), db, operation.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := initializeEmptyCore(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := executionlineage.EnsureRecoverySchema(t.Context(), db, operation.ID); err != nil {
		t.Fatal(err)
	}
	authority, _ := executionlineage.OpenSQL(t.Context(), db)
	ticket, err := authority.Initialize(t.Context(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	// The SQL intent committed, but the process stopped before writing a ticket
	// or its hash. Retrying must recover that exact tuple, never mint another.
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("original interrupted initialization could not reconcile", err)
	}
	document, err := readExecutionTicketDocument(opts.workdir)
	if err != nil || document != ticket.Document() {
		t.Fatal("unpublished ticket retry changed its SQL generation", document, err)
	}
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("completed exact retry failed", err)
	}
	for _, problem := range []string{"copied-workdir", "changed-configuration", "lost-published-ticket"} {
		t.Run(problem, func(t *testing.T) {
			candidate := opts
			switch problem {
			case "copied-workdir":
				candidate.workdir = filepath.Join(t.TempDir(), "copy")
				copyExecutionWorkdir(t, opts.workdir, candidate.workdir)
			case "changed-configuration":
				changed := *cfg
				changed.Storage.BlobsRoot = filepath.Join(t.TempDir(), "different-payloads")
				candidate.config = filepath.Join(t.TempDir(), "changed.yaml")
				writeYAML(t, candidate.config, &changed)
			case "lost-published-ticket":
				if err := os.Remove(filepath.Join(opts.workdir, executionTicketFile)); err != nil {
					t.Fatal(err)
				}
			}
			if err := run(t.Context(), candidate, io.Discard); err == nil {
				t.Fatal("changed or incomplete original recovery was accepted")
			}
		})
	}
	db, err = database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	if err := storageguard.Check(t.Context(), db, operation.ID); err != nil {
		t.Fatal("lost published ticket cleared maintenance", err)
	}
	var rows []models.ExecutionRecovery
	if err := db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].ID != operation.ID {
		t.Fatal("retry minted another recovery operation", rows, err)
	}
}

func copyExecutionWorkdir(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == ".lock" || entry.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

type sqlRestoreFixture struct {
	cfg      *config.Config
	opts     options
	db       *gorm.DB
	state    checkpoint
	requests atomic.Int64
	missing  atomic.Bool
}

func newSQLRestoreFixture(t *testing.T, advanceBeforeBegin bool) *sqlRestoreFixture {
	t.Helper()
	opts, cfg := executionOptions(t)
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	const body = "original chat body retained through SQL restore"
	files, _ := filestore.FromDB(db)
	ref, _, err := files.Put(t.Context(), strings.NewReader(body), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("solo_messages").Create(map[string]any{"id": "history", "session_id": "owner", "content": ref}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	db, err = database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	f := &sqlRestoreFixture{cfg: cfg, db: db}
	digest := sha256.Sum256([]byte(body))
	sha := hex.EncodeToString(digest[:])
	key := "payloads/sha256/" + sha[:2] + "/" + sha
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.Method == http.MethodHead && r.URL.Path == "/backup" {
			return // S3 HeadBucket validates only the business payload target.
		}
		if r.Method == http.MethodGet && r.URL.Path == "/backup" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprintf(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>replica/%s</Key><Size>%d</Size></Contents></ListBucketResult>`, key, len(body))
			return
		}
		if f.missing.Load() || r.URL.Path != "/backup/replica/"+key || r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("X-Amz-Meta-Grasp-Sha256", sha)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(server.Close)
	pathStyle := true
	target := config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: server.URL, Bucket: "backup", Prefix: "replica", PathStyle: &pathStyle, AccessKeyID: "synthetic", SecretAccessKey: "synthetic"}}
	opts.target = filepath.Join(filepath.Dir(opts.config), "replica.yaml")
	writeYAML(t, opts.target, &config.Config{Storage: target})
	opts.mode, opts.workdir = "copy", filepath.Join(filepath.Dir(opts.config), "replica")
	f.state, err = prepareCheckpoint(opts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Enter(t.Context(), db, f.state.Header.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := payloadstore.Open(t.Context(), db, cfg); err != nil {
		t.Fatal(err)
	}
	if err := snapshotDatabase(t.Context(), db, cfg, opts.workdir, &f.state); err != nil {
		t.Fatal(err)
	}
	objects, err := payloadstore.OpenObjects(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	var manifest bytes.Buffer
	f.state.Summary, err = payloadstore.WriteReplica(t.Context(), db, objects, objects, f.state.Header, &manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.workdir, "manifest.jsonl"), manifest.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	f.state.Phase = "complete"
	if err := writeJSON(filepath.Join(opts.workdir, "checkpoint.json"), f.state); err != nil {
		t.Fatal(err)
	}
	if advanceBeforeBegin {
		// A valid older backup can predate the dataset active at restore-begin.
		// Advance the source through an explicit offline generation first.
		authority, _ := executionlineage.OpenSQL(t.Context(), db)
		owner := uuid.NewString()
		if err := storageguard.Leave(t.Context(), db, f.state.Header.ID); err != nil {
			t.Fatal(err)
		}
		if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
			t.Fatal(err)
		}
		ticket, err := authority.BeginRestore(t.Context(), owner, f.state.Header.DatabaseSHA256)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, err := authority.QuarantineTx(tx, owner, ticket)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := authority.Activate(t.Context(), db, owner, ticket); err != nil {
			t.Fatal(err)
		}
		if err := storageguard.Leave(t.Context(), db, owner); err != nil {
			t.Fatal(err)
		}
	}
	opts.mode = "restore-begin"
	if err := restoreExecution(t.Context(), opts, cfg, db, io.Discard); err != nil {
		t.Fatal(err)
	}
	f.opts = opts
	return f
}

func TestSQLRestoreCannotReleaseThroughCopyVerifyOrLostTicket(t *testing.T) {
	f := newSQLRestoreFixture(t, false)
	reads := f.requests.Load()
	for _, removeTicket := range []bool{false, true} {
		if removeTicket {
			if err := os.Remove(filepath.Join(f.opts.workdir, executionTicketFile)); err != nil {
				t.Fatal(err)
			}
		}
		for _, mode := range []string{"verify", "copy", "release", "restore-begin"} {
			opts := f.opts
			opts.mode, opts.resume = mode, true
			if mode == "copy" {
				state, _ := prepareCheckpoint(opts, f.cfg)
				state.Phase = "verified" // Exercise the resumable-copy path too.
				if err := writeJSON(filepath.Join(opts.workdir, "checkpoint.json"), state); err != nil {
					t.Fatal(err)
				}
			}
			err := run(t.Context(), opts, io.Discard)
			if mode == "restore-begin" && !removeTicket {
				if err != nil {
					t.Fatal("exact restore-begin retry failed", err)
				}
			} else if err == nil {
				t.Fatal("unfinished restore escaped via", mode, "lost ticket", removeTicket)
			}
			if err := storageguard.Check(t.Context(), f.db, f.state.Header.ID); err != nil {
				t.Fatal("unfinished restore lost maintenance", mode, err)
			}
			if _, err := storageguard.Register(t.Context(), f.db); err == nil {
				t.Fatal("unfinished restore allowed a writer", mode)
			}
		}
	}
	if f.requests.Load() != reads {
		t.Fatal("pending recovery attempted payload I/O before its fence", reads, f.requests.Load())
	}
}

func TestSQLRestoreVerifiesBackupAndReplicaBeforeRebindingImportedSQL(t *testing.T) {
	f := newSQLRestoreFixture(t, false)
	backup, err := os.ReadFile(filepath.Join(f.opts.workdir, f.state.Database))
	if err != nil {
		t.Fatal(err)
	}
	restored := *f.cfg
	restored.Database.Path = filepath.Join(t.TempDir(), "restored.sqlite")
	if err := os.WriteFile(restored.Database.Path, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	restoredConfig := filepath.Join(t.TempDir(), "restored.yaml")
	writeYAML(t, restoredConfig, &restored)
	opts := f.opts
	opts.mode, opts.config = "restore-finalize", restoredConfig
	target, err := database.ConnectWithFiles(restored.Database, filepath.Join(restored.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(target) }()
	var before models.ExecutionDataset
	if err := target.First(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, problem := range []string{"changed-backup", "missing-replica"} {
		t.Run(problem, func(t *testing.T) {
			switch problem {
			case "changed-backup":
				if err := os.WriteFile(filepath.Join(opts.workdir, f.state.Database), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing-replica":
				if err := os.WriteFile(filepath.Join(opts.workdir, f.state.Database), backup, 0o600); err != nil {
					t.Fatal(err)
				}
				f.missing.Store(true)
			}
			if err := run(t.Context(), opts, io.Discard); err == nil {
				t.Fatal("unverified restore finalized", problem)
			}
			var pending int64
			if err := target.Model(&models.ExecutionRecovery{}).Where("id = ?", f.state.Header.ID).Count(&pending).Error; err != nil || pending != 0 {
				t.Fatal("unverified backup re-registered imported SQL intent", pending, err)
			}
			var after models.ExecutionDataset
			if err := target.First(&after).Error; err != nil || after != before {
				t.Fatal("unverified restore changed the original SQL generation", err)
			}
			if err := storageguard.Check(t.Context(), target, f.state.Header.ID); err != nil {
				t.Fatal("failed restore released imported maintenance", err)
			}
		})
	}
	f.missing.Store(false)
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("verified original restore could not complete", err)
	}
	authority, _ := executionlineage.OpenSQL(t.Context(), target)
	permit, err := authority.ReadActive(t.Context())
	if err != nil || permit.DatasetID() == before.DatasetID || permit.Lineage() == before.Lineage {
		t.Fatal("verified restoration did not rotate the SQL binding", err)
	}
	var message struct{ Content string }
	if err := target.Table("solo_messages").Select("content").Take(&message, "id = ?", "history").Error; err != nil || !filestore.IsRef(message.Content) {
		t.Fatal("restore altered original message identity or reference", message, err)
	}
	status, err := storageguard.Inspect(t.Context(), target)
	if err != nil || status.MaintenanceID != "" {
		t.Fatal("successful verified restore retained maintenance", status, err)
	}
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("same verified restore could not recover a lost response", err)
	}
	// Normal work can grow the reference set after completion. A receipt read
	// must not attempt a new full replica verification or quarantine that work.
	if err := target.Table("solo_messages").Create(map[string]any{"id": "new-history", "session_id": "owner", "content": message.Content}).Error; err != nil {
		t.Fatal(err)
	}
	reads := f.requests.Load()
	if err := run(t.Context(), opts, io.Discard); err != nil || f.requests.Load() != reads {
		t.Fatal("completed original receipt repeated object verification after normal new work", err)
	}
	nextOwner := uuid.NewString()
	if err := storageguard.Enter(t.Context(), target, nextOwner, nil); err != nil {
		t.Fatal(err)
	}
	reads = f.requests.Load()
	if err := run(t.Context(), opts, io.Discard); !errors.Is(err, executionlineage.ErrInactive) || f.requests.Load() != reads {
		t.Fatal("completed retry reported active during another maintenance or repeated object I/O", err)
	}
	if err := storageguard.Check(t.Context(), target, nextOwner); err != nil {
		t.Fatal("completed receipt cleared a different maintenance owner", err)
	}
	if err := storageguard.Leave(t.Context(), target, nextOwner); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Enter(t.Context(), target, f.state.Header.ID, nil); err != nil {
		t.Fatal(err)
	}
	runtimeID := uuid.NewString()
	if err := target.Create(&storageguard.Runtime{ID: runtimeID, Instance: "must-stop-before-release"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), opts, io.Discard); err == nil {
		t.Fatal("completed recovery released maintenance with a registered runtime")
	}
	if err := storageguard.Check(t.Context(), target, f.state.Header.ID); err != nil {
		t.Fatal("runtime refusal cleared maintenance", err)
	}
	if err := target.Delete(&storageguard.Runtime{}, "id = ?", runtimeID).Error; err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("completed same-owner receipt could not finish Leave after runtime stopped", err)
	}
	completeBefore, err := os.ReadFile(filepath.Join(opts.workdir, "execution-complete.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(target); err != nil {
		t.Fatal(err)
	}
	// Importing the original SQL a second time must require a new recovery
	// operation. A completed workdir cannot re-activate its old generation.
	if err := os.WriteFile(restored.Database.Path, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), opts, io.Discard); err == nil {
		t.Fatal("completed recovery ticket was reused after another SQL rollback")
	}
	rewound, err := database.ConnectWithFiles(restored.Database, filepath.Join(restored.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(rewound) }()
	if err := storageguard.Check(t.Context(), rewound, f.state.Header.ID); err != nil {
		t.Fatal("rejected second import cleared maintenance", err)
	}
	var pending int64
	if err := rewound.Model(&models.ExecutionRecovery{}).Where("id = ?", f.state.Header.ID).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatal("completed ticket re-registered SQL intent after rollback", pending, err)
	}
	completeAfter, _ := os.ReadFile(filepath.Join(opts.workdir, "execution-complete.json"))
	if !bytes.Equal(completeBefore, completeAfter) {
		t.Fatal("rejected second import rewrote completion evidence")
	}
	// Losing the completion file must not erase the earlier monotonic target
	// registration. The same rolled-back SQL still cannot reuse this ticket.
	if err := os.Remove(filepath.Join(opts.workdir, "execution-complete.json")); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), opts, io.Discard); err == nil {
		t.Fatal("lost completion plus SQL rollback reused the original generation")
	}
	if err := storageguard.Check(t.Context(), rewound, f.state.Header.ID); err != nil {
		t.Fatal("lost-completion failure cleared maintenance", err)
	}
	if err := rewound.Model(&models.ExecutionRecovery{}).Where("id = ?", f.state.Header.ID).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatal("lost completion caused recovery intent to be registered again", pending, err)
	}
}

func TestSQLRestoreOlderBackupKeepsActualImportedSourceOnCompletedRetry(t *testing.T) {
	f := newSQLRestoreFixture(t, true)
	backup, err := os.ReadFile(filepath.Join(f.opts.workdir, f.state.Database))
	if err != nil {
		t.Fatal(err)
	}
	restored := *f.cfg
	restored.Database.Path = filepath.Join(t.TempDir(), "older-backup.sqlite")
	if err := os.WriteFile(restored.Database.Path, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	opts := f.opts
	opts.mode, opts.config = "restore-finalize", filepath.Join(t.TempDir(), "restored.yaml")
	writeYAML(t, opts.config, &restored)
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("valid older backup could not restore", err)
	}
	db, err := database.ConnectWithFiles(restored.Database, filepath.Join(restored.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	var binding models.ExecutionDataset
	if err := db.First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	document, err := readExecutionTicketDocument(opts.workdir)
	if err != nil || binding.SourceDatasetID == document.Marker.PreviousDatasetID || binding.SourceLineage == document.Marker.PreviousLineage {
		t.Fatal("fixture did not cover an older imported source", binding, err)
	}
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("completed older-backup retry compared its source to the later live dataset", err)
	}
	var after models.ExecutionDataset
	if err := db.First(&after).Error; err != nil || after != binding {
		t.Fatal("completed retry changed the imported source", after, err)
	}
}

func TestSQLRestoreResumesTargetRegistrationOnBothSidesOfLocalPhaseWrite(t *testing.T) {
	for _, phaseWritten := range []bool{false, true} {
		t.Run(fmt.Sprint(phaseWritten), func(t *testing.T) {
			f := newSQLRestoreFixture(t, false)
			backup, err := os.ReadFile(filepath.Join(f.opts.workdir, f.state.Database))
			if err != nil {
				t.Fatal(err)
			}
			restored := *f.cfg
			restored.Database.Path = filepath.Join(t.TempDir(), "restored.sqlite")
			if err := os.WriteFile(restored.Database.Path, backup, 0o600); err != nil {
				t.Fatal(err)
			}
			opts := f.opts
			opts.mode, opts.config = "restore-finalize", filepath.Join(t.TempDir(), "restored.yaml")
			writeYAML(t, opts.config, &restored)
			db, err := database.ConnectWithFiles(restored.Database, filepath.Join(restored.Storage.BlobsRoot, "payloads"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close(db) }()
			target, _ := loadTarget(opts.target)
			objects, err := payloadstore.OpenObjects(t.Context(), target)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyReplica(t.Context(), db, objects, opts.workdir, &f.state); err != nil {
				t.Fatal(err)
			}
			document, err := readExecutionTicketDocument(opts.workdir)
			if err != nil {
				t.Fatal(err)
			}
			authority, _ := executionlineage.OpenSQL(t.Context(), db)
			if _, err := authority.ResumeImported(t.Context(), document, f.state.Header.DatabaseSHA256); err != nil {
				t.Fatal(err)
			}
			var intent executionRestore
			intentFile := filepath.Join(opts.workdir, "execution-restore.json")
			if err := readExecutionJSON(intentFile, &intent); err != nil || intent.TargetRegistrationStarted {
				t.Fatal("fixture did not stop before target publication", err)
			}
			if err := config.Load(opts.config); err != nil {
				t.Fatal(err)
			}
			intent.TargetDatabaseSHA256, err = closureDatabaseIdentity(config.GetConfig().Database)
			if err != nil {
				t.Fatal(err)
			}
			if phaseWritten {
				intent.TargetRegistrationStarted = true
			}
			if err := writeJSON(intentFile, intent); err != nil {
				t.Fatal(err)
			}
			// No activation or quarantine preceded this simulated interruption.
			if _, err := authority.ReadActive(t.Context()); !errors.Is(err, executionlineage.ErrInactive) {
				t.Fatal("pending target registration admitted execution", err)
			}
			if err := run(t.Context(), opts, io.Discard); err != nil {
				t.Fatal("normal interrupted registration could not resume", err)
			}
			var saved models.ExecutionRecovery
			if err := db.Take(&saved, "id = ?", f.state.Header.ID).Error; err != nil || saved.State != "finalized" || saved.FinalizedAt == nil {
				t.Fatal("retry lost the original target recovery row", saved, err)
			}
			after, err := readExecutionTicketDocument(opts.workdir)
			if err != nil || after != document {
				t.Fatal("retry changed the original recovery generation", err)
			}
		})
	}
}

func TestExecutionConfigurationFingerprintTracksIdentityNotCredentials(t *testing.T) {
	base := config.Config{
		Database: config.DatabaseConfig{Driver: "mysql", DSN: "first:one@tcp(db:3306)/app?parseTime=true"},
		Storage:  config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://objects.example", Bucket: "payloads", Prefix: "grasp", AccessKeyID: "a", SecretAccessKey: "b"}},
	}
	want, err := executionConfigurationFingerprint(&base)
	if err != nil {
		t.Fatal(err)
	}
	same := base
	same.Database.DSN = "second:two@tcp(db:3306)/app?parseTime=true&timeout=5s"
	same.Storage.S3.AccessKeyID, same.Storage.S3.SecretAccessKey = "rotated", "rotated"
	same.Storage.MaxFileBytes = 1
	if got, err := executionConfigurationFingerprint(&same); err != nil || got != want {
		t.Fatal("credential or tunable change altered the execution fingerprint", err)
	}
	for name, change := range map[string]func(*config.Config){
		"database": func(c *config.Config) { c.Database.DSN = "first:one@tcp(db:3306)/other?parseTime=true" },
		"host":     func(c *config.Config) { c.Database.DSN = "first:one@tcp(replica:3306)/app?parseTime=true" },
		"bucket":   func(c *config.Config) { c.Storage.S3.Bucket = "elsewhere" },
		"prefix":   func(c *config.Config) { c.Storage.S3.Prefix = "other" },
	} {
		changed := base
		change(&changed)
		if got, err := executionConfigurationFingerprint(&changed); err != nil || got == want {
			t.Fatal("identity change kept the execution fingerprint", name, err)
		}
	}
}
