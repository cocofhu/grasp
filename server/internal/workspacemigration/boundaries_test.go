package workspacemigration

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/models"
	agentruntime "github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExplicitCredentialSurvivesAmbiguousSourceAndMerge(t *testing.T) {
	db, opts := fixture(t, true)
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	encrypted, err := crypto.Encrypt("chosen-identity")
	if err != nil {
		t.Fatal(err)
	}
	const selected = "chosen-ai"
	if err := db.Create(&models.ProjectCredential{ID: selected, Name: "Extra identity", ProjectID: "workspace-b", Type: "ai", Provider: "cursor", EnvKey: "GRASP_CURSOR_API_KEY", Enabled: true, ValueEnc: encrypted}).Error; err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(opts.SharedRoot, "workspace-b", "agent.json")
	var baseline map[string]any
	if err := readJSON(shared, &baseline); err != nil {
		t.Fatal(err)
	}
	baseline["aiCredentialId"] = selected
	if err := writeJSON(shared, baseline); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(db, plan, filepath.Join(t.TempDir(), "backup"), true); err != nil {
		t.Fatal(err)
	}
	agent, _ := services.NewAgentService(opts.ProfilesRoot).Get("agent-workspace-b")
	service := services.NewProjectCredentialService(db)
	env, creds := map[string]string{}, service.ResolveEnv(opts.CanonicalID)
	agentruntime.ApplyAgentCredentialChoice(env, creds, opts.CanonicalID, agentruntime.AgentCredentialChoice{Backend: agent.AcpBackend, AiCredentialID: agent.AiCredentialID}, service.ResolveSelectedValue)
	agentruntime.ApplyCredentialBindings(env, creds, opts.CanonicalID, agent.CredentialBindings, service.ResolveSelectedValue)
	if env["GRASP_CURSOR_API_KEY"] != "chosen-identity" || creds["GRASP_CURSOR_API_KEY"] != "chosen-identity" {
		t.Fatal("explicit identity erased by ambiguous default binding")
	}
}

