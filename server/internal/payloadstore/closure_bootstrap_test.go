package payloadstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func closureFreshFixture(t *testing.T) (*gorm.DB, config.StorageConfig) {
	t.Helper()
	dir := t.TempDir()
	runtime, err := database.OpenRuntime(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "fresh.sqlite")}, filepath.Join(dir, "blobs", "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.DB.AutoMigrate(&Location{}, &PrimaryClosure{}); err != nil {
		t.Fatal(err)
	}
	return runtime.DB, config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(dir, "blobs"), S3: config.S3StorageConfig{Endpoint: "https://primary.test", Bucket: "private", Prefix: "new"}}
}

func TestEmptyPrimaryBootstrapUsesGenuineOriginAndExactFixedSeedDigest(t *testing.T) {
	db, cfg := closureFreshFixture(t)
	ctx := context.Background()
	if err := CheckPrimaryClosure(ctx, db, cfg); err != nil {
		t.Fatal("genuine empty installation cannot reach first binding", err)
	}
	empty, err := VerifyReferences(ctx, db, &replicaStore{})
	if err != nil || empty.Payloads != 0 || empty.Attachments != 0 {
		t.Fatal(empty, err)
	}
	location, _ := proposedLocation(cfg)
	if err := BindEmptyPrimaryClosureTx(ctx, db, location); err == nil {
		t.Fatal("non-atomic empty binding admitted")
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return BindEmptyPrimaryClosureTx(ctx, tx, location) }); err != nil {
		t.Fatal(err)
	}
	var row PrimaryClosure
	if err := db.First(&row).Error; err != nil || row.ReferenceSHA256 != empty.SHA256 || row.Cells != empty.Cells || row.Payloads != 0 || row.Attachments != 0 {
		t.Fatal("seed JSON reference stamp was fabricated", row, empty, err)
	}
	if err := CheckPrimaryClosure(ctx, db, cfg); err != nil {
		t.Fatal(err)
	}
	if err := CanBootstrapPrimaryClosure(ctx, db); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal("existing primary could mint another empty-origin receipt", err)
	}
}

