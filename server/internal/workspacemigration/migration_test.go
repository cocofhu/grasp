package workspacemigration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

func fixture(t *testing.T, multiple bool) (*gorm.DB, Options) {
	t.Helper()
	root := t.TempDir()
	db, err := database.OpenSQLiteTest(filepath.Join(root, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	return seedFixture(t, db, root, multiple)
}

func seedFixture(t *testing.T, db *gorm.DB, root string, multiple bool) (*gorm.DB, Options) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Where("1 = 1").Delete(&models.Project{}).Error; err != nil {
		t.Fatal(err)
	}
	projects := []models.Project{{ID: "workspace-a", Name: "Alpha", Variables: []models.ProjectVariable{{Name: "region", Type: "string", Value: "a"}}, NotifyPolicy: models.DefaultProjectNotifyPolicy()}}
	if multiple {
		projects = append(projects, models.Project{ID: "workspace-b", Name: "Beta", Variables: []models.ProjectVariable{{Name: "region", Type: "string", Value: "classified-test-value", Secret: true}}, NotifyPolicy: models.ProjectNotifyPolicy{ChannelIDs: []string{"channel-b"}, FailedTemplate: "failure {project}"}})
	}
	if err := db.Create(&projects).Error; err != nil {
		t.Fatal(err)
	}
	opts := Options{CanonicalID: projects[0].ID, ProfilesRoot: filepath.Join(root, "profiles"), SharedRoot: filepath.Join(root, "project-shared")}
	for _, project := range projects {
		name := "agent-" + project.ID
		profile := filepath.Join(opts.ProfilesRoot, name)
		if err := os.MkdirAll(filepath.Join(profile, "workspace"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profile, "workspace", "own.txt"), []byte(project.Name), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(profile, "agent.json"), map[string]any{"projectId": project.ID, "acpBackend": "cursor", "env": map[string]string{"OWN": project.Name}, "capabilities": map[string]any{"allowMemoryWrite": true}}); err != nil {
			t.Fatal(err)
		}
		shared := filepath.Join(opts.SharedRoot, project.ID)
		if err := os.MkdirAll(filepath.Join(shared, "workspace"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(shared, "agent.json"), map[string]any{"env": map[string]string{"BASE": project.Name}, "mcp": []any{map[string]string{"name": "source-tool", "command": "fixture-tool"}}}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shared, "workspace", "shared.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o750); err != nil {
			t.Fatal(err)
		}
		flow := models.WorkflowDef{ID: "flow-" + project.ID, ProjectID: project.ID, Name: "Build", Version: 1, PublishedVersion: 1, Graph: models.Graph{Nodes: []models.Node{{ID: "start", Type: "input"}}}}
		if err := db.Create(&flow).Error; err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if err := db.Create(&models.WorkflowVersion{WorkflowID: flow.ID, Version: 1, Name: flow.Name, Graph: flow.Graph, Source: models.VersionSourceSave, CreatedAt: now, PublishedAt: &now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ProjectMemoryItem{ID: "memory-" + project.ID, ProjectID: project.ID, AgentName: "rememberer", Title: "same title", Content: "preserve-" + project.ID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ProjectCredential{ID: "cred-" + project.ID + "-cursor", ProjectID: project.ID, EnvKey: "GRASP_CURSOR_API_KEY", Type: "ai", Provider: "cursor", Name: project.Name, ValueEnc: "opaque-encrypted-fixture-" + project.ID, Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ProjectMcpApiKey{ID: "key-" + project.ID, ProjectID: project.ID, Name: "external", KeyHash: "hash-" + project.ID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ProjectAuditEvent{ID: "audit-" + project.ID, ProjectID: project.ID, Payload: map[string]any{"keep": project.Name}, Actor: "original", OccurredAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.TokenUsageEvent{ProjectID: project.ID, ProjectName: project.Name, Source: "workflow", ModelKey: "fixture", InputTokens: 123, OutputTokens: 45, CacheReadTokens: 6, CacheWriteTokens: 7}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.Run{ID: "run-" + project.ID, WorkflowID: flow.ID, Graph: flow.Graph, Status: "waiting_human", McpToken: "stable-token-" + project.ID, StartedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, opts
}

func TestPlanReadOnlyAndNoSecretExposure(t *testing.T) {
	db, opts := fixture(t, true)
	before, err := capture(db)
	if err != nil {
		t.Fatal(err)
	}
	files, err := scanFiles(opts.ProfilesRoot)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", plan.Blockers)
	}
	if len(plan.WorkflowRenames) != 1 || len(plan.MemoryRenames) != 1 || len(plan.RevokeMCPKeyIDs) != 2 {
		t.Fatalf("incomplete plan: %+v", plan)
	}
	encoded, _ := json.Marshal(plan)
	for _, sensitive := range []string{"classified-test-value", "opaque-encrypted-fixture", "stable-token", "preserve-workspace"} {
		if bytes.Contains(encoded, []byte(sensitive)) {
			t.Fatalf("report exposed %q", sensitive)
		}
	}
	after, err := capture(db)
	if err != nil {
		t.Fatal(err)
	}
	if digest(before) != digest(after) {
		t.Fatal("planning mutated the database")
	}
	afterFiles, _ := scanFiles(opts.ProfilesRoot)
	if digest(files) != digest(afterFiles) {
		t.Fatal("planning mutated profiles")
	}
}

func TestMergePreservesHistoryDefaultsCredentialsAndVCS(t *testing.T) {
	db, opts := fixture(t, true)
	// Ordinary Save would prune old versions above 200. Migration must not.
	for v := 2; v <= 205; v++ {
		if err := db.Create(&models.WorkflowVersion{WorkflowID: "flow-workspace-b", Version: v, Name: "Build", Graph: models.Graph{}, Source: models.VersionSourceSave}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "flow-workspace-b").Updates(map[string]any{"version": 205}).Error; err != nil {
		t.Fatal(err)
	}
	before, err := capture(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	if err := Verify(db, plan, backup); err != nil {
		t.Fatal(err)
	}
	var flow models.WorkflowDef
	if err := db.First(&flow, "id = ?", "flow-workspace-b").Error; err != nil {
		t.Fatal(err)
	}
	if flow.ProjectID != opts.CanonicalID || flow.PublishedVersion != 1 || flow.Version != 206 {
		t.Fatalf("draft/published relationship changed: %+v", flow)
	}
	if flow.Defaults == nil || flow.Defaults.InheritWorkspace || len(flow.Defaults.Variables) != 1 || !flow.Defaults.Variables[0].Secret || flow.Defaults.Variables[0].Value != "classified-test-value" {
		t.Fatalf("secret defaults not preserved: %+v", flow.Defaults)
	}
	if flow.Defaults.NotifyPolicy == nil || flow.Defaults.NotifyPolicy.FailedTemplate != "failure {project}" {
		t.Fatal("full notification policy lost")
	}
	var versions int64
	if err := db.Model(&models.WorkflowVersion{}).Where("workflow_id = ?", flow.ID).Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if versions != 206 {
		t.Fatalf("old versions pruned: %d", versions)
	}
	var run models.Run
	if err := db.First(&run, "id = ?", "run-workspace-b").Error; err != nil {
		t.Fatal(err)
	}
	if run.McpToken != "stable-token-workspace-b" || run.Status != "waiting_human" {
		t.Fatal("run snapshot changed")
	}
	agent, ok := services.NewAgentService(opts.ProfilesRoot).Get("agent-workspace-b")
	if !ok || agent.ProjectID != opts.CanonicalID || agent.Env["BASE"] != "Beta" {
		t.Fatalf("effective agent not materialized: %+v", agent)
	}
	if agent.CredentialBindings["GRASP_CURSOR_API_KEY"] == nil || *agent.CredentialBindings["GRASP_CURSOR_API_KEY"] != "cred-workspace-b-cursor" {
		t.Fatal("credential identity changed")
	}
	info, err := os.Stat(filepath.Join(opts.ProfilesRoot, agent.Name, "workspace", "shared.sh"))
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Fatal("shared executable permissions lost")
	}
	revisions, err := services.NewWorkspaceVcsService(opts.ProfilesRoot).ListRevisions(agent.Name, 10)
	if err != nil || len(revisions) < 2 {
		t.Fatalf("original workspace not retained in VCS: %v %d", err, len(revisions))
	}
	var credential models.ProjectCredential
	if err := db.First(&credential, "id = ?", "cred-workspace-b-cursor").Error; err != nil {
		t.Fatal(err)
	}
	if credential.ValueEnc != "opaque-encrypted-fixture-workspace-b" || credential.BuiltinSlot != "GRASP_CURSOR_API_KEY" {
		t.Fatal("encrypted credential or default slot changed")
	}
	var keys []models.ProjectMcpApiKey
	if err := db.Find(&keys).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if key.RevokedAt == nil {
			t.Fatal("project-wide key expanded without revocation")
		}
	}
	after, err := capture(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyHistory(before, after, 1); err != nil {
		t.Fatal(err)
	}
	if err := Apply(db, plan, backup, true); err != nil {
		t.Fatalf("completed apply is not idempotent: %v", err)
	}
}

func TestRollbackRestoresDatabaseAndOriginalFiles(t *testing.T) {
	db, opts := fixture(t, true)
	original, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, original, backup, true); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(db, original, backup, true); err != nil {
		t.Fatal(err)
	}
	restored, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Fingerprint != original.Fingerprint {
		t.Fatal("rollback failed to restore the exact source")
	}
	if err := Rollback(db, original, backup, true); err != nil {
		t.Fatal("rollback must be idempotent")
	}
}

func TestNoOverwriteAfterNewWrites(t *testing.T) {
	db, opts := fixture(t, false)
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ProjectAuditEvent{ID: "new-event", Actor: "new-user"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Verify(db, plan, backup); err == nil {
		t.Fatal("verification accepted unrelated writes")
	}
	if err := Rollback(db, plan, backup, true); err == nil {
		t.Fatal("rollback would overwrite newer writes")
	}
	var count int64
	if err := db.Model(&models.ProjectAuditEvent{}).Where("id = ?", "new-event").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("new history lost")
	}
}

func TestStaleBlockedAndOfflineSafety(t *testing.T) {
	db, opts := fixture(t, false)
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, backup, false); err == nil {
		t.Fatal("online apply allowed")
	}
	if err := db.Model(&models.Project{}).Where("id = ?", opts.CanonicalID).UpdateColumn("description", "new write").Error; err != nil {
		t.Fatal(err)
	}
	if err := Apply(db, plan, backup, true); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale plan accepted: %v", err)
	}
	if err := db.Model(&models.Run{}).Where("1 = 1").UpdateColumn("status", "running").Error; err != nil {
		t.Fatal(err)
	}
	plan, err = BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(plan, "active-run") {
		t.Fatal("active run not blocked")
	}
	if err := Apply(db, plan, backup, true); !errors.Is(err, ErrBlocked) {
		t.Fatalf("active apply allowed: %v", err)
	}
}

func TestAuthorizationAndFormerPMJobDecisions(t *testing.T) {
	db, opts := fixture(t, true)
	if err := db.Model(&models.Project{}).Where("id = ?", "workspace-b").Updates(map[string]any{"pm_leader_enabled": true, "pm_leader_agent": "agent-workspace-b"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AgentCronJob{ID: "former-pm-job", ProjectID: "workspace-b", AgentName: "agent-workspace-b", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(plan, "review-tool-authority") || !hasCode(plan, "former-pm-job") {
		t.Fatal("implicit authority change allowed")
	}
	opts.Decisions.ReauthorizeWorkspaceTools, opts.Decisions.PauseFormerPMJobs = true, true
	plan, err = BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if hasCode(plan, "review-tool-authority") || hasCode(plan, "former-pm-job") || len(plan.PauseJobIDs) != 1 {
		t.Fatal("explicit decisions ignored")
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("ordinary workflow policy should represent source defaults: %+v", plan.Blockers)
	}
}

func TestFilePublishFailureCanRollback(t *testing.T) {
	db, opts := fixture(t, true)
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := applyWithPublish(db, plan, backup, true, func(string, string, string) error { return errors.New("synthetic disk failure") }); err == nil {
		t.Fatal("expected publication failure")
	}
	var journal Journal
	if err := readJSON(filepath.Join(backup, "journal.json"), &journal); err != nil {
		t.Fatal(err)
	}
	if journal.Stage != "data-committed" {
		t.Fatalf("wrong recovery stage %s", journal.Stage)
	}
	if err := Rollback(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	restored, err := BuildPlan(db, opts)
	if err != nil || restored.Fingerprint != plan.Fingerprint {
		t.Fatalf("partial migration not restored: %v", err)
	}
	if err := Apply(db, plan, filepath.Join(t.TempDir(), "retry"), true); err != nil {
		t.Fatalf("rollback left unusable staging paths: %v", err)
	}
}

func TestOrphanRepairAndUnsafePaths(t *testing.T) {
	db, opts := fixture(t, false)
	if err := db.Table("artifacts").Create(map[string]any{"id": "orphan", "run_id": "deleted", "name": "history", "project_id": "missing", "kind": "text"}).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil || !hasCode(plan, "orphan-rows") {
		t.Fatal("orphan ignored")
	}
	opts.Decisions.RepairOrphans = true
	plan, err = BuildPlan(db, opts)
	if err != nil || hasCode(plan, "orphan-rows") {
		t.Fatal("orphan decision ignored")
	}
	if err := Apply(db, plan, filepath.Join(opts.ProfilesRoot, "backup"), true); err == nil {
		t.Fatal("nested backup allowed")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(opts.ProfilesRoot, "external")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(db, opts); err == nil {
		t.Fatal("symlink silently followed or omitted")
	}
}

func TestCredentialBindingConversionDoesNotSelectAnotherIdentity(t *testing.T) {
	rows := []models.ProjectCredential{
		{ID: "cred-a-cursor", ProjectID: "a", EnvKey: "KEY", Type: "ai", ValueEnc: "one", Enabled: true},
		{ID: "extra", ProjectID: "a", EnvKey: "KEY", Type: "custom", Enabled: true},
		{ID: "single", ProjectID: "a", EnvKey: "OTHER", Type: "custom", ValueEnc: "two", Enabled: true},
		{ID: "vendor", ProjectID: "a", EnvKey: "GRASP_OPENCODE_API_KEY", Type: "ai", Provider: "opencode", ValueEnc: "three", Enabled: true},
		{ID: "protected", ProjectID: "a", EnvKey: "GRASP_ARTIFACT_TOKEN", Type: "custom", ValueEnc: "four", Enabled: true},
	}
	bindings := sourceBindings(rows, "a")
	if bindings["KEY"] != "" || bindings["OTHER"] != "single" || bindings["GRASP_OPENCODE_API_KEY"] != "" || bindings["GRASP_ARTIFACT_TOKEN"] != "" {
		t.Fatalf("invalid bindings %+v", bindings)
	}
}

func TestTypedSnapshotRoundTrip(t *testing.T) {
	db, _ := fixture(t, false)
	if err := db.Exec("CREATE TABLE typed_fixture (id INTEGER PRIMARY KEY, payload BLOB, large INTEGER, nullable TEXT, stamp DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO typed_fixture VALUES (?, ?, ?, ?, ?)", 1, []byte{0, 255, 3}, int64(9007199254740993), nil, time.Date(2026, 10, 20, 1, 2, 3, 4000, time.UTC)).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := capture(db)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := restore(db, decoded); err != nil {
		t.Fatal(err)
	}
	after, err := capture(db)
	if err != nil || digest(snapshot) != digest(after) {
		t.Fatal("typed database backup changed values", err)
	}
}

func hasCode(p Plan, code string) bool {
	for _, issue := range p.Blockers {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestChooseWorkspaceAndReportIntegrity(t *testing.T) {
	db, opts := fixture(t, true)
	opts.CanonicalID = ""
	plan, err := BuildPlan(db, opts)
	if err != nil || !hasCode(plan, "choose-workspace") {
		t.Fatal("multiple workspaces selected implicitly")
	}
	opts.CanonicalID = "workspace-a"
	plan, err = BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	plan.WorkflowRenames["flow-workspace-b"] = "not part of the accepted plan"
	if err := Apply(db, plan, filepath.Join(t.TempDir(), "backup"), true); !errors.Is(err, ErrStalePlan) {
		t.Fatal("modified plan accepted", err)
	}
	if _, err := decodeCell(Cell{Kind: "invalid"}); err == nil {
		t.Fatal("invalid backup accepted")
	}
	if !strings.Contains(ErrBlocked.Error(), "blocker") {
		t.Fatal("missing operational diagnosis")
	}
}
