package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
)

func upgradeTestDB(t *testing.T) (*config.Config, options) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "original.sqlite")},
		Storage: config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "blobs")}}
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("solo_sessions").Create(map[string]any{"id": "legacy", "user_id": "owner", "status": "closed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("solo_messages").Create(map[string]any{"id": "message", "session_id": "legacy", "role": "assistant", "content": "original history", "events": `[{"type":"text","text":"old process"}]`, "created_at": time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	return cfg, options{mode: "upgrade", offline: true, workdir: filepath.Join(dir, "upgrade")}
}

func TestOfflineUpgradeRetainsMaintenanceAndOriginalBackupAcrossResume(t *testing.T) {
	ctx := context.Background()
	cfg, opts := upgradeTestDB(t)
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	var first bytes.Buffer
	if err := upgradeDatabase(ctx, opts, cfg, db, &first); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(opts.workdir, "before.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt database.OfflineUpgradeReceipt
	if err := db.First(&receipt).Error; err != nil || receipt.LegacyEvents != 0 || receipt.HistoryMessages != 1 || receipt.HistoryEvents != 1 || receipt.Protocol != offlineUpgradeProtocol {
		t.Fatalf("receipt lost legacy history boundary: %+v %v", receipt, err)
	}
	if err := storageguard.Check(ctx, db, receipt.ID); err != nil {
		t.Fatal("completion released maintenance", err)
	}
	if _, err := storageguard.Register(ctx, db); err == nil {
		t.Fatal("server entered during offline upgrade")
	}
	if err := upgradeDatabase(ctx, opts, cfg, db, io.Discard); err == nil {
		t.Fatal("existing workdir was silently adopted without resume")
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	db, err = database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	opts.resume = true
	if err := upgradeDatabase(ctx, opts, cfg, db, io.Discard); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(opts.workdir, "before.sqlite"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retry overwrote original SQL backup", err)
	}
	var saved database.OfflineUpgradeReceipt
	if err := db.First(&saved).Error; err != nil || saved != receipt {
		t.Fatalf("retry replaced original receipt: %+v %v", saved, err)
	}
	if err := os.WriteFile(filepath.Join(opts.workdir, "before.sqlite"), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := upgradeDatabase(ctx, opts, cfg, db, io.Discard); err == nil {
		t.Fatal("damaged original backup was accepted")
	}
	if err := storageguard.Check(ctx, db, receipt.ID); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineUpgradeInvalidHistoryKeepsSourceAndFenceWithoutReceipt(t *testing.T) {
	ctx := context.Background()
	cfg, opts := upgradeTestDB(t)
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	if err := db.Table("solo_messages").Where("id = ?", "message").UpdateColumn("events", "{broken").Error; err != nil {
		t.Fatal(err)
	}
	if err := upgradeDatabase(ctx, opts, cfg, db, io.Discard); err == nil {
		t.Fatal("invalid JSON produced a completion receipt")
	}
	var raw string
	if err := db.Table("solo_messages").Select("events").Where("id = ?", "message").Row().Scan(&raw); err != nil || raw != "{broken" {
		t.Fatal("invalid source was discarded", err)
	}
	if db.Migrator().HasTable(&database.OfflineUpgradeReceipt{}) {
		var count int64
		if err := db.Model(&database.OfflineUpgradeReceipt{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("invalid history was marked complete", err)
		}
	}
	status, err := storageguard.Inspect(ctx, db)
	if err != nil || status.MaintenanceID == "" || len(status.Runtimes) != 0 {
		t.Fatal("failure lost offline barrier", err)
	}
	if _, err := os.Stat(filepath.Join(opts.workdir, "before.sqlite")); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineUpgradeLegacyBlobIntentsKeepIdentityAndNeverDispatch(t *testing.T) {
	ctx := context.Background()
	cfg, opts := upgradeTestDB(t)
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	payload := []byte{'{', '"', 'x', '"', ':', '1', '}', 0, 255}
	if err := db.Table("chat_pending_turns").Create(map[string]any{"id": "old-dispatch", "surface": "solo", "session_key": "legacy", "started": true, "position": 7, "payload": payload}).Error; err != nil {
		t.Fatal(err)
	}
	if err := upgradeDatabase(ctx, opts, cfg, db, io.Discard); err != nil {
		t.Fatal(err)
	}
	var old models.ChatPendingTurn
	if err := db.First(&old, "id = ?", "old-dispatch").Error; err != nil || !bytes.Equal(old.Payload, payload) || !old.Started || old.Position != 7 {
		t.Fatalf("legacy intent changed: %+v %v", old, err)
	}
	var queued int64
	if err := db.Table("chat_queue_turns").Count(&queued).Error; err != nil || queued != 0 {
		t.Fatal("offline normalization imported executable work", err)
	}
	var raw string
	if err := db.Table("chat_pending_turns").Select("payload").Where("id = ?", old.ID).Row().Scan(&raw); err != nil || !filestore.IsRef(raw) {
		t.Fatal("BLOB was not canonicalized", err)
	}
}

func TestOfflineUpgradeOptionsRequireExplicitOfflineAndNoExecutionSwitch(t *testing.T) {
	for _, opts := range []options{
		{mode: "upgrade"}, {mode: "upgrade", workdir: "x"},
		{mode: "upgrade", workdir: "x", offline: true, activate: true},
		{mode: "upgrade", workdir: "x", offline: true, executionControl: "control"},
		{mode: "upgrade", workdir: "x", offline: true, target: "other"},
	} {
		if err := validateOptions(opts); err == nil {
			t.Fatal("unsafe upgrade options accepted", opts.mode)
		}
	}
	if err := validateOptions(options{mode: "upgrade", workdir: "x", offline: true}); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineUpgradeMissingAttachmentCannotBecomeComplete(t *testing.T) {
	ctx := context.Background()
	cfg, opts := upgradeTestDB(t)
	db, err := database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	if err := db.Table("solo_messages").Where("id = ?", "message").UpdateColumn("images", `[{"ref":"blob:missing-owned-source","mimeType":"text/plain"}]`).Error; err != nil {
		t.Fatal(err)
	}
	if err := upgradeDatabase(ctx, opts, cfg, db, io.Discard); err == nil {
		t.Fatal("dangling attachment root was accepted")
	}
	if db.Migrator().HasTable(&database.OfflineUpgradeReceipt{}) {
		var count int64
		if err := db.Model(&database.OfflineUpgradeReceipt{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed closure was marked complete", err)
		}
	}
	status, err := storageguard.Inspect(ctx, db)
	if err != nil || status.MaintenanceID == "" {
		t.Fatal("missing attachment cleared maintenance", err)
	}
}
