package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	mysqlcfg "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestRuntimeCloseFencesSQLBeforeUnregistering(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "live.db")}
	runtime, err := OpenRuntime(cfg, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.Create(&models.Setting{Key: "late", Value: "true"}).Error; err == nil {
		t.Fatal("late worker can still publish")
	}
	control, err := ConnectWithFiles(cfg, filepath.Join(dir, "control"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close(control) }()
	status, err := storageguard.Inspect(context.Background(), control)
	if err != nil || len(status.Runtimes) != 0 {
		t.Fatalf("clean close retained presence: %+v %v", status, err)
	}
}

func TestRuntimeMemoryCloseDoesNotReconnectToAPhantomDatabase(t *testing.T) {
	for _, path := range []string{":memory:", "file:grasp-runtime-close?mode=memory&cache=shared"} {
		t.Run(path, func(t *testing.T) {
			runtime, err := OpenRuntime(config.DatabaseConfig{Driver: "sqlite", Path: path}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatalf("ephemeral database shutdown must not query a newly created empty database: %v", err)
			}
		})
	}
}

func TestSQLiteSnapshotIncludesWALAndRequiresMaintenance(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "live.db")}
	db, err := OpenWithFiles(cfg, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close(db) }()
	owner := uuid.NewString()
	backup := filepath.Join(dir, "snapshot.db")
	if err := Snapshot(ctx, db, cfg, owner, backup); err == nil {
		t.Fatal("snapshot admitted without offline fence")
	}
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Setting{Key: "wal-value", Value: "retained"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Snapshot(ctx, db, cfg, owner, backup); err != nil {
		t.Fatal(err)
	}
	if err := Snapshot(ctx, db, cfg, owner, backup); err == nil {
		t.Fatal("immutable SQL backup was overwritten")
	}
	stat, _ := os.Stat(backup)
	if stat.Mode().Perm() != 0o600 {
		t.Fatal("SQL snapshot is not private")
	}
	restored, err := ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: backup}, filepath.Join(dir, "restored-payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close(restored) }()
	var row models.Setting
	if err := restored.First(&row, "key = ?", "wal-value").Error; err != nil || row.Value != "retained" {
		t.Fatalf("WAL content lost: %+v %v", row, err)
	}
}

func TestMySQLBackupOptionsDoNotInterpolateCredentials(t *testing.T) {
	cfg := mysqlcfg.NewConfig()
	cfg.Net, cfg.Addr, cfg.DBName = "tcp", "[::1]:3306", "backup"
	cfg.User, cfg.Passwd, cfg.TLSConfig = "user", "password\"\n$(private)\\", "true"
	options, err := mysqlClientOptions(cfg)
	if err != nil || !strings.Contains(options, `password="password\"\n$(private)\\"`) || !strings.Contains(options, "ssl-mode=VERIFY_IDENTITY") {
		t.Fatalf("unsafe client options or TLS downgrade: %v", err)
	}
	cfg.TLSConfig = "private-profile"
	if _, err := mysqlClientOptions(cfg); err == nil {
		t.Fatal("custom TLS silently downgraded")
	}
	cfg.TLSConfig, cfg.DBName = "false", "--execute=bad"
	if _, err := mysqlClientOptions(cfg); err == nil {
		t.Fatal("option-shaped database name accepted")
	}
}
