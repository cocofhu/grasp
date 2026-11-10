package payloadstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func closureFixture(t *testing.T, bodyMode string, databaseConfig ...config.DatabaseConfig) (*gorm.DB, config.StorageConfig, string, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	sqlConfig := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "sql.db")}
	if len(databaseConfig) != 0 {
		sqlConfig = databaseConfig[0]
	}
	db, err := database.OpenWithFiles(sqlConfig, filepath.Join(dir, "blobs", "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	var mutations atomic.Int32
	const body = "original retained history"
	digest := sha256.Sum256([]byte(body))
	key := "/private/current/payloads/sha256/" + hex.EncodeToString(digest[:1]) + "/" + hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if pool.Stats().InUse != 0 {
			mutations.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Path != key || bodyMode == "missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		actual := body
		if bodyMode == "corrupt" {
			actual = strings.Repeat("x", len(body))
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(actual)))
		_, _ = w.Write([]byte(actual))
	}))
	t.Cleanup(server.Close)
	cfg := config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(dir, "blobs"), S3: config.S3StorageConfig{
		Endpoint: server.URL, Bucket: "private", Prefix: "current", AccessKeyID: "fixture-access", SecretAccessKey: "fixture-secret"}}
	location, _ := proposedLocation(cfg)
	if err := seedPriorPrimary(db, location); err != nil {
		t.Fatal(err)
	}
	files, _ := filestore.FromDB(db)
	ref, _, err := files.Put(context.Background(), strings.NewReader(body), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("solo_messages").Create(map[string]any{"id": "original", "session_id": "solo", "content": ref}).Error; err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	return db, cfg, owner, &mutations
}

func TestPrimaryClosureActualS3RetainsOriginalReceiptAndExecution(t *testing.T) {
	db, cfg, owner, mutations := closureFixture(t, "valid")
	ctx := context.Background()
	if err := db.Create(&models.ExecutionDataset{ID: "primary", DatasetID: "original-dataset", Lineage: "original-lineage"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckPrimaryClosure(ctx, db, cfg); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal("remote location alone was treated as archived", err)
	}
	row, err := CertifyPrimary(ctx, db, cfg, owner)
	if err != nil || row.Payloads != 1 || row.CompletedAt.IsZero() || row.FinalizedAt != nil {
		t.Fatal(row, err)
	}
	if err := storageguard.Check(ctx, db, owner); err != nil {
		t.Fatal("certification implicitly released maintenance", err)
	}
	if _, err := storageguard.Register(ctx, db); err == nil {
		t.Fatal("runtime entered before explicit finalize")
	}
	rotated := cfg
	rotated.S3.AccessKeyID, rotated.S3.SecretAccessKey = "rotated-access", "rotated-secret"
	retry, err := CertifyPrimary(ctx, db, rotated, owner)
	if err != nil || retry != row {
		t.Fatal("credential rotation changed physical identity/original receipt", retry, err)
	}
	if err := CheckPrimaryClosure(ctx, db, rotated); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal("unfinalized verification was treated as startup eligibility", err)
	}
	final, err := FinalizePrimaryClosure(ctx, db, cfg, owner)
	if err != nil || final.FinalizedAt == nil || !final.CompletedAt.Equal(row.CompletedAt) {
		t.Fatal(final, err)
	}
	if err := CheckPrimaryClosure(ctx, db, rotated); err != nil {
		t.Fatal(err)
	}
	var control storageguard.Control
	if err := db.First(&control).Error; err != nil || control.Owner != "" {
		t.Fatal(control, err)
	}
	var dataset models.ExecutionDataset
	if err := db.First(&dataset).Error; err != nil || dataset.DatasetID != "original-dataset" || dataset.Lineage != "original-lineage" {
		t.Fatal("certificate changed execution authority", dataset, err)
	}
	// New normal data changes the full reference digest. A historical closure
	// remains eligible; startup must not equate it to today's growing set.
	if err := db.Table("solo_messages").Create(map[string]any{"id": "new", "content": "later message"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckPrimaryClosure(ctx, db, cfg); err != nil {
		t.Fatal("new writes invalidated the original physical closure", err)
	}
	other := uuid.NewString()
	if err := storageguard.Enter(ctx, db, other, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizePrimaryClosure(ctx, db, cfg, owner); err == nil {
		t.Fatal("old finalization cleared a later owner's maintenance")
	}
	if err := storageguard.Check(ctx, db, other); err != nil || mutations.Load() != 0 {
		t.Fatal("object I/O held SQL or mutated objects", err, mutations.Load())
	}
}

func TestPrimaryClosureMissingOrCorruptRemoteCannotUseRetainedLocal(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			db, cfg, owner, mutations := closureFixture(t, mode)
			var raw string
			if err := db.Table("solo_messages").Select("content").Row().Scan(&raw); err != nil {
				t.Fatal(err)
			}
			files, _ := filestore.FromDB(db)
			if bytes, err := files.Load(t.Context(), raw, filestore.Text); err != nil || string(bytes) != "original retained history" {
				t.Fatal("fixture has no retained local fallback", err)
			}
			if _, err := CertifyPrimary(t.Context(), db, cfg, owner); err == nil {
				t.Fatal("retained local file hid missing/corrupt primary")
			}
			var count int64
			if err := db.Model(&PrimaryClosure{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("partial certification published a receipt", err, count)
			}
			if err := storageguard.Check(t.Context(), db, owner); err != nil || mutations.Load() != 0 {
				t.Fatal("failure released maintenance or mutated objects", err, mutations.Load())
			}
		})
	}
}

type closureHookStore struct {
	*replicaStore
	once sync.Once
	hook func()
}

func (s *closureHookStore) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	s.once.Do(s.hook)
	return s.replicaStore.Open(ctx, key)
}

