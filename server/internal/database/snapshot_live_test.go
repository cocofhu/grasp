package database

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	mysqlcfg "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestMySQLLiveOfflineSnapshotAndNativeRestore(t *testing.T) {
	dsn := os.Getenv("GRASP_MYSQL_STORAGE_TEST_DSN")
	if dsn == "" {
		t.Skip("set GRASP_MYSQL_STORAGE_TEST_DSN for an isolated native MySQL 8+ fixture")
	}
	connection, err := mysqlcfg.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(connection.DBName, "grasp_storage_test_") {
		t.Fatal("live fixture requires a dedicated grasp_storage_test_ database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "mysql", DSN: dsn}
	runtime, err := OpenRuntime(cfg, filepath.Join(dir, "payloads"))
	if err != nil {
		diagnostic, openErr := ConnectWithFiles(cfg, filepath.Join(dir, "payloads"))
		if openErr == nil {
			defer func() { _ = Close(diagnostic) }()
			for _, model := range models.AllModels() {
				if migrateErr := diagnostic.AutoMigrate(model); migrateErr != nil {
					t.Fatalf("MySQL schema %T: %v", model, migrateErr)
				}
			}
		}
		t.Fatal(err)
	}
	if err := runtime.DB.Create(&models.SoloMessage{ID: "mysql-backup-message", SessionID: "private-owner", Content: "MySQL snapshot includes original opaque payload references"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := ConnectWithFiles(cfg, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close(db) }()
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "database.sql")
	if err := Snapshot(ctx, db, cfg, owner, backup); err != nil {
		t.Fatal(err)
	}
	restoredName := "grasp_storage_test_restore_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE DATABASE " + restoredName + " CHARACTER SET utf8mb4").Error; err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Exec("DROP DATABASE " + restoredName).Error }()
	options, err := mysqlClientOptions(connection)
	if err != nil {
		t.Fatal(err)
	}
	defaults := filepath.Join(dir, "client.cnf")
	if err := os.WriteFile(defaults, []byte(options), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	command := exec.CommandContext(ctx, "mysql", "--defaults-file="+defaults, restoredName)
	command.Stdin = input
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := command.Run(); err != nil {
		t.Fatalf("native mysql restore failed: %v", err)
	}
	restoredConnection := *connection
	restoredConnection.DBName = restoredName
	restored, err := ConnectWithFiles(config.DatabaseConfig{Driver: "mysql", DSN: restoredConnection.FormatDSN()}, filepath.Join(dir, "restored-payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close(restored) }()
	var originalRef, restoredRef string
	if err := db.Table("solo_messages").Select("content").Where("id = ?", "mysql-backup-message").Row().Scan(&originalRef); err != nil {
		t.Fatal(err)
	}
	if err := restored.Table("solo_messages").Select("content").Where("id = ?", "mysql-backup-message").Row().Scan(&restoredRef); err != nil || originalRef != restoredRef {
		t.Fatalf("native restore changed payload reference: %v", err)
	}
	if err := storageguard.Check(ctx, restored, owner); err != nil {
		t.Fatal("restored SQL did not preserve maintenance fence")
	}
	if _, err := storageguard.Register(ctx, restored); err == nil {
		t.Fatal("restored database bypassed offline activation")
	}
	tables, err := restored.Migrator().GetTables()
	if err != nil || len(tables) < len(models.AllModels()) {
		t.Fatalf("native restore incomplete: %d tables %v", len(tables), err)
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if reportPath := os.Getenv("GRASP_MYSQL_STORAGE_TEST_REPORT"); reportPath != "" {
		report, _ := json.MarshalIndent(map[string]any{"status": "passed", "nativeDumpAndRestore": true,
			"tables": len(tables), "rawPayloadReferencePreserved": true, "restoredStartupFenced": true}, "", "  ")
		if err := os.WriteFile(reportPath, report, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