func TestEmptyPrimaryBootstrapRefusesHistoricalOrModifiedSealedInstallations(t *testing.T) {
	for _, state := range []string{"business", "quarantine", "modified-seed", "missing-seal", "invalid-seal", "existing-location", "restored-dataset"} {
		t.Run(state, func(t *testing.T) {
			db, cfg := closureFreshFixture(t)
			var err error
			switch state {
			case "business":
				err = db.Table("solo_messages").Create(map[string]any{"id": "retained-history", "content": "history"}).Error
			case "quarantine":
				err = db.Create(&models.ExecutionQuarantine{ID: "unknown-retained", DatasetID: "current"}).Error
			case "modified-seed":
				err = db.Model(&models.Workspace{}).Where("id = ?", models.WorkspaceID).Update("name", "edited workspace").Error
			case "missing-seal":
				err = db.Where("id = ?", "primary").Delete(&database.FreshOriginSeal{}).Error
			case "invalid-seal":
				err = db.Model(&database.FreshOriginSeal{}).Where("id = ?", "primary").Update("protocol", "guessed-current-schema").Error
			case "existing-location":
				old, _ := proposedLocation(config.StorageConfig{Driver: "local", BlobsRoot: cfg.BlobsRoot})
				err = db.Create(&old).Error
			case "restored-dataset":
				err = db.Create(&models.ExecutionDataset{ID: "primary", SourceDatasetID: uuid.NewString()}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := CanBootstrapPrimaryClosure(t.Context(), db); !errors.Is(err, ErrPrimaryClosure) {
				t.Fatal("empty-origin proof adopted retained data", state, err)
			}
			location, _ := proposedLocation(cfg)
			if err := db.Transaction(func(tx *gorm.DB) error { return BindEmptyPrimaryClosureTx(t.Context(), tx, location) }); err == nil {
				t.Fatal("final binding skipped transactional recheck", state)
			}
			var count int64
			if err := db.Model(&PrimaryClosure{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("invalid bootstrap published a receipt", err, count)
			}
		})
	}
}

func TestPrimaryClosureStartupUsesOnlyMatchingValidHistoricalSQLProof(t *testing.T) {
	db, cfg, owner, _ := closureFixture(t, "missing")
	location, _ := proposedLocation(cfg)
	if err := db.AutoMigrate(&PrimaryClosure{}, &Replica{}); err != nil {
		t.Fatal(err)
	}
	valid := PrimaryClosure{ID: owner, Protocol: PrimaryClosureProtocol, PhysicalFingerprint: location.Fingerprint,
		ReferenceSHA256: strings.Repeat("a", 64), Cells: 10, Payloads: 1, CompletedAt: time.Now().UTC()}
	for _, mutate := range []func(*PrimaryClosure){
		func(r *PrimaryClosure) { r.Protocol = "legacy" },
		func(r *PrimaryClosure) { r.PhysicalFingerprint = strings.Repeat("b", 64) },
		func(r *PrimaryClosure) { r.ReferenceSHA256 = "invalid" },
		func(r *PrimaryClosure) { r.Cells = -1 },
		func(r *PrimaryClosure) { r.CompletedAt = time.Time{} },
		func(r *PrimaryClosure) { before := r.CompletedAt.Add(-time.Second); r.FinalizedAt = &before },
	} {
		bad := valid
		mutate(&bad)
		if err := db.Create(&bad).Error; err != nil {
			t.Fatal(err)
		}
		if err := CheckPrimaryClosure(t.Context(), db, cfg); !errors.Is(err, ErrPrimaryClosure) {
			t.Fatal("malformed proof admitted remote startup", bad, err)
		}
		if err := db.Where("id = ?", bad.ID).Delete(&PrimaryClosure{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckPrimaryClosure(t.Context(), db, cfg); err != nil {
		t.Fatal("metadata startup check performed AM or GET", err)
	}
	if err := db.Exec("PRAGMA query_only = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", valid.ID).Delete(&PrimaryClosure{}).Error; err != nil {
		t.Fatal(err)
	}
	replica := Replica{ID: uuid.NewString(), SourceFingerprint: location.Fingerprint, DestinationFingerprint: strings.Repeat("c", 64),
		DatabaseSHA256: strings.Repeat("d", 64), ManifestSHA256: strings.Repeat("e", 64), CreatedAt: time.Now().UTC()}
	if err := db.Create(&replica).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckPrimaryClosure(t.Context(), db, cfg); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal("source-side replica was treated as a verified primary destination", err)
	}
	if err := db.Model(&Replica{}).Where("id = ?", replica.ID).Updates(map[string]any{"source_fingerprint": strings.Repeat("f", 64), "destination_fingerprint": location.Fingerprint}).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckPrimaryClosure(t.Context(), db, cfg); err != nil {
		t.Fatal("genuine destination proof required unnecessary object I/O", err)
	}
}

func TestLocalPrimaryStillRequiresItsExactPhysicalIdentity(t *testing.T) {
	db, cfg, _, _ := closureFixture(t, "valid")
	local, _ := proposedLocation(config.StorageConfig{Driver: "local", BlobsRoot: cfg.BlobsRoot})
	if err := db.Model(&Location{}).Where("name = ?", "primary").Updates(map[string]any{"driver": "local", "fingerprint": local.Fingerprint}).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckPrimaryClosure(t.Context(), db, config.StorageConfig{Driver: "local", BlobsRoot: cfg.BlobsRoot}); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrimaryClosure(t.Context(), db, config.StorageConfig{Driver: "local", BlobsRoot: t.TempDir()}); !errors.Is(err, ErrPrimaryClosure) {
		t.Fatal("local root silently changed", err)
	}
}
