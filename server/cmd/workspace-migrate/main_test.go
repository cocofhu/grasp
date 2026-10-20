package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/workspacemigration"
)

func commandFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "source.db")
	db, err := database.OpenSQLiteTest(path)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	profiles := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	return path, profiles
}

func TestCommandPlanApplyVerifyAndRollback(t *testing.T) {
	databasePath, profiles := commandFixture(t)
	plan := filepath.Join(t.TempDir(), "plan.json")
	backup := filepath.Join(t.TempDir(), "backup")
	var output bytes.Buffer
	if err := run([]string{"plan", "--database", databasePath, "--profiles", profiles, "--plan", plan}, &output, &output); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(plan)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("plan permissions are not private")
	}
	if !strings.Contains(output.String(), "No business data was modified") {
		t.Fatal("missing preflight result")
	}
	for _, phase := range []string{"apply", "verify", "rollback"} {
		args := []string{phase, "--database", databasePath, "--plan", plan, "--backup", backup}
		if phase != "verify" {
			args = append(args, "--offline")
		}
		if err := run(args, &output, &output); err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
	}
}

func TestReadOnlyOpenDoesNotSeedOrAllowWrites(t *testing.T) {
	path, _ := commandFixture(t)
	db, err := openDatabase("sqlite", path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if err := db.Create(&models.Project{ID: "write", Name: "forbidden"}).Error; err == nil {
		t.Fatal("read-only preflight allowed a write")
	}
	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, err := openDatabase("sqlite", missing, "", true); err == nil {
		t.Fatal("missing database was created")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preflight created a database")
	}
}

func TestCLIRejectsIncompleteWriteAndInvalidInput(t *testing.T) {
	path, profiles := commandFixture(t)
	planFile := filepath.Join(t.TempDir(), "plan.json")
	var output bytes.Buffer
	if err := run([]string{"plan", "--database", path, "--profiles", profiles, "--plan", planFile}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{}, {"unknown"}, {"plan"}, {"plan", "--plan", planFile, "extra"},
		{"apply", "--plan", planFile, "--database", path},
		{"verify", "--plan", planFile, "--database", path},
		{"rollback", "--plan", planFile, "--database", path},
		{"plan", "--plan", planFile, "--driver", "invalid"},
	} {
		if err := run(args, &output, &output); err == nil {
			t.Fatalf("unsafe arguments accepted: %v", args)
		}
	}
	if err := run([]string{"plan", "--database", path, "--profiles", profiles, "--plan", planFile}, &output, &output); err == nil {
		t.Fatal("an existing accepted plan was overwritten")
	}
	t.Setenv("GRASP_MIGRATION_TEST_EMPTY_DSN", "")
	if _, err := openDatabase("mysql", "", "GRASP_MIGRATION_TEST_EMPTY_DSN", true); err == nil {
		t.Fatal("missing MySQL DSN accepted")
	}
}

func TestCLIBlockedPlanIsStillWrittenForReview(t *testing.T) {
	path, _ := commandFixture(t)
	plan := filepath.Join(t.TempDir(), "blocked.json")
	var output bytes.Buffer
	err := run([]string{"plan", "--database", path, "--plan", plan}, &output, &output)
	if !errors.Is(err, workspacemigration.ErrBlocked) {
		t.Fatal("unreviewed files allowed", err)
	}
	if _, err := os.Stat(plan); err != nil {
		t.Fatal("blocked preflight report was not written")
	}
}
