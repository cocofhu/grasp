package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
)

func completedUpgradeForInit(t *testing.T) (*config.Config, options, string) {
	t.Helper()
	cfg, opts := upgradeTestDB(t)
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("runs").Create(map[string]any{"id": "unknown-run", "status": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("chat_pending_turns").Create(map[string]any{"id": "unknown-dispatch", "surface": "solo", "session_key": "legacy", "started": true, "payload": []byte(`{"text":"must never replay"}`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := upgradeDatabase(context.Background(), opts, cfg, db, io.Discard); err != nil {
		t.Fatal(err)
	}
	var receipt database.OfflineUpgradeReceipt
	if err := db.First(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	opts.mode = "execution-init"
	return cfg, opts, receipt.ID
}

func TestOfflineUpgradeThenExplicitInitializationUsesOriginalOwnerAndQuarantinesPending(t *testing.T) {
	ctx := context.Background()
	cfg, opts, owner := completedUpgradeForInit(t)
	if err := initializeExecution(ctx, opts, cfg, io.Discard); err != nil {
		t.Fatal("completed upgrade has no supported explicit next step", err)
	}
	if err := initializeExecution(ctx, opts, cfg, io.Discard); err != nil {
		t.Fatal("exact completed initialization did not reconcile", err)
	}
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	var binding models.ExecutionDataset
	if err := db.First(&binding).Error; err != nil || binding.OperationID != owner || binding.QuarantineCount != 2 {
		t.Fatal("original upgrade UUID or pending quarantine lost", binding, err)
	}
	status, err := storageguard.Inspect(ctx, db)
	if err != nil || status.MaintenanceID != "" {
		t.Fatal("explicit initialized operation did not finish its original maintenance", err)
	}
	var count int64
	if err := db.Table("chat_queue_turns").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("initialization replayed a legacy request", err)
	}
	var pending models.ChatPendingTurn
	if err := db.First(&pending, "id = ?", "unknown-dispatch").Error; err != nil || !pending.Started || string(pending.Payload) != `{"text":"must never replay"}` {
		t.Fatal("unknown source changed", err)
	}
	authority, err := executionlineage.OpenSQL(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := authority.ReadActive(ctx)
	if err != nil || authority.CheckBinding(ctx, db, permit) != nil {
		t.Fatal("explicit initialization did not bind the real authority", err)
	}
}

func TestOfflineUpgradeInitializationRejectsUnverifiedHistoryAndMismatchedCheckpoint(t *testing.T) {
	for _, problem := range []string{"incomplete", "source-changed", "foreign-initialization"} {
		t.Run(problem, func(t *testing.T) {
			cfg, opts, owner := completedUpgradeForInit(t)
			switch problem {
			case "incomplete":
				if err := os.Remove(filepath.Join(opts.workdir, "upgrade-complete.json")); err != nil {
					t.Fatal(err)
				}
			case "source-changed":
				db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Table("solo_messages").Where("id = ?", "message").UpdateColumn("event_log", nil).Error; err != nil {
					t.Fatal(err)
				}
				_ = database.Close(db)
			case "foreign-initialization":
				if err := writeJSON(filepath.Join(opts.workdir, "execution-initialization.json"), initialization{Version: 1, ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := initializeExecution(context.Background(), opts, cfg, io.Discard); err == nil {
				t.Fatal("unverified upgrade was used to initialize execution")
			}
			db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close(db) }()
			var count int64
			if err := db.Model(&models.ExecutionDataset{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("rejected upgrade already bound execution", count, err)
			}
			if err := storageguard.Check(context.Background(), db, owner); err != nil {
				t.Fatal("failed explicit initialization lost original maintenance", err)
			}
			if problem == "source-changed" {
				var pointer *string
				if err := db.Table("solo_messages").Select("event_log").Where("id = ?", "message").Row().Scan(&pointer); err != nil || pointer != nil {
					t.Fatal("initialization silently repaired unverified history", err)
				}
			}
		})
	}
}
