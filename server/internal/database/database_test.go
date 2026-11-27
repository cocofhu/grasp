package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
)

func TestOpenFileAndMigrate(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Every model table should be migrated.
	for _, m := range models.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Errorf("table missing for %T", m)
		}
	}
}

func TestConfiguredPayloadRootSurvivesDatabaseReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "grasp.db")}
	root := filepath.Join(dir, "persistent", "payloads")
	db, err := OpenWithFiles(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&models.Artifact{ID: "restart", Kind: "markdown", Content: "persistent report"}).Error; err != nil {
		t.Fatal(err)
	}
	_ = Close(db)
	db, err = OpenWithFiles(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(db) })
	var artifact models.Artifact
	if err = db.First(&artifact, "id = ?", "restart").Error; err != nil || artifact.Content != "persistent report" {
		t.Fatalf("reopen=%+v err=%v", artifact, err)
	}
}

func TestOpenSQLiteTestClonesMigratedSchema(t *testing.T) {
	db, err := OpenSQLiteTest(filepath.Join(t.TempDir(), "clone.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	for _, m := range models.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Errorf("table missing for %T", m)
		}
	}
	if err := db.Create(&models.Run{ID: "r-clone", WorkflowID: "w", Status: "running"}).Error; err != nil {
		t.Fatalf("insert into cloned schema: %v", err)
	}
}

func TestMigrateLegacyPreviewStorageKeepsNewestPerNodeAndSandbox(t *testing.T) {
	db, err := openSQLiteConn(":memory:", "")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`
CREATE TABLE run_preview_ports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  item_key TEXT NOT NULL,
  sandbox_name TEXT,
  registered_at DATETIME NOT NULL
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_preview_run_node_item ON run_preview_ports(run_id, node_id, item_key)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO run_preview_ports(run_id, node_id, item_key, sandbox_name, registered_at) VALUES
  ('r1', 'n1', 'p:3000', 'sb-1', '2026-11-22 10:00:00'),
  ('r1', 'n1', 'p:4000', 'sb-1', '2026-11-22 11:00:00'),
  ('r1', 'n2', 'p:5000', 'sb-1', '2026-11-22 12:00:00'),
  ('r2', 'n1', 'u:https://example.test', NULL, '2026-11-22 09:00:00'),
  ('r2', 'n2', 'p:3000', 'sb-2', '2026-11-22 08:00:00')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyPreviewStorage(db); err != nil {
		t.Fatal(err)
	}
	var ids []uint
	if err := db.Table("run_preview_ports").Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != 3 || ids[1] != 4 || ids[2] != 5 {
		t.Fatalf("legacy rows after collapse=%v, want newest per node and sandbox [3 4 5]", ids)
	}
	if db.Migrator().HasIndex(&models.RunPreviewPort{}, "idx_preview_run_node_item") {
		t.Fatal("legacy multi-preview index still exists")
	}
	if err := db.AutoMigrate(&models.RunPreviewPort{}); err != nil {
		t.Fatalf("active preview schema migration: %v", err)
	}
	if !db.Migrator().HasIndex(&models.RunPreviewPort{}, "idx_preview_run_node") {
		t.Fatal("active preview unique index missing")
	}
}

func TestOpenMemory(t *testing.T) {
	db, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	if !db.Migrator().HasTable(&models.WorkflowDef{}) {
		t.Fatal("workflow table missing in memory db")
	}
}

func TestOpenWithExistingQueryAndBadPath(t *testing.T) {
	// A dsn that already carries query params skips the default-append branch.
	db, err := OpenSQLite(":memory:?cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open with query: %v", err)
	}
	if !db.Migrator().HasTable(&models.WorkflowDef{}) {
		t.Fatal("migrate failed for dsn with query")
	}
	// An unwritable path surfaces an open error rather than panicking.
	if _, err := OpenSQLite("/nonexistent-root-dir-xyz/nested/db.sqlite"); err == nil {
		t.Error("expected open error for unwritable path")
	}
}

func TestOpenMySQLEmptyDSN(t *testing.T) {
	if _, err := Open(config.DatabaseConfig{Driver: "mysql", DSN: ""}); err == nil {
		t.Fatal("expected error for empty mysql DSN")
	}
	if _, err := Open(config.DatabaseConfig{Driver: "mysql", DSN: "   "}); err == nil {
		t.Fatal("expected error for whitespace mysql DSN")
	}
	if _, err := Open(config.DatabaseConfig{Driver: "mysql", DSN: "user:pass@tcp(127.0.0.1:1)/db?timeout=1s"}); err == nil {
		t.Fatal("unreachable mysql should fail open")
	}
}

func TestOpenSQLiteDriver(t *testing.T) {
	db, err := Open(config.DatabaseConfig{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite via Open: %v", err)
	}
	if !db.Migrator().HasTable(&models.WorkflowDef{}) {
		t.Fatal("workflow table missing")
	}
}

func TestOpenPostgresRejectsInvalidDSN(t *testing.T) {
	for _, dsn := range []string{" ", "postgresql://user:test-password@db:invalid/grasp"} {
		db, err := Open(config.DatabaseConfig{Driver: "postgres", DSN: dsn})
		if db != nil || err == nil || !strings.Contains(err.Error(), "database.dsn") || strings.Contains(err.Error(), "test-password") {
			t.Fatalf("invalid PostgreSQL connection: db=%v err=%v", db, err)
		}
	}
}

func TestOpenRejectsUnknownDriver(t *testing.T) {
	db, err := Open(config.DatabaseConfig{Driver: "invalid", Path: ":memory:"})
	if db != nil || err == nil || !strings.Contains(err.Error(), "unsupported database.driver") {
		t.Fatalf("invalid driver: db=%v err=%v", db, err)
	}
}