func TestInheritedAndGenericReferencesCannotAcquireNewAuthority(t *testing.T) {
	for _, mode := range []string{"binding", "shared-ai", "shared-mcp", "env"} {
		t.Run(mode, func(t *testing.T) {
			db, opts := fixture(t, true)
			path := filepath.Join(opts.ProfilesRoot, "agent-workspace-a", "agent.json")
			var raw map[string]any
			if mode == "shared-ai" || mode == "shared-mcp" {
				path = filepath.Join(opts.SharedRoot, "workspace-a", "agent.json")
			}
			if err := readJSON(path, &raw); err != nil {
				t.Fatal(err)
			}
			const foreign = "cred-workspace-b-cursor"
			switch mode {
			case "binding":
				raw["credentialBindings"] = map[string]string{"GRASP_CURSOR_API_KEY": foreign}
			case "shared-ai":
				raw["aiCredentialId"] = foreign
			case "shared-mcp":
				raw["mcp"] = []any{map[string]any{"name": "remote", "command": "remote", "env": map[string]string{"TOKEN": "${credential:" + foreign + "}"}}}
			case "env":
				raw["env"] = map[string]string{"TOKEN": "${credential:" + foreign + "}"}
			}
			if err := writeJSON(path, raw); err != nil {
				t.Fatal(err)
			}
			plan, err := BuildPlan(db, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !hasCode(plan, "cross-source-credential") && !hasCode(plan, "cross-source-mcp-secret") {
				t.Fatalf("cross-source authority unblocked: %s", mode)
			}
		})
	}
}

func TestPartialSchemaRollbackAndReservedPaths(t *testing.T) {
	db, opts := fixture(t, false)
	if err := db.Migrator().DropColumn(&models.WorkflowDef{}, "Defaults"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&models.ProjectCredential{}, "BuiltinSlot"); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	before, err := capture(db)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := writeJSON(filepath.Join(backup, "database.json"), before); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(opts.ProfilesRoot, filepath.Join(backup, "profiles")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(opts.SharedRoot, filepath.Join(backup, "shared")); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(backup, "journal.json"), Journal{Version: Version, Stage: "backed-up", PlanDigest: digest(plan)}); err != nil {
		t.Fatal(err)
	}
	// First DDL committed; the process stopped before its second ALTER or journal update.
	if err := db.Migrator().AddColumn(&models.WorkflowDef{}, "Defaults"); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	after, err := capture(db)
	if err != nil || !originalDataUnchanged(before, after) {
		t.Fatalf("partial DDL recovery lost original cells: %v", err)
	}
	// A user directory in the reserved namespace is rejected before backup or DML.
	plan, err = BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(heldRoot(plan, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	freshBackup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, freshBackup, true); err == nil {
		t.Fatal("preexisting retained directory accepted")
	}
	if _, err := os.Stat(freshBackup); !os.IsNotExist(err) {
		t.Fatal("reserved path failure wrote a backup or journal")
	}
}

// Opt-in test: accepts only an empty disposable database with this exact name.
func TestMySQLMigration(t *testing.T) {
	dsn := os.Getenv("GRASP_WORKSPACE_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set GRASP_WORKSPACE_MIGRATION_TEST_DSN for disposable MySQL integration")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT DATABASE()").Scan(&name).Error; err != nil || name != "grasp_workspace_migration_test" {
		t.Fatal("integration requires the isolated grasp_workspace_migration_test database")
	}
	tables, err := db.Migrator().GetTables()
	if err != nil || len(tables) != 0 {
		t.Fatal("integration requires an empty disposable database")
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	db, opts := seedFixture(t, db, t.TempDir(), true)
	if err := db.Create(&models.TokenUsageEvent{ID: uint(9007199254740993), ProjectID: "workspace-b", Source: "workflow", ModelKey: "large-id", InputTokens: 123}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, row := range []models.ProjectCredential{
		{ID: "alias-a", ProjectID: "workspace-a", Type: "custom", Name: "Shared", EnvKey: "EXAMPLE_KEY", ValueEnc: "opaque", Enabled: true},
		{ID: "alias-b", ProjectID: "workspace-b", Type: "custom", Name: " shared ", EnvKey: "example_key", ValueEnc: "opaque", Enabled: false, RevokedAt: &now},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// The default MySQL collation treats these accent variants as the same name.
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "flow-workspace-a").UpdateColumn("name", "Café").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "flow-workspace-b").UpdateColumn("name", "Cafe").Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.WorkflowRenames) != 1 {
		t.Fatalf("MySQL collation collision missed: %+v", plan.WorkflowRenames)
	}
	if plan.CredentialRenames["alias-b"] == "" {
		t.Fatal("MySQL EnvKey collation or revoked alias collision missed")
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	if err := Verify(db, plan, backup); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	restored, err := BuildPlan(db, opts)
	if err != nil || restored.Fingerprint != plan.Fingerprint {
		t.Fatalf("MySQL round trip differs: %v", err)
	}
}

func TestPersistentInflightWorkAndIncompleteProfilesBlock(t *testing.T) {
	db, opts := fixture(t, false)
	if err := db.Create(&models.ChatTurnDraft{ID: "turn", ThreadID: "thread", Status: "streaming"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AgentCronRun{ID: "cron", Status: "running"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(opts.ProfilesRoot, ".unfinished"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(opts.SharedRoot, "missing"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"active-turn", "active-cron-run", "invalid-agent-profile", "orphan-shared"} {
		if !hasCode(plan, code) {
			t.Fatalf("missing blocker %s: %+v", code, plan.Blockers)
		}
	}
}

func TestMemoryRenameCollisionChainAndBackupIntegrity(t *testing.T) {
	db, opts := fixture(t, true)
	if err := db.Create(&models.ProjectMemoryItem{ID: "memory-0", ProjectID: "workspace-a", AgentName: "rememberer", Title: "same title · memory-workspace-b", Content: "original"}).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.MemoryRenames["memory-workspace-b"] != "same title · memory-workspace-b (2)" {
		t.Fatalf("collision suffix reused: %+v", plan.MemoryRenames)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := Apply(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(backup, "profiles", "agent-workspace-a", "workspace", "own.txt")
	if err := os.WriteFile(file, []byte("corrupted backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(db, plan, backup); err == nil {
		t.Fatal("corrupt backup verified")
	}
	if err := Rollback(db, plan, backup, true); err == nil {
		t.Fatal("corrupt backup restored")
	}
}

func TestBackupAliasesCannotOverlapLiveTrees(t *testing.T) {
	db, opts := fixture(t, false)
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "live-alias")
	if err := os.Symlink(filepath.Dir(opts.ProfilesRoot), alias); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(alias, filepath.Base(opts.ProfilesRoot), "new", "backup")
	if err := Apply(db, plan, backup, true); err == nil {
		t.Fatal("symlink ancestor bypassed overlap validation")
	}
	if _, err := os.Stat(filepath.Join(opts.ProfilesRoot, "new")); !os.IsNotExist(err) {
		t.Fatal("unsafe backup path modified live profiles")
	}
}

func TestLiveWorkflowSandboxCannotCarryOldRuntimeConfiguration(t *testing.T) {
	db, opts := fixture(t, true)
	if err := db.Create(&models.Sandbox{Name: "live-workflow", ProjectID: "workspace-b", Purpose: "run", Status: "running"}).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(plan, "live-workflow-sandbox") {
		t.Fatal("live old runtime configuration accepted")
	}
}

func TestCredentialAliasesRemainDistinctAndEditable(t *testing.T) {
	db, opts := fixture(t, true)
	opts.Decisions.CredentialNames = map[string]string{"cred-workspace-a-cursor": "Cursor API Key"}
	for id, name := range map[string]string{"cred-workspace-a-cursor": "Cursor API Key", "cred-workspace-b-cursor": "Cursor"} {
		if err := db.Model(&models.ProjectCredential{}).Where("id = ?", id).UpdateColumn("name", name).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CredentialRenames["cred-workspace-a-cursor"] != "Cursor" || plan.CredentialRenames["cred-workspace-b-cursor"] == "" {
		t.Fatalf("credential alias collision missed: %+v", plan.CredentialRenames)
	}
	if err := Apply(db, plan, filepath.Join(t.TempDir(), "backup"), true); err != nil {
		t.Fatal(err)
	}
	var rows []models.ProjectCredential
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows[0].Name == rows[1].Name || rows[1].ValueEnc != "opaque-encrypted-fixture-workspace-b" {
		t.Fatal("alias or identity changed incorrectly")
	}
	// Ordinary save of the displayed alias must work after the merge.
	if _, err := services.NewProjectCredentialService(db).Update(opts.CanonicalID, rows[1].ID, services.ProjectCredentialInput{Name: rows[1].Name}); err != nil {
		t.Fatal(err)
	}
}

func TestMissingBuiltinSlotWillNotIntroduceDuplicateAliases(t *testing.T) {
	db, opts := fixture(t, true)
	if err := db.Delete(&models.ProjectCredential{}, "id = ?", "cred-workspace-a-cursor").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProjectCredential{}).Where("id = ?", "cred-workspace-b-cursor").UpdateColumn("name", "Cursor").Error; err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CredentialRenames["cred-workspace-b-cursor"] == "" {
		t.Fatal("future builtin alias was not reserved")
	}
	if err := Apply(db, plan, filepath.Join(t.TempDir(), "backup"), true); err != nil {
		t.Fatal(err)
	}
	service := services.NewProjectCredentialService(db)
	rows, err := service.List(opts.CanonicalID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.EnvKey != "GRASP_CURSOR_API_KEY" {
			continue
		}
		if seen[row.Name] {
			t.Fatal("normal List reintroduced a duplicate alias")
		}
		seen[row.Name] = true
		if _, err := service.Update(opts.CanonicalID, row.ID, services.ProjectCredentialInput{Name: row.Name}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationAndRollbackPreserveFilePermissions(t *testing.T) {
	db, opts := fixture(t, false)
	file := filepath.Join(opts.ProfilesRoot, "agent-workspace-a", "workspace", "own.txt")
	// Group write is commonly removed by umask 0022 when a copy is created.
	// Fingerprints and restored access must retain the explicit source mode.
	if err := os.Chmod(file, 0770); err != nil {
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
	for _, path := range []string{file, filepath.Join(backup, "profiles", "agent-workspace-a", "workspace", "own.txt")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0770 {
			t.Fatalf("copied permissions differ at %s: %v %v", path, info, err)
		}
	}
	if err := Rollback(db, plan, backup, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0770 {
		t.Fatalf("rollback permissions differ: %v %v", info, err)
	}
}
