package main

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestStorageFoundationCannotExecuteAnInitializedOrRestoredDataset(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "foundation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	if err := requireUninitializedFoundation(db); err != nil {
		t.Fatal("empty origin should use ordinary workers", err)
	}
	if err := db.Create(&models.ExecutionDataset{ID: "primary", DatasetID: "restored", Lineage: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := requireUninitializedFoundation(db); err == nil {
		t.Fatal("initialized history escaped external execution guards")
	}
	if err := db.Migrator().DropTable(&models.ExecutionDataset{}); err != nil {
		t.Fatal(err)
	}
	if err := requireUninitializedFoundation(db); err == nil {
		t.Fatal("missing binding schema was treated as empty")
	}
}

func TestStorageFoundationRejectsConvertedHistoryBeforeExecutionInitialization(t *testing.T) {
	for _, kind := range []string{"upgrade_receipt", "imported_pointer", "ordinary_payload"} {
		t.Run(kind, func(t *testing.T) {
			db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close(db) }()
			switch kind {
			case "upgrade_receipt":
				if err := db.AutoMigrate(&database.OfflineUpgradeReceipt{}); err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&database.OfflineUpgradeReceipt{ID: "original-upgrade"}).Error; err != nil {
					t.Fatal(err)
				}
			case "imported_pointer":
				if err := db.Table("solo_messages").Create(map[string]any{"id": "partially-imported", "event_log": `{"version":1,"from":1,"through":1,"count":1,"legacy":{"format":"solo-events-array/v1"}}`}).Error; err != nil {
					t.Fatal(err)
				}
			case "ordinary_payload":
				if err := db.Create(&models.SoloMessage{ID: "ordinary", SessionID: "fresh", Content: "new canonical payload"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			var datasets int64
			if err := db.Model(&models.ExecutionDataset{}).Count(&datasets).Error; err != nil || datasets != 0 {
				t.Fatal("fixture unexpectedly initialized execution", datasets, err)
			}
			err = requireUninitializedFoundation(db)
			if kind == "ordinary_payload" && err != nil {
				t.Fatal("normal foundation writes must remain readable after restart", err)
			}
			if kind != "ordinary_payload" && err == nil {
				t.Fatal("converted history escaped before execution initialization")
			}
		})
	}
}
