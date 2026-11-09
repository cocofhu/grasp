package database

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func freshOriginDB(t *testing.T) *gorm.DB {
	t.Helper()
	root := t.TempDir()
	db, err := ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "sql.db")}, filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(db) })
	return db
}

func TestFreshOriginRequiresOriginalEmptyTablesAndPrivateSamePoolProof(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"", "runs", "unknown_empty_table"} {
		t.Run(table, func(t *testing.T) {
			db := freshOriginDB(t)
			if err := storageguard.Enter(ctx, db, uuid.NewString(), nil); err != nil {
				t.Fatal(err)
			}
			if table != "" {
				if err := db.Exec("CREATE TABLE " + table + " (id integer PRIMARY KEY)").Error; err != nil {
					t.Fatal(err)
				}
			}
			proof, err := CaptureFreshOrigin(ctx, db)
			if err != nil || proof.Empty() != (table == "") {
				t.Fatal("application tables inferred as fresh", proof.Empty(), err)
			}
			if table != "" {
				if err := SealFreshOrigin(ctx, db, proof, "runtime"); err == nil {
					t.Fatal("unproven origin sealed")
				}
				if db.Migrator().HasTable(&FreshOriginSeal{}) {
					t.Fatal("rejected proof changed schema")
				}
				return
			}
			other := freshOriginDB(t)
			if err := SealFreshOrigin(ctx, other, proof, "runtime"); err == nil {
				t.Fatal("proof used on another database")
			}
			if err := SealFreshOrigin(ctx, db, FreshOriginProof{}, "runtime"); err == nil {
				t.Fatal("zero proof accepted")
			}
			if err := db.Transaction(func(tx *gorm.DB) error {
				if _, err := CaptureFreshOrigin(ctx, tx); err == nil {
					t.Fatal("transaction produced fresh proof")
				}
				if err := SealFreshOrigin(ctx, tx, proof, "runtime"); err == nil {
					t.Fatal("origin DDL ran inside transaction")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := SealFreshOrigin(ctx, db, proof, "unknown"); err == nil {
				t.Fatal("unknown initializer accepted")
			}
			if err := db.AutoMigrate(&models.Run{}, &models.StateRun{}); err != nil {
				t.Fatal(err)
			}
			if err := SealFreshOrigin(ctx, db, proof, "execution-init"); err != nil {
				t.Fatal(err)
			}
			var seal FreshOriginSeal
			if err := db.First(&seal, "id = ?", "primary").Error; err != nil || validateFreshSeal(seal) != nil || seal.Initializer != "execution-init" {
				t.Fatal(seal, err)
			}
			if db.Migrator().HasTable(&OfflineUpgradeReceipt{}) {
				t.Fatal("fresh seal fabricated an old upgrade receipt")
			}
		})
	}
}

func TestFreshOriginDoesNotInferPastAfterCrashOrIgnoreNewData(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"proof_lost", "business_inserted"} {
		t.Run(mode, func(t *testing.T) {
			db := freshOriginDB(t)
			proof, err := CaptureFreshOrigin(ctx, db)
			if err != nil || !proof.Empty() {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&models.Run{}, &models.StateRun{}); err != nil {
				t.Fatal(err)
			}
			if mode == "proof_lost" {
				proof, err = CaptureFreshOrigin(ctx, db)
				if err != nil || proof.Empty() {
					t.Fatal("post-crash current schema inferred empty past", err)
				}
			} else {
				if err := db.Create(&models.Run{ID: "arrived-during-initialization", Status: "completed"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := SealFreshOrigin(ctx, db, proof, "runtime"); err == nil {
				t.Fatal("incomplete origin silently repaired")
			}
			if db.Migrator().HasTable(&FreshOriginSeal{}) {
				t.Fatal("failed seal changed metadata schema")
			}
		})
	}
}

func TestFreshRuntimeSealSurvivesLegitimateNewPayloadGrowth(t *testing.T) {
	root := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "fresh.db")}
	runtime, err := OpenRuntime(cfg, filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	var before FreshOriginSeal
	if err := runtime.DB.First(&before, "id = ?", "primary").Error; err != nil || before.Initializer != "runtime" || validateFreshSeal(before) != nil {
		t.Fatal(before, err)
	}
	if err := runtime.DB.Create(&models.SoloSession{ID: "new-solo", UserID: "alice", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.Create(&models.SoloMessage{ID: "new-human", SessionID: "new-solo", Role: "user", Content: "legitimate new conversation"}).Error; err != nil {
		t.Fatal(err)
	}
	var ref string
	if err := runtime.DB.Table("solo_messages").Where("id = ?", "new-human").Pluck("content", &ref).Error; err != nil || !filestore.IsRef(ref) {
		t.Fatal("new payload is not canonical", ref, err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRuntime(cfg, filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	var after FreshOriginSeal
	if err := reopened.DB.First(&after, "id = ?", "primary").Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("normal new writes changed original origin", before, after, err)
	}
	proof, err := CaptureFreshOrigin(context.Background(), reopened.DB)
	if err != nil || proof.Empty() {
		t.Fatal("reopen reclassified populated SQL as empty", err)
	}
}

func TestRuntimeNeverFreshSealsAnExistingApplicationSchema(t *testing.T) {
	root := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "existing.db")}
	db, err := ConnectWithFiles(cfg, filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE runs (id text PRIMARY KEY, status text)").Error; err != nil {
		t.Fatal(err)
	}
	if err := Close(db); err != nil {
		t.Fatal(err)
	}
	runtime, err := OpenRuntime(cfg, filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if runtime.DB.Migrator().HasTable(&FreshOriginSeal{}) {
		t.Fatal("AutoMigrate made old schema look like a fresh install")
	}
}
