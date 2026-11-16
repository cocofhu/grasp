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
