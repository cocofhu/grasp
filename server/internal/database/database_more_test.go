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