func TestPrimaryClosureRefencesOwnerPrimaryAndSourceAfterObjectReads(t *testing.T) {
	for _, changed := range []string{"owner", "primary", "source", "runtime"} {
		t.Run(changed, func(t *testing.T) {
			db, cfg, owner, _ := closureFixture(t, "valid")
			body := []byte("original retained history")
			digest := sha256.Sum256(body)
			key := "payloads/sha256/" + hex.EncodeToString(digest[:1]) + "/" + hex.EncodeToString(digest[:])
			backend := &closureHookStore{replicaStore: &replicaStore{objects: map[string]replicaBytes{key: {body: body, info: objectstore.Info{Key: key, Size: int64(len(body))}}}}}
			backend.hook = func() {
				var err error
				switch changed {
				case "owner":
					err = db.Model(&storageguard.Control{}).Where("name = ?", "primary").Update("owner", uuid.NewString()).Error
				case "primary":
					err = db.Model(&Location{}).Where("name = ?", "primary").Update("fingerprint", strings.Repeat("a", 64)).Error
				case "source":
					err = db.Table("solo_messages").Where("id = ?", "original").Update("content", "changed during object GET").Error
				case "runtime":
					err = db.Create(&storageguard.Runtime{ID: uuid.NewString(), StartedAt: time.Now().UTC()}).Error
				}
				if err != nil {
					t.Error(err)
				}
			}
			location, _ := proposedLocation(cfg)
			if _, err := certifyPrimaryObjects(t.Context(), db, location, owner, backend); err == nil {
				t.Fatal("changed metadata published a stale physical certificate", changed)
			}
			var count int64
			if err := db.Model(&PrimaryClosure{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("failed fence retained a success receipt", err, count)
			}
		})
	}
}

func TestPrimaryClosureRejectsTransactionLocalOrChangedPrimary(t *testing.T) {
	db, cfg, owner, _ := closureFixture(t, "valid")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := CertifyPrimary(t.Context(), tx, cfg, owner); err == nil {
			t.Fatal("transaction admitted object I/O")
		}
		if _, err := FinalizePrimaryClosure(t.Context(), tx, cfg, owner); err == nil {
			t.Fatal("transaction accepted an outer finalize")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	local := config.StorageConfig{Driver: "local", BlobsRoot: cfg.BlobsRoot}
	if _, err := CertifyPrimary(t.Context(), db, local, owner); err == nil {
		t.Fatal("local certificate pretended to verify a remote primary")
	}
	changed := cfg
	changed.S3.Prefix = "unverified"
	if err := CheckPrimaryClosure(t.Context(), db, changed); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal("configured physical primary changed silently", err)
	}
	if err := CheckPrimaryClosure(t.Context(), db, config.StorageConfig{Driver: "unknown"}); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal(err)
	}
}
