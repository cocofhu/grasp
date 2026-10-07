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
	var p models.Project
	if err := restored.First(&p, "id = ?", models.DefaultProjectID).Error; err != nil {
		t.Fatalf("backup must contain data: %v", err)
	}
}

func TestEnsureDefaultProjectOnlyOnEmptyDB(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "default.db"))
	if err != nil {
		t.Fatal(err)
	}
	var p models.Project
	if err := db.First(&p, "id = ?", models.DefaultProjectID).Error; err != nil {
		t.Fatalf("default project missing on fresh db: %v", err)
	}
	db.Exec("DELETE FROM projects")
	now := time.Now()
	if err := db.Create(&models.Project{
		ID: "custom", Name: "Custom", Variables: []models.ProjectVariable{},
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	ensureDefaultProject(db)
	var n int64
	db.Model(&models.Project{}).Count(&n)
	if n != 1 {
		t.Fatalf("ensureDefaultProject must not create when projects exist, got %d", n)
	}
}
