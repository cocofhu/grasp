package formatguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/google/uuid"
)

func TestCleanupChecksFormatAndFinalizedPrimaryBeforeWritesOrObjectIO(t *testing.T) {
	for _, proof := range []string{"missing", "not-finalized", "finalized", "wrong-physical"} {
		t.Run(proof, func(t *testing.T) {
			var calls atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(backend.Close)
			sql, db := freshFixture(t)
			cfg := &config.Config{Database: sql, Storage: config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Dir(sql.Path),
				S3: config.S3StorageConfig{Endpoint: backend.URL, Bucket: "private", Prefix: "verified"}}}
			if err := db.AutoMigrate(&payloadstore.Location{}, &payloadstore.PrimaryClosure{}); err != nil {
				t.Fatal(err)
			}
			location, err := payloadstore.PrimaryLocation(cfg.Storage)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&location).Error; err != nil {
				t.Fatal(err)
			}
			if proof != "missing" {
				now := time.Now().UTC()
				row := payloadstore.PrimaryClosure{ID: uuid.NewString(), Protocol: payloadstore.PrimaryClosureProtocol,
					PhysicalFingerprint: location.Fingerprint, ReferenceSHA256: strings.Repeat("a", 64), CompletedAt: now}
				if proof != "not-finalized" {
					row.FinalizedAt = &now
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			// This new body is deliberately absent from the frozen certificate
			// digest. Normal work does not invalidate a verified physical primary.
			if err := db.Create(&models.SoloMessage{ID: "new-work", Content: "canonical later body"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Close(db); err != nil {
				t.Fatal(err)
			}
			if proof == "wrong-physical" {
				cfg.Storage.S3.Prefix = "different-primary"
			}
			before := treeDigest(t, filepath.Dir(sql.Path))
			err = CheckConfiguration(t.Context(), cfg)
			if proof == "finalized" {
				if err != nil {
					t.Fatal("valid proof required live backend or frozen-set equality", err)
				}
			} else if !errors.Is(err, ErrMigrationRequired) {
				t.Fatal("ineligible primary reached runtime startup", err)
			}
			if after := treeDigest(t, filepath.Dir(sql.Path)); !reflect.DeepEqual(before, after) || calls.Load() != 0 {
				t.Fatal("preflight wrote SQL/files or contacted objects", calls.Load(), before, after)
			}
		})
	}
}

func TestCleanupMissingFreshDatabaseCanReachOriginCaptureWithoutCreatingFiles(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "new", "database.sqlite")},
		Storage: config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(root, "new", "blobs"),
			S3: config.S3StorageConfig{Endpoint: "https://unused.invalid", Bucket: "private"}}}
	before := treeDigest(t, root)
	if err := CheckConfiguration(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if after := treeDigest(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("preflight pre-created fresh files", before, after)
	}
	if err := CheckConfiguration(t.Context(), nil); !errors.Is(err, ErrMigrationRequired) {
		t.Fatal("missing configuration passed startup", err)
	}
}
