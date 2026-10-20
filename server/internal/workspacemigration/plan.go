package workspacemigration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

const Version = 1

type Decisions struct {
	ReauthorizeWorkspaceTools bool              `json:"reauthorizeWorkspaceTools"`
	PauseFormerPMJobs         bool              `json:"pauseFormerPMJobs"`
	RepairOrphans             bool              `json:"repairOrphans"`
	WorkflowNames             map[string]string `json:"workflowNames,omitempty"`
	MemoryTitles              map[string]string `json:"memoryTitles,omitempty"`
	CredentialNames           map[string]string `json:"credentialNames,omitempty"`
}

type Options struct {
	CanonicalID  string    `json:"canonicalId"`
	ProfilesRoot string    `json:"profilesRoot"`
	SharedRoot   string    `json:"sharedRoot"`
	Decisions    Decisions `json:"decisions"`
}

type Issue struct {
	Code     string `json:"code"`
	Resource string `json:"resource,omitempty"`
	Message  string `json:"message"`
}

type ProjectSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Plan contains no configuration values, secrets, message bodies, or graphs.
// Its fingerprint covers those values without exposing them in the report.
type Plan struct {
	Version           int               `json:"version"`
	Options           Options           `json:"options"`
	Fingerprint       string            `json:"fingerprint"`
	Projects          []ProjectSummary  `json:"projects"`
	Counts            map[string]int    `json:"counts"`
	WorkflowRenames   map[string]string `json:"workflowRenames"`
	MemoryRenames     map[string]string `json:"memoryRenames"`
	CredentialRenames map[string]string `json:"credentialRenames"`
	PauseJobIDs       []string          `json:"pauseJobIds"`
	RevokeMCPKeyIDs   []string          `json:"revokeMcpKeyIds"`
	Blockers          []Issue           `json:"blockers"`
	Warnings          []Issue           `json:"warnings"`
}

type sourceState struct {
	snapshot  Snapshot
	projects  []models.Project
	flows     []models.WorkflowDef
	creds     []models.ProjectCredential
	memories  []models.ProjectMemoryItem
	jobs      []models.AgentCronJob
	channels  []models.ChannelConfig
	keys      []models.ProjectMcpApiKey
	runs      []models.Run
	boxes     []models.Sandbox
	agents    []services.Agent
	profiles  []FileDigest
	shared    []FileDigest
	sharedIDs []string
	issues    []Issue
	turns     []models.ChatTurnDraft
	cronRuns  []models.AgentCronRun
}

func load(db *gorm.DB, opts Options) (sourceState, error) {
	var s sourceState
	var err error
	s.snapshot, err = capture(db)
	if err != nil {
		return s, err
	}
	for _, entry := range []struct {
		table string
		out   any
	}{
		{"projects", &s.projects}, {"workflow_defs", &s.flows},
		{"project_credentials", &s.creds}, {"project_memory_items", &s.memories},
		{"agent_cron_jobs", &s.jobs}, {"channel_configs", &s.channels},
		{"project_mcp_api_keys", &s.keys}, {"runs", &s.runs}, {"sandboxes", &s.boxes},
		{"chat_turn_drafts", &s.turns}, {"agent_cron_runs", &s.cronRuns},
	} {
		if db.Migrator().HasTable(entry.table) {
			if err := db.Table(entry.table).Find(entry.out).Error; err != nil {
				return s, err
			}
		}
	}
	sort.Slice(s.projects, func(i, j int) bool { return s.projects[i].ID < s.projects[j].ID })
	sort.Slice(s.flows, func(i, j int) bool { return s.flows[i].ID < s.flows[j].ID })
	sort.Slice(s.memories, func(i, j int) bool { return s.memories[i].ID < s.memories[j].ID })
	sort.Slice(s.creds, func(i, j int) bool { return s.creds[i].ID < s.creds[j].ID })
	s.profiles, err = scanFiles(opts.ProfilesRoot)
	if err != nil {
		return s, err
	}
	s.shared, err = scanFiles(opts.SharedRoot)
	if err != nil {
		return s, err
	}
	sharedEntries, err := os.ReadDir(opts.SharedRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return s, err
	}
	for _, entry := range sharedEntries {
		if entry.IsDir() {
			s.sharedIDs = append(s.sharedIDs, entry.Name())
		}
	}
	if opts.ProfilesRoot != "" {
		entries, err := os.ReadDir(opts.ProfilesRoot)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return s, err
		}
		svc := services.NewAgentService(opts.ProfilesRoot)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(opts.ProfilesRoot, entry.Name(), "agent.json")
			data, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				s.issues = append(s.issues, Issue{Code: "invalid-agent-profile", Resource: entry.Name(), Message: "Every profile directory must contain a valid agent.json. Repair it or archive the directory outside the runtime root."})
				continue
			}
			if err != nil {
				return s, err
			}
			var config map[string]json.RawMessage
			if err := json.Unmarshal(data, &config); err != nil {
				s.issues = append(s.issues, Issue{Code: "invalid-agent-profile", Resource: entry.Name(), Message: "Repair malformed agent.json before migrating."})
				continue
			}
			agent, ok := svc.Get(entry.Name())
			if !ok {
				return s, fmt.Errorf("cannot load Agent: %s", entry.Name())
			}
			s.agents = append(s.agents, agent)
		}
	}
	return s, nil
}

