package database

import (
	"github.com/cocofhu/grasp/internal/models"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceStartupPreservesCustomIDAndRejectsMultipleBeforeMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&models.Project{}).Where("id = ?", models.DefaultProjectID).Update("id", "custom-id").Error; err != nil {
		t.Fatal(err)
	}
	if err = closeGorm(db); err != nil {
		t.Fatal(err)
	}
	db, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	var p models.Project
	if err = db.First(&p).Error; err != nil || p.ID != "custom-id" {
		t.Fatalf("custom workspace lost: %+v %v", p, err)
	}
	if err = db.Create(&models.Project{ID: "second", Name: "Second"}).Error; err != nil {
		t.Fatal(err)
	}
	_ = closeGorm(db)
	if _, err = OpenSQLite(path); err == nil {
		t.Fatal("multiple projects silently accepted")
	}
}

func TestWorkspaceFilePreflightRejectsOrphansAndInvalidIdentity(t *testing.T) {
	profiles := filepath.Join(t.TempDir(), "profiles")
	shared := filepath.Join(filepath.Dir(profiles), "project-shared", "removed")
	if err := os.MkdirAll(shared, 0700); err != nil {
		t.Fatal(err)
	}
	if err := checkWorkspaceFiles(profiles, ""); err == nil {
		t.Fatal("shared-only installation treated as empty")
	}
	if err := checkWorkspaceFiles(profiles, "canonical"); err == nil {
		t.Fatal("foreign shared configuration adopted")
	}
	if err := os.Rename(shared, filepath.Join(filepath.Dir(shared), "canonical")); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(profiles, "agent")
	if err := os.MkdirAll(agent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "agent.json"), []byte(`{"projectId":"removed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkWorkspaceFiles(profiles, "canonical"); err == nil {
		t.Fatal("foreign Agent configuration adopted")
	}
	if err := os.WriteFile(filepath.Join(agent, "agent.json"), []byte(`{"projectId":"canonical"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkWorkspaceFiles(profiles, "canonical"); err != nil {
		t.Fatal(err)
	}
}
func TestWorkspaceStartupDoesNotSeedOverOrphanResources(t *testing.T) {
	db, err := OpenSQLiteTest(filepath.Join(t.TempDir(), "orphans.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Delete(&models.Project{}, "id = ?", models.DefaultProjectID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&models.Artifact{ID: "orphan", ProjectID: "gone"}).Error; err != nil {
		t.Fatal(err)
	}
	if err = ensureDefaultProject(db); err == nil {
		t.Fatal("orphan resources were treated as empty installation")
	}
	var n int64
	db.Model(&models.Project{}).Count(&n)
	if n != 0 {
		t.Fatal("seeded an invented workspace")
	}
}
