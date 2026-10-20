package database

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestOpenRejectsLegacyProjectsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := openSQLiteConn(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Exec("CREATE TABLE projects (id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	_ = closeGorm(raw)
	if _, err := OpenSQLite(path); !errors.Is(err, ErrLegacySchema) {
		t.Fatalf("want ErrLegacySchema, got %v", err)
	}
}

func TestOpenRejectsLegacyProjectIDColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-col.db")
	raw, err := openSQLiteConn(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Exec("CREATE TABLE workflow_defs (id TEXT PRIMARY KEY, project_id TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	_ = closeGorm(raw)
	if _, err := OpenSQLite(path); !errors.Is(err, ErrLegacySchema) {
		t.Fatalf("want ErrLegacySchema, got %v", err)
	}
}

func TestOpenRejectsLegacySharedDirectory(t *testing.T) {
	data := t.TempDir()
	profiles := filepath.Join(data, "profiles")
	if err := os.MkdirAll(filepath.Join(data, "project-shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLite(filepath.Join(data, "grasp.db"), profiles); !errors.Is(err, ErrLegacySchema) {
		t.Fatalf("want ErrLegacySchema, got %v", err)
	}
}

func TestOpenFreshSeedsWorkspaceAndReopens(t *testing.T) {
	data := t.TempDir()
	path := filepath.Join(data, "grasp.db")
	profiles := filepath.Join(data, "profiles")
	db, err := OpenSQLite(path, profiles)
	if err != nil {
		t.Fatal(err)
	}
	var ws models.Workspace
	if err := db.First(&ws, "id = ?", models.WorkspaceID).Error; err != nil {
		t.Fatalf("workspace not seeded: %v", err)
	}
	_ = closeGorm(db)
	db, err = OpenSQLite(path, profiles)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	var n int64
	db.Model(&models.Workspace{}).Count(&n)
	if n != 1 {
		t.Fatalf("want 1 workspace, got %d", n)
	}
}

func TestPrimaryChannelIndexIsGlobal(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO channel_configs (id, name, type, is_primary) VALUES ('a', 'a', 'qq', 1)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO channel_configs (id, name, type, is_primary) VALUES ('b', 'b', 'qq', 1)").Error; err == nil {
		t.Fatal("second primary channel must violate the unique index")
	}
	if err := db.Exec("INSERT INTO channel_configs (id, name, type, is_primary) VALUES ('c', 'c', 'qq', 0)").Error; err != nil {
		t.Fatalf("secondary channel: %v", err)
	}
}
