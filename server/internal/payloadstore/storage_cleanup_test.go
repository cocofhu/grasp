package payloadstore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
)

func TestCleanupRemoteFirstInstallBindsFinalizedEmptyClosureBeforeProbe(t *testing.T) {
	db, storageConfig := closureFreshFixture(t)
	var probes atomic.Int32
	var invalid atomic.Bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pool, _ := db.DB()
		if pool.Stats().InUse != 0 {
			invalid.Store(true)
		}
		var proof PrimaryClosure
		if r.Method != http.MethodHead || r.URL.Path != "/private" ||
			db.Take(&proof).Error != nil || proof.FinalizedAt == nil || proof.Payloads != 0 || proof.Attachments != 0 {
			invalid.Store(true)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		probes.Add(1)
	}))
	t.Cleanup(backend.Close)
	storageConfig.S3.Endpoint = backend.URL
	storageConfig.S3.AccessKeyID = "fixture-access"
	storageConfig.S3.SecretAccessKey = "fixture-secret"
	cfg := &config.Config{Storage: storageConfig}
	storage, err := Open(t.Context(), db, cfg)
	if err != nil {
		t.Fatal("genuine fresh remote installation needs a prior certify cycle", err)
	}
	defer storage.Close()
	if probes.Load() != 1 || invalid.Load() {
		t.Fatal("object probe ran before atomic metadata/closure binding or held SQL", probes.Load(), invalid.Load())
	}
	if err := CheckPrimaryClosure(t.Context(), db, storageConfig); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupRemoteMissingProofOrChangedPrimaryCannotReachProbe(t *testing.T) {
	for _, prior := range []string{"local", "s3"} {
		t.Run(prior, func(t *testing.T) {
			db, cfg := closureFreshFixture(t)
			var calls atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(backend.Close)
			cfg.S3.Endpoint, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey = backend.URL, "fixture-access", "fixture-secret"
			original := cfg
			original.Driver = prior
			location, err := proposedLocation(original)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&location).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := Open(t.Context(), db, &config.Config{Storage: cfg}); err == nil {
				t.Fatal("unverified primary accepted")
			}
			var unchanged Location
			if err := db.First(&unchanged, "name = ?", "primary").Error; err != nil || unchanged.Fingerprint != location.Fingerprint || calls.Load() != 0 {
				t.Fatal("failed initialization changed primary or contacted objects", unchanged, calls.Load(), err)
			}
		})
	}
}

func TestCleanupRemoteFirstInstallCannotInventAnEmptyOriginOnExistingSchema(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	remote, _ := proposedLocation(config.StorageConfig{Driver: "s3", BlobsRoot: t.TempDir(),
		S3: config.S3StorageConfig{Endpoint: "https://unused.invalid", Bucket: "private"}})
	if err := bindLocation(db, remote); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal("unsealed preexisting schema was adopted as a new remote installation", err)
	}
	var count int64
	if err := db.Model(&Location{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed bootstrap persisted a location", count, err)
	}
}