func jsonBytes(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func BuildPlan(db *gorm.DB, opts Options) (Plan, error) {
	var err error
	if opts.ProfilesRoot != "" {
		if info, statErr := os.Lstat(filepath.Clean(opts.ProfilesRoot)); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return Plan{}, fmt.Errorf("profiles root is a symlink; align the server profiles path and shared sibling explicitly before migration")
		}
		opts.ProfilesRoot, err = CanonicalPath(opts.ProfilesRoot)
		if err != nil {
			return Plan{}, err
		}
	}
	if opts.SharedRoot == "" && opts.ProfilesRoot != "" {
		opts.SharedRoot = filepath.Join(filepath.Dir(opts.ProfilesRoot), "project-shared")
	}
	if opts.SharedRoot != "" {
		opts.SharedRoot, err = CanonicalPath(opts.SharedRoot)
		if err != nil {
			return Plan{}, err
		}
	}
	s, err := load(db, opts)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{
		Version: Version, Options: opts, Counts: map[string]int{},
		Projects: []ProjectSummary{}, WorkflowRenames: map[string]string{}, MemoryRenames: map[string]string{},
		CredentialRenames: map[string]string{},
		PauseJobIDs:       []string{}, RevokeMCPKeyIDs: []string{}, Blockers: []Issue{}, Warnings: []Issue{},
	}
	for _, table := range s.snapshot.Tables {
		p.Counts[table.Name] = len(table.Rows)
	}
	p.Blockers = append(p.Blockers, s.issues...)
	for _, project := range s.projects {
		p.Projects = append(p.Projects, ProjectSummary{ID: project.ID, Name: project.Name})
	}
	if len(s.projects) == 1 && p.Options.CanonicalID == "" {
		p.Options.CanonicalID = s.projects[0].ID
	}
	if len(s.projects) == 0 && p.Options.CanonicalID == "" {
		p.Options.CanonicalID = models.DefaultProjectID
	}
	projects := map[string]models.Project{}
	for _, project := range s.projects {
		projects[project.ID] = project
	}
	for _, sharedID := range s.sharedIDs {
		if _, ok := projects[sharedID]; !ok && !opts.Decisions.RepairOrphans {
			p.Blockers = append(p.Blockers, Issue{Code: "orphan-shared", Resource: sharedID, Message: "Confirm orphan repair after reviewing this shared baseline. Unused baselines will be retained only in the migration backup."})
		}
	}
	canonical, found := projects[p.Options.CanonicalID]
	if len(s.projects) > 0 && !found {
		p.Blockers = append(p.Blockers, Issue{Code: "choose-workspace", Message: "Select an existing workspace ID explicitly; no source is selected by order."})
	}
	if !safeName(p.Options.CanonicalID) {
		p.Blockers = append(p.Blockers, Issue{Code: "invalid-workspace-id", Message: "Workspace ID must be a safe directory name."})
	}
	if opts.ProfilesRoot == "" {
		p.Blockers = append(p.Blockers, Issue{Code: "profiles-required", Message: "Provide the actual Agent profiles directory, including when it is empty."})
	} else if containsPath(opts.ProfilesRoot, opts.SharedRoot) || containsPath(opts.SharedRoot, opts.ProfilesRoot) {
		p.Blockers = append(p.Blockers, Issue{Code: "overlapping-files", Message: "Profiles and shared directories must be separate trees."})
	}
	expectedShared, err := CanonicalPath(services.DefaultSharedAgentRoot(opts.ProfilesRoot))
	if err != nil {
		return p, err
	}
	if opts.ProfilesRoot != "" && opts.SharedRoot != expectedShared {
		p.Blockers = append(p.Blockers, Issue{Code: "shared-runtime-path", Message: "Shared root must match the server's profiles sibling project-shared directory; relocate it before planning."})
	}
	for _, run := range s.runs {
		if run.Status == "running" || run.Status == "cancelling" {
			p.Blockers = append(p.Blockers, Issue{Code: "active-run", Resource: run.ID, Message: "Reach an execution safe point before offline migration."})
		}
	}
	scopeChanges := len(s.projects) != 1
	for _, flow := range s.flows {
		scopeChanges = scopeChanges || flow.ProjectID != p.Options.CanonicalID
	}
	for _, agent := range s.agents {
		scopeChanges = scopeChanges || agent.ProjectID != p.Options.CanonicalID
	}
	for _, box := range s.boxes {
		live := box.Status == "running" || box.Status == "creating" || box.Status == "pulling"
		if box.Purpose == "run" && live && (scopeChanges || box.ProjectID != "" && box.ProjectID != p.Options.CanonicalID) {
			p.Blockers = append(p.Blockers, Issue{Code: "live-workflow-sandbox", Resource: box.Name, Message: "Preserve the workflow sandbox working tree, logs, configuration and precise login write-back, then retire it on the source deployment. Reattach does not refresh existing MCP, environment or credentials."})
		}
		if box.Purpose != "run" && (box.Status == "running" || box.Status == "creating" || box.Status == "pulling") {
			p.Blockers = append(p.Blockers, Issue{Code: "active-session", Resource: box.Name, Message: "Drain and retire the session, including precise login write-back and outbound delivery, before stopping the old server."})
		}
	}
	for _, turn := range s.turns {
		if turn.Status == "streaming" {
			p.Blockers = append(p.Blockers, Issue{Code: "active-turn", Resource: turn.ThreadID, Message: "Complete or recover the persisted streaming turn on the source deployment before migration."})
		}
	}
	for _, run := range s.cronRuns {
		if run.Status == "running" {
			p.Blockers = append(p.Blockers, Issue{Code: "active-cron-run", Resource: run.ID, Message: "Complete or recover the persisted scheduler execution before migration."})
		}
	}
	for _, job := range s.jobs {
		if job.ClaimedAt != nil {
			p.Blockers = append(p.Blockers, Issue{Code: "claimed-job", Resource: job.ID, Message: "Resolve the scheduler claim before migration; claims are not cleared automatically."})
		}
		old := projects[job.ProjectID]
		if len(s.projects) > 1 && job.Enabled && old.PmLeaderAgent == job.AgentName && canonical.PmLeaderAgent != job.AgentName {
			if opts.Decisions.PauseFormerPMJobs {
				p.PauseJobIDs = append(p.PauseJobIDs, job.ID)
			} else {
				p.Blockers = append(p.Blockers, Issue{Code: "former-pm-job", Resource: job.ID, Message: "Reconfigure this former PM task or explicitly pause it; its privileges will not silently change."})
			}
		}
	}
	if len(s.projects) > 1 {
		privileged := len(s.channels) > 0
		for _, old := range s.projects {
			privileged = privileged || old.PmLeaderEnabled
		}
		if privileged && !opts.Decisions.ReauthorizeWorkspaceTools {
			p.Blockers = append(p.Blockers, Issue{Code: "review-tool-authority", Message: "Existing PM and channel tools would gain workspace-wide access. Explicitly reauthorize that access or disable those capabilities before planning."})
		}
		for _, key := range s.keys {
			if key.RevokedAt == nil {
				p.RevokeMCPKeyIDs = append(p.RevokeMCPKeyIDs, key.ID)
			}
		}
		p.Warnings = append(p.Warnings, Issue{Code: "external-clients", Message: "All project-wide MCP keys are revoked on merge. Configure clients with the new workspace address and newly issued keys; workflow-scoped /v1 keys are retained."})
		if found && !canonical.NotifyPolicy.IsEnabled() {
			for _, old := range s.projects {
				if old.ID != canonical.ID && old.NotifyPolicy.IsEnabled() && len(old.NotifyPolicy.ChannelIDs) > 0 {
					p.Warnings = append(p.Warnings, Issue{Code: "workspace-notify-disabled", Resource: old.ID, Message: "The retained workspace has run notifications switched off, which also silences this source's converted workflow notifications. Enable the workspace switch after migration if they should still be sent."})
				}
			}
		}
	}
	workflowKey, err := nameKey(db, "workflow_defs", "name")
	if err != nil {
		return p, err
	}
	memoryKey, err := nameKey(db, "project_memory_items", "title")
	if err != nil {
		return p, err
	}
	// The retained workspace reserves names first so its users keep theirs.
	retainedFirst := func(projectID string) int {
		if projectID == p.Options.CanonicalID {
			return 0
		}
		return 1
	}
	flows := append([]models.WorkflowDef(nil), s.flows...)
	sort.SliceStable(flows, func(i, j int) bool { return retainedFirst(flows[i].ProjectID) < retainedFirst(flows[j].ProjectID) })
	memories := append([]models.ProjectMemoryItem(nil), s.memories...)
	sort.SliceStable(memories, func(i, j int) bool {
		return retainedFirst(memories[i].ProjectID) < retainedFirst(memories[j].ProjectID)
	})
	creds := append([]models.ProjectCredential(nil), s.creds...)
	sort.SliceStable(creds, func(i, j int) bool { return retainedFirst(creds[i].ProjectID) < retainedFirst(creds[j].ProjectID) })
	seenNames := map[string]bool{}
	for _, flow := range flows {
		name := flow.Name
		if chosen, ok := opts.Decisions.WorkflowNames[flow.ID]; ok {
			name = strings.TrimSpace(chosen)
		}
		if db.Dialector.Name() == "mysql" && utf8.RuneCountInString(name) > 191 {
			p.Blockers = append(p.Blockers, Issue{Code: "workflow-name-too-long", Resource: flow.ID, Message: "Choose a workflow name of at most 191 characters for the MySQL unique index."})
		}
		if name == "" {
			p.Blockers = append(p.Blockers, Issue{Code: "empty-workflow-name", Resource: flow.ID, Message: "Choose a non-empty workflow name."})
		}
		name, err = reserveName(name, flow.ID, seenNames, workflowKey)
		if err != nil {
			return p, err
		}
		if name != flow.Name {
			p.WorkflowRenames[flow.ID] = name
		}
		if _, ok := projects[flow.ProjectID]; !ok && !opts.Decisions.RepairOrphans {
			p.Blockers = append(p.Blockers, Issue{Code: "orphan-workflow", Resource: flow.ID, Message: "Confirm orphan repair after checking the missing source configuration."})
		}
	}
	seenMemory := map[string]bool{}
	for _, memory := range memories {
		title := memory.Title
		if chosen, ok := opts.Decisions.MemoryTitles[memory.ID]; ok {
			title = strings.TrimSpace(chosen)
		}
		if db.Dialector.Name() == "mysql" && utf8.RuneCountInString(title) > 191 {
			p.Blockers = append(p.Blockers, Issue{Code: "memory-title-too-long", Resource: memory.ID, Message: "Choose a memory title of at most 191 characters for the MySQL unique index."})
		}
		if title == "" {
			p.Blockers = append(p.Blockers, Issue{Code: "empty-memory-title", Resource: memory.ID, Message: "Choose a non-empty memory title."})
		}
		key := func(value string) (string, error) {
			normalized, err := memoryKey(value)
			if err != nil {
				return "", err
			}
			agent, err := memoryKey(memory.AgentName)
			return agent + "\x00" + normalized, err
		}
		title, err = reserveName(title, memory.ID, seenMemory, key)
		if err != nil {
			return p, err
		}
		if title != memory.Title {
			p.MemoryRenames[memory.ID] = title
		}
	}
	for _, agent := range s.agents {
		if _, ok := projects[agent.ProjectID]; !ok && !opts.Decisions.RepairOrphans {
			p.Blockers = append(p.Blockers, Issue{Code: "orphan-agent", Resource: agent.Name, Message: "Confirm repair of this Agent's missing workspace before conversion."})
		}
	}
	credentialEnvKey, err := nameKey(db, "project_credentials", "env_key")
	if err != nil {
		return p, err
	}
	aliases, err := futureBuiltinAliases(s.creds, p.Options.CanonicalID, credentialEnvKey)
	if err != nil {
		return p, err
	}
	for _, credential := range creds {
		name := credential.Name
		if chosen, ok := opts.Decisions.CredentialNames[credential.ID]; ok {
			name = strings.TrimSpace(chosen)
		}
		choice := credential
		choice.Name = name
		name = migratedCredentialName(choice)
		if name == "" && strings.TrimSpace(credential.EnvKey) != "" {
			p.Blockers = append(p.Blockers, Issue{Code: "empty-credential-name", Resource: credential.ID, Message: "Choose a non-empty credential display name."})
		}
		if strings.TrimSpace(credential.EnvKey) != "" {
			envKey, err := credentialEnvKey(strings.TrimSpace(credential.EnvKey))
			if err != nil {
				return p, err
			}
			key := func(value string) (string, error) {
				return envKey + "\x00" + strings.ToLower(strings.TrimSpace(value)), nil
			}
			name, err = reserveName(name, credential.ID, aliases, key)
			if err != nil {
				return p, err
			}
		}
		if name != credential.Name {
			p.CredentialRenames[credential.ID] = name
		}
	}
	checkReferences(&p, s)
	for _, table := range s.snapshot.Tables {
		if table.Name == "project_audit_events" || table.Name == "token_usage_events" {
			continue
		}
		column := -1
		for i, col := range table.Columns {
			if col == "project_id" {
				column = i
			}
		}
		if column < 0 {
			continue
		}
		for _, row := range table.Rows {
			v, err := decodeCell(row[column])
			if err != nil {
				return p, err
			}
			id := fmt.Sprint(v)
			if raw, ok := v.([]byte); ok {
				id = string(raw)
			}
			if _, ok := projects[id]; !ok && !opts.Decisions.RepairOrphans {
				p.Blockers = append(p.Blockers, Issue{Code: "orphan-rows", Resource: table.Name, Message: "Missing or empty project attribution requires explicit orphan repair."})
				break
			}
		}
	}
	sort.Strings(p.PauseJobIDs)
	sort.Strings(p.RevokeMCPKeyIDs)
	p.Fingerprint = digest(struct {
		Database Snapshot
		Profiles []FileDigest
		Shared   []FileDigest
	}{s.snapshot, s.profiles, s.shared})
	return p, nil
}

