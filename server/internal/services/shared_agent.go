package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// SharedAgentConfig is the project-level Agent baseline used as the extend layer
// at startup (workflow Run + project-context chat test). Shape mirrors Agent
// without a Name; identity is ProjectID. Empty config is valid.
type SharedAgentConfig struct {
	ProjectID         string            `json:"projectId"`
	AcpBackend        string            `json:"acpBackend,omitempty"`
	DefaultProjectID  string            `json:"defaultProjectId,omitempty"`
	GitCredentialType string            `json:"gitCredentialType,omitempty"`
	GitSshKnownHosts  string            `json:"gitSshKnownHosts,omitempty"`
	GitSshPrivateKey  string            `json:"gitSshPrivateKey,omitempty"`
	Files             []AgentFile       `json:"files"`
	MCP               []MCPServer       `json:"mcp"`
	Env               map[string]string `json:"env"`
	Layout            AgentLayout       `json:"layout"`
}

// sharedAgentDisk mirrors agent.json under data/project-shared/<projectId>/.
type sharedAgentDisk struct {
	AcpBackend        string            `json:"acpBackend,omitempty"`
	DefaultProjectID  string            `json:"defaultProjectId,omitempty"`
	GitCredentialType string            `json:"gitCredentialType,omitempty"`
	GitSshKnownHosts  string            `json:"gitSshKnownHosts,omitempty"`
	GitSshPrivateKey  string            `json:"gitSshPrivateKey,omitempty"`
	MCP               []MCPServer       `json:"mcp,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	Layout            *AgentLayout      `json:"layout,omitempty"`
}

// SharedAgentService persists per-project shared Agent baselines on disk:
//
//	<root>/<projectId>/workspace/**  -- shared working-dir tree
//	<root>/<projectId>/agent.json    -- mcp / env / layout / meta
type SharedAgentService struct {
	root string
	mu   sync.Mutex
}

// DefaultSharedAgentRoot derives the shared-config root next to profiles
// (data/profiles → data/project-shared).
func DefaultSharedAgentRoot(profilesRoot string) string {
	profilesRoot = strings.TrimSpace(profilesRoot)
	if profilesRoot == "" {
		profilesRoot = "data/profiles"
	}
	return filepath.Join(filepath.Dir(profilesRoot), "project-shared")
}

// NewSharedAgentService builds the service. root empty → DefaultSharedAgentRoot("").
func NewSharedAgentService(root string) *SharedAgentService {
	if strings.TrimSpace(root) == "" {
		root = DefaultSharedAgentRoot("")
	}
	_ = os.MkdirAll(root, 0o755)
	return &SharedAgentService{root: root}
}

// Root returns the on-disk root.
func (s *SharedAgentService) Root() string { return s.root }

// Get returns the shared config for a project. Missing dir → empty valid config.
func (s *SharedAgentService) Get(projectID string) SharedAgentConfig {
	pid := sanitizeProjectID(projectID)
	out := SharedAgentConfig{
		ProjectID: strings.TrimSpace(projectID),
		Env:       map[string]string{},
		Files:     []AgentFile{},
		MCP:       []MCPServer{},
	}
	if pid == "" {
		return out
	}
	dir := filepath.Join(s.root, pid)
	if _, err := os.Stat(dir); err != nil {
		out.Layout = AgentLayout{}.withDefaults()
		return out
	}
	cfg := s.readConfig(pid)
	layout := AgentLayout{}
	if cfg.Layout != nil {
		layout = *cfg.Layout
	}
	backend := strings.TrimSpace(cfg.AcpBackend)
	if backend != "" {
		backend = NormalizeAcpBackend(backend)
	}
	layout = layout.withDefaults()
	if strings.TrimSpace(layout.ConfigRoot) == DefaultConfigRoot && backend != "" && backend != AcpBackendCursor {
		layout.ConfigRoot = DefaultConfigRootForBackend(backend)
	}
	env := cfg.Env
	if env == nil {
		env = map[string]string{}
	}
	return SharedAgentConfig{
		ProjectID:         strings.TrimSpace(projectID),
		AcpBackend:        backend,
		DefaultProjectID:  strings.TrimSpace(cfg.DefaultProjectID),
		GitCredentialType: normalizeGitCredentialType(cfg.GitCredentialType),
		GitSshKnownHosts:  cfg.GitSshKnownHosts,
		GitSshPrivateKey:  cfg.GitSshPrivateKey,
		Files:             s.readFiles(pid),
		MCP:               cfg.MCP,
		Env:               env,
		Layout:            layout,
	}
}

// Save writes the shared config for a project (full replace of workspace + agent.json).
func (s *SharedAgentService) Save(cfg SharedAgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid := sanitizeProjectID(cfg.ProjectID)
	if pid == "" {
		return fmt.Errorf("invalid project id")
	}
	if err := ValidateAgentSSHMeta(cfg.GitSshKnownHosts, cfg.GitSshPrivateKey); err != nil {
		return err
	}
	StripSSHEnvKeys(cfg.Env)
	dir := filepath.Join(s.root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	layout := cfg.Layout.withDefaults()
	backend := strings.TrimSpace(cfg.AcpBackend)
	if backend != "" {
		backend = NormalizeAcpBackend(backend)
		if strings.TrimSpace(cfg.Layout.ConfigRoot) == "" {
			layout.ConfigRoot = DefaultConfigRootForBackend(backend)
		}
	}
	disk := sharedAgentDisk{
		AcpBackend:        backend,
		DefaultProjectID:  strings.TrimSpace(cfg.DefaultProjectID),
		GitCredentialType: normalizeGitCredentialType(cfg.GitCredentialType),
		GitSshKnownHosts:  cfg.GitSshKnownHosts,
		GitSshPrivateKey:  cfg.GitSshPrivateKey,
		MCP:               cfg.MCP,
		Env:               cfg.Env,
		Layout:            &layout,
	}
	b, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), b, 0o644); err != nil {
		return err
	}
	work := filepath.Join(dir, WorkDirName)
	if err := os.RemoveAll(work); err != nil {
		return err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	for _, f := range cfg.Files {
		rel := safeRel(f.Path)
		if rel == "" {
			continue
		}
		full, err := underRoot(work, rel)
		if err != nil {
			return fmt.Errorf("file %q: %w", f.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// WorkDir returns the shared workspace path when it exists.
func (s *SharedAgentService) WorkDir(projectID string) string {
	pid := sanitizeProjectID(projectID)
	if pid == "" {
		return ""
	}
	d := filepath.Join(s.root, pid, WorkDirName)
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return d
	}
	return ""
}

// AsAgent views the shared config as an Agent-shaped value for merge helpers.
func (c SharedAgentConfig) AsAgent() Agent {
	projectID := strings.TrimSpace(c.DefaultProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(c.ProjectID)
	}
	env := c.Env
	if env == nil {
		env = map[string]string{}
	}
	return Agent{
		Name:              "",
		ProjectID:         projectID,
		AcpBackend:        c.AcpBackend,
		GitCredentialType: c.GitCredentialType,
		GitSshKnownHosts:  c.GitSshKnownHosts,
		GitSshPrivateKey:  c.GitSshPrivateKey,
		Files:             c.Files,
		MCP:               c.MCP,
		Env:               env,
		Layout:            c.Layout,
	}
}

// ExtendOverlay merges shared (base) then agent (overlay).
// Non-Token same-key: Agent wins. Token-class keys: shared wins when present;
// if shared lacks the key, Agent stock value is kept so legacy agents keep auth.
// SSH meta literals: Agent wins when non-empty, else shared (same as other meta).
// projectId: only fill when Agent.ProjectID is empty (does not persist back).
func ExtendOverlay(shared SharedAgentConfig, agent Agent) Agent {
	base := shared.AsAgent()
	out := Agent{
		Name:              agent.Name,
		ProjectID:         strings.TrimSpace(agent.ProjectID),
		AcpBackend:        pickNonEmpty(agent.AcpBackend, base.AcpBackend),
		GitCredentialType: pickNonEmpty(agent.GitCredentialType, base.GitCredentialType),
		GitSshKnownHosts:  pickNonEmpty(agent.GitSshKnownHosts, base.GitSshKnownHosts),
		GitSshPrivateKey:  pickNonEmpty(agent.GitSshPrivateKey, base.GitSshPrivateKey),
		Files:             mergeFiles(base.Files, agent.Files),
		MCP:               mergeMCP(base.MCP, agent.MCP),
		Env:               envauth.MergeEnvSharedTokenPriority(base.Env, agent.Env),
		Layout:            mergeLayout(base.Layout, agent.Layout),
	}
	if strings.TrimSpace(out.ProjectID) == "" {
		out.ProjectID = strings.TrimSpace(base.ProjectID)
	}
	if out.AcpBackend != "" {
		out.AcpBackend = NormalizeAcpBackend(out.AcpBackend)
	}
	out.Layout = out.Layout.withDefaults()
	if out.Env == nil {
		out.Env = map[string]string{}
	}
	return out
}

func pickNonEmpty(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func mergeFiles(base, overlay []AgentFile) []AgentFile {
	byPath := map[string]AgentFile{}
	order := make([]string, 0, len(base)+len(overlay))
	add := func(f AgentFile) {
		p := safeRel(f.Path)
		if p == "" {
			return
		}
		if _, ok := byPath[p]; !ok {
			order = append(order, p)
		}
		byPath[p] = AgentFile{Path: p, Content: f.Content}
	}
	for _, f := range base {
		add(f)
	}
	for _, f := range overlay {
		add(f)
	}
	out := make([]AgentFile, 0, len(order))
	for _, p := range order {
		out = append(out, byPath[p])
	}
	return out
}

func mergeMCP(base, overlay []MCPServer) []MCPServer {
	byName := map[string]MCPServer{}
	order := make([]string, 0, len(base)+len(overlay))
	add := func(m MCPServer) {
		n := strings.TrimSpace(m.Name)
		if n == "" {
			return
		}
		m.Name = n
		if _, ok := byName[n]; !ok {
			order = append(order, n)
		}
		byName[n] = m
	}
	for _, m := range base {
		add(m)
	}
	for _, m := range overlay {
		add(m)
	}
	out := make([]MCPServer, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out
}

func mergeLayout(base, overlay AgentLayout) AgentLayout {
	return AgentLayout{
		ConfigRoot:   pickNonEmpty(overlay.ConfigRoot, base.ConfigRoot),
		WorkspaceDir: pickNonEmpty(overlay.WorkspaceDir, base.WorkspaceDir),
	}
}

func (s *SharedAgentService) readConfig(pid string) sharedAgentDisk {
	var cfg sharedAgentDisk
	b, err := os.ReadFile(filepath.Join(s.root, pid, "agent.json"))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(b, &cfg)
	return cfg
}

func (s *SharedAgentService) readFiles(pid string) []AgentFile {
	return readTreeIfDir(filepath.Join(s.root, pid, WorkDirName))
}

func sanitizeProjectID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	// Reuse agent-name sanitizer: strips path separators / unsafe runes.
	return sanitize(id)
}

// MigrateProjectSandboxEnvOnce copies Project.SandboxEnv into shared env
// (same key: shared wins / not overwritten), then clears Project.SandboxEnv
// after a successful round-trip check. Idempotent: projects with empty
// SandboxEnv are no-ops. On failure the project field is left intact.
func MigrateProjectSandboxEnvOnce(db *gorm.DB, projects *ProjectService, shared *SharedAgentService) {
	if db == nil || projects == nil || shared == nil {
		return
	}
	var rows []models.Project
	if err := db.Find(&rows).Error; err != nil {
		log.Warn().Err(err).Msg("shared-agent migrate: list projects failed")
		return
	}
	for _, p := range rows {
		if len(p.SandboxEnv) == 0 {
			continue
		}
		if err := migrateOneProjectSandboxEnv(projects, shared, p); err != nil {
			log.Warn().Err(err).Str("project", p.ID).Msg("shared-agent migrate: project failed; keeping SandboxEnv")
			continue
		}
		log.Info().Str("project", p.ID).Int("keys", len(p.SandboxEnv)).Msg("shared-agent migrate: Project.SandboxEnv → shared env")
	}
}

func migrateOneProjectSandboxEnv(projects *ProjectService, shared *SharedAgentService, p models.Project) error {
	cfg := shared.Get(p.ID)
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	for _, e := range p.SandboxEnv {
		k := strings.TrimSpace(e.Key)
		if k == "" || !e.IsEnabled() {
			continue
		}
		if _, exists := cfg.Env[k]; exists {
			continue // same-key: shared wins
		}
		cfg.Env[k] = e.Value
	}
	cfg.ProjectID = p.ID
	if err := shared.Save(cfg); err != nil {
		return err
	}
	// Validate: every enabled migrated key is present in shared (or was already there).
	got := shared.Get(p.ID)
	for _, e := range p.SandboxEnv {
		k := strings.TrimSpace(e.Key)
		if k == "" || !e.IsEnabled() {
			continue
		}
		if _, ok := got.Env[k]; !ok {
			return fmt.Errorf("migration validation missing key %q", k)
		}
	}
	empty := []models.EnvEntry{}
	_, err := projects.Update(p.ID, nil, nil, &empty, nil, nil, nil)
	return err
}
