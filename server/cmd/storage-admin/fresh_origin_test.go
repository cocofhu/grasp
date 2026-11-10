package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

func TestInitializeEmptyCoreSealsOnlyObservedFreshOrigin(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "existing_schema"}[existing], func(t *testing.T) {
			root := t.TempDir()
			db, err := database.ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "db.sqlite")}, filepath.Join(root, "files"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close(db) })
			if existing {
				if err := db.AutoMigrate(&models.Run{}, &models.StateRun{}); err != nil {
					t.Fatal(err)
				}
			}
			if err := storageguard.Enter(context.Background(), db, uuid.NewString(), nil); err != nil {
				t.Fatal(err)
			}
			if err := initializeEmptyCore(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			if db.Migrator().HasTable(&database.FreshOriginSeal{}) == existing {
				t.Fatal("fresh proof inferred from an existing core schema")
			}
			if !existing {
				var seal database.FreshOriginSeal
				if err := db.First(&seal, "id = ?", "primary").Error; err != nil || seal.Protocol != database.FreshOriginProtocol || seal.Initializer != "execution-init" {
					t.Fatal(seal, err)
				}
				if err := initializeEmptyCore(context.Background(), db); err != nil {
					t.Fatal("exact retry failed", err)
				}
				if db.Migrator().HasTable(&database.OfflineUpgradeReceipt{}) {
					t.Fatal("fresh install invented a prior SQL upgrade")
				}
			}
		})
	}
}