// Use the database column's collation, including accent and trailing-space
// semantics, rather than assuming Go's case folding matches a unique index.
func nameKey(db *gorm.DB, table, column string) (func(string) (string, error), error) {
	if db.Dialector.Name() != "mysql" {
		return func(value string) (string, error) { return value, nil }, nil
	}
	var collation, charset string
	if err := db.Raw("SELECT COLLATION_NAME, CHARACTER_SET_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?", table, column).Row().Scan(&collation, &charset); err != nil {
		return nil, err
	}
	valid := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !valid.MatchString(collation) || !valid.MatchString(charset) {
		return nil, fmt.Errorf("cannot determine name collation for %s", table)
	}
	padding, err := collationPadding(db, collation)
	if err != nil {
		return nil, err
	}
	expression := "CONVERT(? USING " + charset + ")"
	if padding == "PAD SPACE" {
		expression = "RTRIM(" + expression + ")"
	}
	return func(value string) (string, error) {
		var key string
		err := db.Raw("SELECT HEX(WEIGHT_STRING("+expression+" COLLATE "+collation+"))", value).Scan(&key).Error
		return key, err
	}, nil
}

// MariaDB has no COLLATIONS.PAD_ATTRIBUTE; its NO PAD collations are the ones
// named *_nopad_*, every other collation pads trailing spaces.
func collationPadding(db *gorm.DB, collation string) (string, error) {
	var columns int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = 'information_schema' AND TABLE_NAME = 'COLLATIONS' AND COLUMN_NAME = 'PAD_ATTRIBUTE'").Scan(&columns).Error; err != nil {
		return "", err
	}
	if columns == 0 {
		return mariaDBPadding(collation), nil
	}
	var padding string
	err := db.Raw("SELECT PAD_ATTRIBUTE FROM information_schema.COLLATIONS WHERE COLLATION_NAME = ?", collation).Scan(&padding).Error
	return padding, err
}

