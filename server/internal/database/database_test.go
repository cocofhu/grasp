package database

import (
	"path/filepath"
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
	if _, err := openMySQL("user:pass@tcp(127.0.0.1:1)/db?timeout=1s"); err == nil {
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
