package database

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestStripProjectRuntimeConfigIsIdempotent(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := stripProjectRuntimeConfig(db); err != nil {
		t.Fatal(err)
	}
	for _, column := range projectRuntimeColumns {
		present, err := columnPresent(db, column)
		if err != nil || present {
			t.Fatalf("fresh schema still has %s: present=%v err=%v", column, present, err)
		}
	}

	for _, column := range projectRuntimeColumns {
		if err := db.Exec("ALTER TABLE projects ADD COLUMN " + column + " text").Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := db.Exec(`INSERT INTO projects
		(id, name, description, repo_url, default_branch, repo_dir_name, created_by, created_at, updated_at,
		 git_credential_type, git_credential_id, ssh_hosts_credential_id, env, credential_bindings)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"p1", "app", "demo", "https://github.com/acme/app.git", "main", "app", "alice", now, now,
		"github_https", "cred-1", "hosts-1", `{"APP_ENV":"dev"}`, `{"NPM_TOKEN":"c2"}`,
	).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := `{
		"id":"p1","name":"app","description":"demo","repoUrl":"https://github.com/acme/app.git",
		"defaultBranch":"main","repoDirName":"app","createdBy":"alice",
		"gitCredentialType":"ssh","gitCredentialId":"key","sshHostsCredentialId":"hosts",
		"env":{"APP_ENV":"dev"},"credentialBindings":{"NPM_TOKEN":"c2"}
	}`
	repos := `[
		{"position":0,"projectId":"p1","name":"app","repoUrl":"https://github.com/acme/app.git","repoDirName":"app","branch":"main",
		 "gitCredentialType":"github_https","gitCredentialId":"cred-1","sshHostsCredentialId":"hosts","env":{"APP_ENV":"dev"},"credentialBindings":{"NPM_TOKEN":"c2"},"note":"keep"},
		{"position":1,"projectId":"p2","name":"web","repoUrl":"git@github.com:acme/web.git","repoDirName":"web","branch":"dev"}
	]`
	cleanRepos := `[{"position":0,"projectId":"p3","name":"lib","repoUrl":"https://github.com/acme/lib.git","repoDirName":"lib"}]`
	insertSession := func(id, projectID, branch, snap, repoList string) {
		t.Helper()
		if err := db.Exec(`INSERT INTO solo_sessions
			(id, user_id, title, agent_name, project_id, branch, project_snapshot, repos, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, "alice", id, "agent", projectID, branch, snap, repoList, "closed", now, now,
		).Error; err != nil {
			t.Fatal(err)
		}
	}
	insertSession("s-dirty", "p1", "main", snapshot, repos)
	insertSession("s-clean", "p3", "", "null", cleanRepos)
	if err := db.Exec(`INSERT INTO solo_sessions
		(id, user_id, title, agent_name, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"s-empty", "alice", "empty", "agent", "closed", now, now,
	).Error; err != nil {
		t.Fatal(err)
	}

	if err := stripProjectRuntimeConfig(db); err != nil {
		t.Fatal(err)
	}
	assertRuntimeColumnsGone(t, db)
	assertSnapshotClean(t, db, "s-dirty")
	first := readSessionJSON(t, db, "s-dirty")
	clean := readSessionJSON(t, db, "s-clean")
	if clean.repos != cleanRepos {
		t.Fatalf("already clean repos changed: %s", clean.repos)
	}
	if err := stripProjectRuntimeConfig(db); err != nil {
		t.Fatal(err)
	}
	assertRuntimeColumnsGone(t, db)
	second := readSessionJSON(t, db, "s-dirty")
	if second.snapshot != first.snapshot || second.repos != first.repos {
		t.Fatal("repeat cleanup rewrote an already clean snapshot")
	}
	again := readSessionJSON(t, db, "s-clean")
	if again.repos != cleanRepos {
		t.Fatal("repeat cleanup changed a snapshot that never had runtime keys")
	}

	var project struct {
		Name          string
		Description   string
		RepoURL       string
		DefaultBranch string
		RepoDirName   string
	}
	if err := db.Raw("SELECT name, description, repo_url AS repo_url, default_branch AS default_branch, repo_dir_name AS repo_dir_name FROM projects WHERE id = ?", "p1").Scan(&project).Error; err != nil {
		t.Fatal(err)
	}
	if project.Name != "app" || project.Description != "demo" || project.RepoURL != "https://github.com/acme/app.git" || project.DefaultBranch != "main" || project.RepoDirName != "app" {
		t.Fatalf("clone fields changed: %+v", project)
	}
}

func columnPresent(db *gorm.DB, column string) (bool, error) {
	cols, err := db.Migrator().ColumnTypes("projects")
	if err != nil {
		return false, err
	}
	for _, col := range cols {
		if strings.EqualFold(col.Name(), column) {
			return true, nil
		}
	}
	return false, nil
}

func assertRuntimeColumnsGone(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, column := range projectRuntimeColumns {
		present, err := columnPresent(db, column)
		if err != nil || present {
			t.Fatalf("column %s present=%v err=%v", column, present, err)
		}
	}
}

type sessionJSON struct {
	snapshot string
	repos    string
}

func readSessionJSON(t *testing.T, db *gorm.DB, id string) sessionJSON {
	t.Helper()
	var row sessionSnapshotRow
	if err := db.Table("solo_sessions").Select("id, project_snapshot, repos").Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	return sessionJSON{snapshot: row.ProjectSnapshot, repos: row.Repos}
}

func assertSnapshotClean(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	row := readSessionJSON(t, db, id)
	var snap map[string]any
	if err := json.Unmarshal([]byte(row.snapshot), &snap); err != nil {
		t.Fatal(err)
	}
	for _, key := range projectRuntimeJSONKeys {
		if _, ok := snap[key]; ok {
			t.Fatalf("snapshot still has %s", key)
		}
	}
	if snap["name"] != "app" || snap["repoUrl"] != "https://github.com/acme/app.git" || snap["defaultBranch"] != "main" || snap["repoDirName"] != "app" || snap["createdBy"] != "alice" {
		t.Fatalf("snapshot clone fields changed: %#v", snap)
	}
	var repos []map[string]any
	if err := json.Unmarshal([]byte(row.repos), &repos); err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0]["name"] != "app" || repos[0]["branch"] != "main" || repos[0]["note"] != "keep" || repos[1]["repoUrl"] != "git@github.com:acme/web.git" || repos[1]["repoDirName"] != "web" {
		t.Fatalf("repos clone fields changed: %#v", repos)
	}
	for _, repo := range repos {
		for _, key := range projectRuntimeJSONKeys {
			if _, ok := repo[key]; ok {
				t.Fatalf("repo snapshot still has %s", key)
			}
		}
	}
}