func mariaDBPadding(collation string) string {
	if strings.Contains(strings.ToLower(collation), "_nopad_") {
		return "NO PAD"
	}
	return "PAD SPACE"
}

func reserveName(name, id string, used map[string]bool, key func(string) (string, error)) (string, error) {
	prefix := []rune(name)
	maxPrefix := 180 - utf8.RuneCountInString(id)
	if maxPrefix < 1 {
		maxPrefix = 1
	}
	if len(prefix) > maxPrefix {
		prefix = prefix[:maxPrefix]
	}
	base := string(prefix) + " · " + id
	for i := 1; ; i++ {
		normalized, err := key(name)
		if err != nil {
			return "", err
		}
		if !used[normalized] {
			used[normalized] = true
			return name, nil
		}
		name = base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
	}
}

func gatePolicy(p models.Project) models.WorkflowGateAutoPolicy {
	return models.WorkflowGateAutoPolicy{Enabled: p.PmLeaderEnabled, AgentName: p.PmLeaderAgent, Variable: p.PmGateAutoVar, Prompt: p.PmGateAutoPrompt, EnabledMcps: p.PmEnabledMcps}
}

func checkReferences(plan *Plan, s sourceState) {
	byID := map[string]models.ProjectCredential{}
	for _, credential := range s.creds {
		byID[credential.ID] = credential
	}
	agents := map[string]services.Agent{}
	shared := services.NewSharedAgentService(plan.Options.SharedRoot)
	for _, agent := range s.agents {
		agents[agent.Name] = agent
		base := shared.Get(agent.ProjectID)
		refs := []string{agent.AiCredentialID, agent.OpenCodeCredentialID, agent.GitCredentialID, agent.SshHostsCredentialID, base.AiCredentialID, base.OpenCodeCredentialID}
		for _, ref := range agent.CredentialBindings {
			if ref != nil {
				refs = append(refs, *ref)
			}
		}
		for _, ref := range refs {
			if c, ok := byID[ref]; ref != "" && ok && c.ProjectID != agent.ProjectID {
				plan.Blockers = append(plan.Blockers, Issue{Code: "cross-source-credential", Resource: agent.Name, Message: "An invalid cross-source credential must not become valid through merging."})
			}
		}
		for _, config := range []any{agent.MCP, agent.Env, base.MCP, base.Env} {
			data, _ := json.Marshal(config)
			for id, credential := range byID {
				if credential.ProjectID != agent.ProjectID && strings.Contains(string(data), "credential:"+id) {
					plan.Blockers = append(plan.Blockers, Issue{Code: "cross-source-mcp-secret", Resource: agent.Name, Message: "Resolve a cross-source MCP credential reference before merging."})
				}
			}
			if strings.Contains(string(data), "/mcp/external/") || strings.Contains(string(data), "/mcp/pm/") || strings.Contains(string(data), "/mcp/memory-store/") || strings.Contains(string(data), "/mcp/context-store/") {
				plan.Blockers = append(plan.Blockers, Issue{Code: "embedded-project-address", Resource: agent.Name, Message: "Update the explicitly configured project MCP address to the workspace endpoint."})
			}
		}
	}
	flows := map[string]models.WorkflowDef{}
	for _, flow := range s.flows {
		flows[flow.ID] = flow
	}
	for _, run := range s.runs {
		if run.Status == "completed" {
			continue
		}
		flow, ok := flows[run.WorkflowID]
		if !ok {
			plan.Warnings = append(plan.Warnings, Issue{Code: "deleted-workflow-run", Resource: run.ID, Message: "History is retained; execution cannot be reconstructed from a deleted workflow."})
			continue
		}
		for _, node := range run.Graph.Nodes {
			if agent, ok := agents[models.AgentProfile(node.Config)]; ok && agent.ProjectID != flow.ProjectID {
				plan.Blockers = append(plan.Blockers, Issue{Code: "resume-config-difference", Resource: run.ID, Message: "This run uses an Agent outside its source. Resolve the effective configuration before merging."})
			}
		}
	}
}
