package workspacemigration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	agentruntime "github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

type Journal struct {
	Version           int    `json:"version"`
	Stage             string `json:"stage"`
	PlanDigest        string `json:"planDigest"`
	SchemaDatabase    string `json:"schemaDatabase,omitempty"`
	AfterDatabase     string `json:"afterDatabase,omitempty"`
	AfterProfiles     string `json:"afterProfiles,omitempty"`
	AfterShared       string `json:"afterShared,omitempty"`
	ResultFingerprint string `json:"resultFingerprint,omitempty"`
	FilesStarted      bool   `json:"filesStarted,omitempty"`
}

var (
	ErrStalePlan = errors.New("migration plan is stale or modified; run plan again")
	ErrBlocked   = errors.New("migration has unresolved preflight blockers")
)

// Apply requires the operator to stop every writer and drain the old process
// before it is called. It never silently clears active jobs or kills sandboxes.
func Apply(db *gorm.DB, plan Plan, backupDir string, offline bool) error {
	return applyWithPublish(db, plan, backupDir, offline, publishTree)
}

func applyWithPublish(db *gorm.DB, plan Plan, backupDir string, offline bool, publish func(string, string, string) error) error {
	if !offline {
		return fmt.Errorf("offline acknowledgement is required after draining all writers and outbound queues")
	}
	backupDir, err := validateBackup(plan, backupDir)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(backupDir, "journal.json")
	if _, err := os.Stat(journalPath); err == nil {
		var existing Journal
		if err := readJSON(journalPath, &existing); err != nil {
			return err
		}
		if existing.Stage == "complete" && existing.PlanDigest == digest(plan) {
			return Verify(db, plan, backupDir)
		}
		return fmt.Errorf("an unfinished migration journal exists; verify or roll it back before retrying")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	current, err := BuildPlan(db, plan.Options)
	if err != nil {
		return err
	}
	if plan.Version != Version || digest(current) != digest(plan) {
		return ErrStalePlan
	}
	if len(plan.Blockers) > 0 {
		return ErrBlocked
	}
	for _, kind := range []string{"profiles", "shared"} {
		for _, path := range []string{stageRoot(plan, kind), heldRoot(plan, kind)} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("migration staging or retained path already exists; resolve it before applying")
			}
		}
	}
	if entries, err := os.ReadDir(backupDir); err == nil && len(entries) > 0 {
		return fmt.Errorf("backup directory must be empty")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	source, err := load(db, plan.Options)
	if err != nil {
		return err
	}
	if digest(struct {
		Database         Snapshot
		Profiles, Shared []FileDigest
	}{source.snapshot, source.profiles, source.shared}) != plan.Fingerprint {
		return ErrStalePlan
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(backupDir, 0o700); err != nil {
		return err
	}
	for name, value := range map[string]any{"plan.json": plan, "database.json": source.snapshot} {
		if err := writeJSON(filepath.Join(backupDir, name), value); err != nil {
			return err
		}
	}
	if err := copyTree(plan.Options.ProfilesRoot, filepath.Join(backupDir, "profiles")); err != nil {
		return err
	}
	if err := copyTree(plan.Options.SharedRoot, filepath.Join(backupDir, "shared")); err != nil {
		return err
	}
	journal := Journal{Version: Version, Stage: "backed-up", PlanDigest: digest(plan)}
	if err := writeJSON(journalPath, journal); err != nil {
		return err
	}
	// DDL is a separately journaled stage: MySQL does not roll it back with DML.
	for _, column := range []struct {
		model any
		name  string
	}{
		{&models.WorkflowDef{}, "Defaults"}, {&models.ProjectCredential{}, "BuiltinSlot"},
	} {
		if db.Migrator().HasTable(column.model) && !db.Migrator().HasColumn(column.model, column.name) {
			if err := db.Migrator().AddColumn(column.model, column.name); err != nil {
				return err
			}
		}
	}
	schema, err := capture(db)
	if err != nil {
		return err
	}
	journal.SchemaDatabase, journal.Stage = digest(schema), "schema-ready"
	if err := writeJSON(journalPath, journal); err != nil {
		return err
	}
	journal.FilesStarted = true
	if err := writeJSON(journalPath, journal); err != nil {
		return err
	}
	if err := prepareFiles(plan, source, backupDir); err != nil {
		return err
	}
	profileStage, sharedStage := stageRoot(plan, "profiles"), stageRoot(plan, "shared")
	profiles, err := scanFiles(profileStage)
	if err != nil {
		return err
	}
	shared, err := scanFiles(sharedStage)
	if err != nil {
		return err
	}
	journal.AfterProfiles, journal.AfterShared = digest(profiles), digest(shared)
	currentProfiles, err := scanFiles(plan.Options.ProfilesRoot)
	if err != nil {
		return err
	}
	currentShared, err := scanFiles(plan.Options.SharedRoot)
	if err != nil {
		return err
	}
	if digest(currentProfiles) != digest(source.profiles) || digest(currentShared) != digest(source.shared) {
		return ErrStalePlan
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		current, err := capture(tx)
		if err != nil {
			return err
		}
		if digest(current) != journal.SchemaDatabase {
			return ErrStalePlan
		}
		if err := convertData(tx, plan, source); err != nil {
			return err
		}
		after, err := capture(tx)
		if err != nil {
			return err
		}
		if err := verifyHistory(source.snapshot, after, len(plan.WorkflowRenames)); err != nil {
			return err
		}
		journal.AfterDatabase, journal.Stage = digest(after), "data-prepared"
		return writeJSON(journalPath, journal)
	}); err != nil {
		return err
	}
	journal.Stage = "data-committed"
	if err := writeJSON(journalPath, journal); err != nil {
		return err
	}
	if err := publish(plan.Options.ProfilesRoot, profileStage, heldRoot(plan, "profiles")); err != nil {
		return err
	}
	journal.Stage = "profiles-published"
	if err := writeJSON(journalPath, journal); err != nil {
		return err
	}
	if err := publish(plan.Options.SharedRoot, sharedStage, heldRoot(plan, "shared")); err != nil {
		return err
	}
	result, err := BuildPlan(db, plan.Options)
	if err != nil {
		return err
	}
	journal.Stage, journal.ResultFingerprint = "complete", result.Fingerprint
	if err := writeJSON(journalPath, journal); err != nil {
		return err
	}
	return Verify(db, plan, backupDir)
}

