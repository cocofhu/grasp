package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestOpenSQLiteTestBadParent(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLiteTest(filepath.Join(blocker, "nested.db")); err == nil {
		t.Fatal("expected mkdir failure when parent is a file")
	}
}

func TestOpenSQLiteBacksUpExistingDBAndKeepsThree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grasp.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "backup", "*.db")); len(matches) != 0 {
		t.Fatalf("fresh db must not be backed up, got %v", matches)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		backupSQLite(db, path, base.Add(time.Duration(i)*time.Second))
	}
	_ = closeGorm(db)

	matches, _ := filepath.Glob(filepath.Join(dir, "backup", "grasp-*.db"))
	if len(matches) != 3 {
		t.Fatalf("want 3 backups kept, got %v", matches)
	}
	if filepath.Base(matches[0]) != "grasp-20260101-000002.db" {
		t.Fatalf("oldest backups must be pruned first, got %v", matches)
	}
	restored, err := OpenSQLite(matches[2])
	if err != nil {
		t.Fatal(err)
	}
	var p models.Workspace
	if err := restored.First(&p, "id = ?", models.WorkspaceID).Error; err != nil {
		t.Fatalf("backup must contain data: %v", err)
	}
}

func TestEnsureWorkspaceSeedsSingletonIdempotently(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "default.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Workspace{}).Where("id = ?", models.WorkspaceID).
		Update("name", "Renamed").Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureWorkspace(db); err != nil {
		t.Fatal(err)
	}
	var ws []models.Workspace
	db.Find(&ws)
	if len(ws) != 1 || ws[0].ID != models.WorkspaceID || ws[0].Name != "Renamed" {
		t.Fatalf("ensureWorkspace must keep the existing singleton, got %+v", ws)
	}
}
