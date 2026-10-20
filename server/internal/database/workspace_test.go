package database

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestOpenFreshSeedsWorkspaceAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grasp.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	var ws models.Workspace
	if err := db.First(&ws, "id = ?", models.WorkspaceID).Error; err != nil {
		t.Fatalf("workspace not seeded: %v", err)
	}
	_ = closeGorm(db)
	db, err = OpenSQLite(path)
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