func validateBackup(plan Plan, backupDir string) (string, error) {
	if backupDir == "" || plan.Options.ProfilesRoot == "" || plan.Options.SharedRoot == "" {
		return "", fmt.Errorf("backup, profiles and shared paths are required")
	}
	backupDir, err := CanonicalPath(backupDir)
	if err != nil {
		return "", err
	}
	for _, root := range []string{plan.Options.ProfilesRoot, plan.Options.SharedRoot} {
		root, err = CanonicalPath(root)
		if err != nil {
			return "", err
		}
		if containsPath(root, backupDir) || containsPath(backupDir, root) {
			return "", fmt.Errorf("backup must not overlap either live data directory")
		}
	}
	return backupDir, nil
}

func stageRoot(plan Plan, kind string) string {
	root := plan.Options.ProfilesRoot
	if kind == "shared" {
		root = plan.Options.SharedRoot
	}
	return root + ".workspace-" + plan.Fingerprint[:12] + ".stage"
}

func heldRoot(plan Plan, kind string) string { return stageRoot(plan, kind) + ".original" }

func publishTree(root, staged, held string) error {
	if _, err := os.Lstat(held); err == nil {
		return fmt.Errorf("original directory already retained; recover the migration first")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, held); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(staged, root)
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func readBackup(plan Plan, backupDir string) (Snapshot, error) {
	var original Snapshot
	if err := readJSON(filepath.Join(backupDir, "database.json"), &original); err != nil {
		return original, err
	}
	profiles, err := scanFiles(filepath.Join(backupDir, "profiles"))
	if err != nil {
		return original, err
	}
	shared, err := scanFiles(filepath.Join(backupDir, "shared"))
	if err != nil {
		return original, err
	}
	if digest(struct {
		Database         Snapshot
		Profiles, Shared []FileDigest
	}{original, profiles, shared}) != plan.Fingerprint {
		return original, fmt.Errorf("migration backup integrity check failed")
	}
	return original, nil
}

func convertData(tx *gorm.DB, plan Plan, source sourceState) error {
	id := plan.Options.CanonicalID
	now := time.Now().UTC()
	projects := map[string]models.Project{}
	for _, project := range source.projects {
		projects[project.ID] = project
	}
	if len(projects) == 0 {
		if err := tx.Create(&models.Project{ID: id, Name: models.DefaultProjectName, Variables: []models.ProjectVariable{}, NotifyPolicy: models.DefaultProjectNotifyPolicy(), CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
	}
	// Temporary names make user-chosen name swaps and cross-project duplicates
	// safe under the existing unique index. They never escape this transaction.
	for workflowID := range plan.WorkflowRenames {
		if err := tx.Table("workflow_defs").Where("id = ?", workflowID).UpdateColumn("name", "__workspace_"+plan.Fingerprint[:16]+"_"+workflowID).Error; err != nil {
			return err
		}
	}
	for _, flow := range source.flows {
		updates := map[string]any{"project_id": id}
		if old, ok := projects[flow.ProjectID]; flow.ProjectID != id || !ok {
			if !ok {
				off := false
				old.NotifyPolicy = models.ProjectNotifyPolicy{Enabled: &off}
			}
			gate := gatePolicy(old)
			defaults := models.WorkflowDefaults{InheritWorkspace: false, Variables: old.Variables, NotifyPolicy: &old.NotifyPolicy, GateAuto: &gate}
			if flow.Defaults != nil {
				defaults = *flow.Defaults
				if defaults.InheritWorkspace {
					defaults.Variables = mergeDefaults(old.Variables, defaults.Variables)
					defaults.InheritWorkspace = false
				}
				if defaults.NotifyPolicy == nil {
					defaults.NotifyPolicy = &old.NotifyPolicy
				}
				if defaults.GateAuto == nil {
					defaults.GateAuto = &gate
				}
			}
			b, err := json.Marshal(defaults)
			if err != nil {
				return err
			}
			updates["defaults"] = string(b)
		}
		if name, ok := plan.WorkflowRenames[flow.ID]; ok {
			var maxVersion int
			if err := tx.Model(&models.WorkflowVersion{}).Where("workflow_id = ?", flow.ID).Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
				return err
			}
			if maxVersion < flow.Version {
				maxVersion = flow.Version
			}
			version := models.WorkflowVersion{WorkflowID: flow.ID, Version: maxVersion + 1, Name: name, Description: flow.Description, Graph: flow.Graph, NodeCount: len(flow.Graph.Nodes), Source: models.VersionSourceSave, CreatedAt: now}
			updates["name"], updates["version"], updates["updated_at"] = name, version.Version, now
			if flow.PublishedVersion > 0 && flow.PublishedVersion == flow.Version {
				version.PublishedAt = &now
				updates["published_version"] = version.Version
			}
			// Do not call Save/appendVersion: those prune unpinned old versions.
			if err := tx.Create(&version).Error; err != nil {
				return err
			}
		}
		if err := tx.Table("workflow_defs").Where("id = ?", flow.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	for memoryID := range plan.MemoryRenames {
		if err := tx.Table("project_memory_items").Where("id = ?", memoryID).UpdateColumn("title", "__workspace_"+plan.Fingerprint[:16]+"_"+memoryID).Error; err != nil {
			return err
		}
	}
	for memoryID, title := range plan.MemoryRenames {
		if err := tx.Table("project_memory_items").Where("id = ?", memoryID).UpdateColumn("title", title).Error; err != nil {
			return err
		}
	}
	for _, credential := range source.creds {
		if name, ok := plan.CredentialRenames[credential.ID]; ok {
			if err := tx.Model(&models.ProjectCredential{}).Where("id = ?", credential.ID).UpdateColumn("name", name).Error; err != nil {
				return err
			}
		}
		if isBuiltin(credential) && credential.BuiltinSlot == "" {
			if err := tx.Model(&models.ProjectCredential{}).Where("id = ?", credential.ID).UpdateColumn("builtin_slot", credential.EnvKey).Error; err != nil {
				return err
			}
		}
	}
	if tx.Migrator().HasTable("channel_configs") {
		if err := tx.Table("channel_configs").Where("project_id <> ?", id).UpdateColumn("is_primary", false).Error; err != nil {
			return err
		}
	}
	for _, table := range source.snapshot.Tables {
		if table.Name == "workflow_defs" || table.Name == "project_external_mcp_settings" || table.Name == "project_audit_events" || table.Name == "token_usage_events" {
			continue
		}
		for _, col := range table.Columns {
			if col == "project_id" {
				if err := tx.Table(table.Name).Where("1 = 1").UpdateColumn(col, id).Error; err != nil {
					return err
				}
			}
		}
	}
	if tx.Migrator().HasTable("project_external_mcp_settings") {
		if err := tx.Table("project_external_mcp_settings").Where("project_id <> ?", id).Delete(nil).Error; err != nil {
			return err
		}
	}
	if len(plan.RevokeMCPKeyIDs) > 0 {
		if err := tx.Model(&models.ProjectMcpApiKey{}).Where("id IN ?", plan.RevokeMCPKeyIDs).UpdateColumn("revoked_at", now).Error; err != nil {
			return err
		}
	}
	if len(plan.PauseJobIDs) > 0 {
		if err := tx.Model(&models.AgentCronJob{}).Where("id IN ?", plan.PauseJobIDs).UpdateColumn("enabled", false).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable("chat_threads") {
		if err := tx.Table("chat_threads").Where("sandbox_ref <> ''").UpdateColumn("sandbox_ref", "").Error; err != nil {
			return err
		}
	}
	return tx.Where("id <> ?", id).Delete(&models.Project{}).Error
}

// Normalize historical built-in labels before reserving aliases; otherwise the
// credential list's ordinary label normalization could introduce a new collision.
func migratedCredentialName(row models.ProjectCredential) string {
	if !isBuiltin(row) {
		return row.Name
	}
	aliases := map[string]string{
		"Cursor API Key": "Cursor", "cursor API Key": "Cursor", "Claude Code API Key": "Claude Code", "claude_code API Key": "Claude Code",
		"CodeBuddy API Key": "CodeBuddy", "codebuddy API Key": "CodeBuddy", "Trae API Token": "Trae", "trae API Key": "Trae",
		"OpenCode API Key": "OpenCode", "opencode API Key": "OpenCode", "Codex Login File": "Codex",
		"GitHub HTTPS Token": "GitHub", "GitLab HTTPS Token": "GitLab", "GitLab HTTPS URL": "GitLab URL", "SSH Private Key": "SSH Key", "SSH Known Hosts": "SSH Hosts",
	}
	if name, ok := aliases[strings.TrimSpace(row.Name)]; ok {
		return name
	}
	return row.Name
}

// The normal credential list creates any missing canonical slots. Reserve their
// aliases now so the first list after migration cannot introduce a collision.
func futureBuiltinAliases(rows []models.ProjectCredential, workspaceID string, key func(string) (string, error)) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, row := range rows {
		ids[row.ID] = true
	}
	used := map[string]bool{}
	for _, slot := range []struct{ suffix, env, name string }{
		{"cursor", envauth.EnvCursorAPIKey, "Cursor"}, {"claude", envauth.EnvClaudeAPIKey, "Claude Code"}, {"codebuddy", envauth.EnvCodeBuddyAPIKey, "CodeBuddy"},
		{"trae", envauth.EnvTraeAPIKey, "Trae"}, {"opencode", envauth.EnvOpenCodeAPIKey, "OpenCode"}, {"codex", envauth.EnvCodexAuthFile, "Codex"},
		{"github", envauth.EnvGitHubToken, "GitHub"}, {"gitlab", envauth.EnvGitLabToken, "GitLab"}, {"gitlab-url", "GITLAB_URL", "GitLab URL"},
		{"ssh-key", envauth.EnvGitSSHPrivateKey, "SSH Key"}, {"ssh-hosts", envauth.EnvGitSSHKnownHosts, "SSH Hosts"},
	} {
		if ids["cred-"+workspaceID+"-"+slot.suffix] {
			continue
		}
		env, err := key(slot.env)
		if err != nil {
			return nil, err
		}
		used[env+"\x00"+strings.ToLower(slot.name)] = true
	}
	return used, nil
}

func mergeDefaults(base, overlay []models.ProjectVariable) []models.ProjectVariable {
	result := append([]models.ProjectVariable{}, base...)
	for _, value := range overlay {
		found := false
		for i := range result {
			if result[i].Name == value.Name {
				result[i], found = value, true
				break
			}
		}
		if !found {
			result = append(result, value)
		}
	}
	return result
}

func prepareFiles(plan Plan, source sourceState, backupDir string) error {
	profiles, shared := stageRoot(plan, "profiles"), stageRoot(plan, "shared")
	for _, path := range []string{profiles, shared} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("migration staging directory exists; recover it before retrying")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := copyTree(filepath.Join(backupDir, "profiles"), profiles); err != nil {
		return err
	}
	if err := copyTree(filepath.Join(backupDir, "shared"), shared); err != nil {
		return err
	}
	byID := map[string]models.ProjectCredential{}
	allKeys := map[string]bool{}
	for _, credential := range source.creds {
		byID[credential.ID] = credential
		if envCredential(credential) {
			allKeys[credential.EnvKey] = true
		}
	}
	materialize := len(source.projects) > 1
	for _, sharedID := range source.sharedIDs {
		materialize = materialize || sharedID != plan.Options.CanonicalID
	}
	for _, agent := range source.agents {
		materialize = materialize || agent.ProjectID != plan.Options.CanonicalID
	}
	baselines := services.NewSharedAgentService(filepath.Join(backupDir, "shared"))
	for _, agent := range source.agents {
		path := filepath.Join(profiles, agent.Name, "agent.json")
		var raw map[string]any
		if err := readJSON(path, &raw); err != nil {
			return err
		}
		raw["projectId"] = plan.Options.CanonicalID
		if materialize {
			if err := services.NewWorkspaceVcsService(profiles).EnsureBaseline(agent.Name); err != nil {
				return err
			}
			base := baselines.Get(agent.ProjectID)
			effective := services.ExtendOverlayWithKind(base, agent, func(id string) (string, bool) {
				row, ok := byID[id]
				return row.EnvKey, ok && row.ProjectID == agent.ProjectID && row.Enabled && row.RevokedAt == nil && row.ValueEnc != ""
			})
			raw["acpBackend"], raw["gitCredentialType"], raw["layout"] = effective.AcpBackend, effective.GitCredentialType, effective.Layout
			raw["env"], raw["mcp"] = effective.Env, effective.MCP
			raw["aiCredentialId"], raw["openCodeCredentialId"] = effective.AiCredentialID, effective.OpenCodeCredentialID
			bindings := map[string]*string{}
			selected := sourceBindings(source.creds, agent.ProjectID)
			for key := range allKeys {
				if id, ok := selected[key]; ok {
					value := id
					bindings[key] = &value
				} else {
					bindings[key] = nil
				}
			}
			// Explicit choices override implicit single-slot resolution. In
			// particular, two stored AI identities must not erase the selected one.
			applyChoice := func(credentialID, key string) {
				if credentialID == "" || key == "" {
					return
				}
				row, ok := byID[credentialID]
				bindings[key] = nil
				if ok && row.ProjectID == agent.ProjectID && row.EnvKey == key && row.Enabled && row.RevokedAt == nil && row.ValueEnc != "" {
					id := credentialID
					bindings[key] = &id
				}
			}
			if effective.AcpBackend == "opencode" {
				applyChoice(effective.OpenCodeCredentialID, envauth.EnvOpenCodeAPIKey)
			} else {
				applyChoice(effective.AiCredentialID, agentruntime.CredentialEnvKeyForBackend(effective.AcpBackend))
			}
			applyChoice(agent.GitCredentialID, agentruntime.CredentialEnvKeyForGit(effective.GitCredentialType))
			if effective.GitCredentialType == "ssh" {
				applyChoice(agent.SshHostsCredentialID, envauth.EnvGitSSHKnownHosts)
			}
			for key, value := range agent.CredentialBindings {
				bindings[key] = value
			}
			raw["credentialBindings"] = bindings
			for _, file := range effective.Files {
				rel := filepath.FromSlash(file.Path)
				target := filepath.Join(profiles, agent.Name, services.WorkDirName, rel)
				if !containsPath(filepath.Join(profiles, agent.Name, services.WorkDirName), target) {
					return fmt.Errorf("unsafe effective Agent file")
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
					return err
				}
				mode := os.FileMode(0o600)
				if info, err := os.Stat(target); err == nil {
					mode = info.Mode().Perm()
				} else if info, err := os.Stat(filepath.Join(baselines.WorkDir(agent.ProjectID), rel)); err == nil {
					mode = info.Mode().Perm()
				}
				if err := os.WriteFile(target, []byte(file.Content), mode); err != nil {
					return err
				}
			}
		}
		if err := writeJSON(path, raw); err != nil {
			return err
		}
		if materialize {
			vcs := services.NewWorkspaceVcsService(profiles)
			if _, err := vcs.CommitChange(agent.Name, services.VcsCommitMeta{Author: "system", Reason: "Consolidate workspace defaults", Source: "migration"}); err != nil {
				return err
			}
		}
	}
	if materialize {
		if err := os.RemoveAll(shared); err != nil {
			return err
		}
		return os.MkdirAll(shared, 0o700) // The new shared baseline is empty.
	}
	return nil
}

func envCredential(row models.ProjectCredential) bool {
	if strings.EqualFold(strings.TrimSpace(row.Type), "ai") && strings.EqualFold(strings.TrimSpace(row.Provider), "opencode") {
		return false // OpenCode model vendors require an explicit model selection.
	}
	switch strings.ToLower(row.Type) {
	case "ai", "git", "ssh", "mcp", "custom":
		return strings.TrimSpace(row.EnvKey) != "" && !envauth.IsPlatformReservedEnvKey(row.EnvKey)
	}
	return false
}

func isBuiltin(row models.ProjectCredential) bool {
	if row.BuiltinSlot != "" {
		return true
	}
	for _, suffix := range []string{"cursor", "claude", "codebuddy", "trae", "opencode", "codex", "github", "gitlab", "gitlab-url", "ssh-key", "ssh-hosts"} {
		if row.ID == "cred-"+row.ProjectID+"-"+suffix {
			return true
		}
	}
	return false
}

func sourceBindings(rows []models.ProjectCredential, projectID string) map[string]string {
	type kind struct {
		id      string
		count   int
		cleared bool
	}
	kinds := map[string]*kind{}
	for _, row := range rows {
		if row.ProjectID != projectID || !row.Enabled || row.RevokedAt != nil || !envCredential(row) {
			continue
		}
		if kinds[row.EnvKey] == nil {
			kinds[row.EnvKey] = &kind{}
		}
		entry := kinds[row.EnvKey]
		if row.ValueEnc == "" {
			entry.cleared = entry.cleared || !isBuiltin(row)
		} else {
			entry.count++
			entry.id = row.ID
		}
	}
	result := map[string]string{}
	for key, value := range kinds {
		if value.count == 1 && !value.cleared {
			result[key] = value.id
		}
	}
	return result
}

func verifyHistory(before, after Snapshot, renames int) error {
	modified := map[string]bool{"projects": true, "workflow_defs": true, "project_credentials": true, "artifacts": true, "sandboxes": true, "project_memory_items": true, "chat_threads": true, "agent_cron_jobs": true, "channel_configs": true, "requirement_drafts": true, "project_external_mcp_settings": true, "project_mcp_api_keys": true}
	byName := map[string]TableSnapshot{}
	for _, table := range after.Tables {
		byName[table.Name] = table
	}
	for _, table := range before.Tables {
		current, ok := byName[table.Name]
		if !ok {
			return fmt.Errorf("migration lost table %s", table.Name)
		}
		if modified[table.Name] {
			if table.Name != "projects" && table.Name != "project_external_mcp_settings" && len(current.Rows) != len(table.Rows) {
				return fmt.Errorf("migration changed row count in %s", table.Name)
			}
			continue
		}
		expected := len(table.Rows)
		if table.Name == "workflow_versions" {
			expected += renames
		}
		if len(current.Rows) != expected {
			return fmt.Errorf("history count mismatch in %s", table.Name)
		}
		set := map[string]bool{}
		for _, row := range current.Rows {
			set[digest(row)] = true
		}
		for _, row := range table.Rows {
			if !set[digest(row)] {
				return fmt.Errorf("history changed in %s", table.Name)
			}
		}
	}
	return nil
}

func Verify(db *gorm.DB, plan Plan, backupDir string) error {
	var journal Journal
	if err := readJSON(filepath.Join(backupDir, "journal.json"), &journal); err != nil {
		return err
	}
	if journal.Version != Version || journal.PlanDigest != digest(plan) || journal.Stage != "complete" {
		return fmt.Errorf("migration is not complete")
	}
	current, err := BuildPlan(db, plan.Options)
	if err != nil {
		return err
	}
	if current.Fingerprint != journal.ResultFingerprint {
		return fmt.Errorf("workspace changed since migration; offline verification/rollback must not overwrite newer writes")
	}
	if len(current.Blockers) > 0 {
		return fmt.Errorf("migrated workspace still has preflight blockers")
	}
	var projects []models.Project
	if err := db.Find(&projects).Error; err != nil {
		return err
	}
	if len(projects) != 1 || projects[0].ID != plan.Options.CanonicalID {
		return fmt.Errorf("workspace singleton verification failed")
	}
	original, err := readBackup(plan, backupDir)
	if err != nil {
		return err
	}
	after, err := capture(db)
	if err != nil {
		return err
	}
	return verifyHistory(original, after, len(plan.WorkflowRenames))
}

// Rollback is deliberately offline and rejects intervening writes. Schema
// additions remain nullable; the source application can read its original rows.
func Rollback(db *gorm.DB, plan Plan, backupDir string, offline bool) error {
	if !offline {
		return fmt.Errorf("offline acknowledgement is required")
	}
	backupDir, err := validateBackup(plan, backupDir)
	if err != nil {
		return err
	}
	var journal Journal
	if err := readJSON(filepath.Join(backupDir, "journal.json"), &journal); err != nil {
		return err
	}
	if journal.PlanDigest != digest(plan) || journal.Version != Version {
		return ErrStalePlan
	}
	if journal.Stage == "rolled-back" {
		return nil
	}
	current, err := capture(db)
	if err != nil {
		return err
	}
	original, err := readBackup(plan, backupDir)
	if err != nil {
		return err
	}
	actual := digest(current)
	if actual != digest(original) && actual != journal.SchemaDatabase && actual != journal.AfterDatabase && !originalDataUnchanged(original, current) {
		return fmt.Errorf("database has unrelated writes; automatic rollback refused")
	}
	for _, item := range []struct{ kind, root, after string }{{"profiles", plan.Options.ProfilesRoot, journal.AfterProfiles}, {"shared", plan.Options.SharedRoot, journal.AfterShared}} {
		files, err := scanFiles(item.root)
		if err != nil {
			return err
		}
		backup, err := scanFiles(filepath.Join(backupDir, item.kind))
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(files, backup) && digest(files) != item.after {
			if _, err := os.Stat(item.root); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("profile files have unrelated writes; rollback refused")
			}
		}
		if journal.FilesStarted {
			for _, retained := range []struct{ path, expected string }{{heldRoot(plan, item.kind), digest(backup)}, {stageRoot(plan, item.kind), item.after}} {
				if _, err := os.Lstat(retained.path); errors.Is(err, os.ErrNotExist) {
					continue
				} else if err != nil {
					return err
				}
				files, err := scanFiles(retained.path)
				if err != nil {
					return err
				}
				if retained.expected != "" && digest(files) != retained.expected {
					return fmt.Errorf("migration staging files changed; automatic rollback refused")
				}
			}
		}
	}
	if err := restore(db, original); err != nil {
		return err
	}
	for _, item := range []struct{ kind, root string }{{"profiles", plan.Options.ProfilesRoot}, {"shared", plan.Options.SharedRoot}} {
		if err := os.RemoveAll(item.root); err != nil {
			return err
		}
		if err := copyTree(filepath.Join(backupDir, item.kind), item.root); err != nil {
			return err
		}
		if journal.FilesStarted {
			for _, path := range []string{stageRoot(plan, item.kind), heldRoot(plan, item.kind)} {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
			}
		}
	}
	journal.Stage = "rolled-back"
	return writeJSON(filepath.Join(backupDir, "journal.json"), journal)
}
