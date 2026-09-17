package services

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// AgentService manages user-defined Agents on the filesystem. Each Agent owns a
// single working directory (a file tree) plus its MCP servers and env vars:
//
//	<root>/<agent>/workspace/<path...>   -- the agent's working dir (file tree)
//	<root>/<agent>/agent.json         -- MCP servers + environment vars
//
// Workflow nodes reference an agent by name via agent_profile. At run time the
// runtime copies the whole workspace/ tree into the sandbox config root (rules,
// skills, AGENTS.md, scripts, …), layers the platform base rules + the resolved
// mcp.json on top, and injects the env vars. Files the agent authors are loaded
// natively by the in-container ACP agent.
type AgentService struct {
	root string
	mu   sync.Mutex
	Vcs  *WorkspaceVcsService
}

// NewAgentService builds the service. It ships no preset agents (users create
// their own).
func NewAgentService(root string) *AgentService {
	s := &AgentService{root: root, Vcs: NewWorkspaceVcsService(root)}
	return s
}

// ArtifactStoreMCP is the conventional name for the platform's run-scoped
// artifact-store. The whole MCP config is user-authored; an Agent wires the
// artifact-store by referencing the run-scoped template vars
// (${GRASP_ARTIFACT_URL} / ${GRASP_ARTIFACT_TOKEN}) in its url/headers.
const ArtifactStoreMCP = "artifact-store"

// WorkDirName is the subfolder under each agent that holds its working-dir tree.
const WorkDirName = "workspace"

// MCPServer describes one MCP server an agent can talk to. A server is either
// URL-based (streamable HTTP, optional headers) or command-based (stdio).
type MCPServer struct {
	Name    string            `json:"name"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// AgentFile is one file in the agent's working directory (relative path +
// content). Paths use forward slashes and may be nested (e.g. rules/identity.md,
// skills/gitlab/SKILL.md).
type AgentFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Default sandbox-injection layout (sandbox protocol convention). Used when an
// Agent does not pin its own ConfigRoot / WorkspaceDir.
const (
	DefaultConfigRoot   = "/root/.cursor"
	DefaultWorkspaceDir = "/root/workspace"
)

// AgentLayout is the sandbox-injection layout for an Agent: where, inside the
// sandbox container, the platform mounts this Agent's config and clones the
// repo. Persisted per-agent (agent.json) and consumed at sandbox creation —
// the executor drives the bind-mount target and WORKSPACE_DIR from these,
// instead of a hardcoded path. mcp.json / rules/ / skills/ live under
// ConfigRoot as protocol-fixed sub-paths (derived, not stored).
type AgentLayout struct {
	// ConfigRoot is the container path the Agent's working dir (rules/, skills/,
	// mcp.json) is RW-mounted at. Empty → DefaultConfigRoot.
	ConfigRoot string `json:"configRoot,omitempty"`
	// WorkspaceDir is the container path the repo is cloned into / code runs in
	// (WORKSPACE_DIR). Empty → DefaultWorkspaceDir.
	WorkspaceDir string `json:"workspaceDir,omitempty"`
}

// withDefaults returns the layout with empty fields filled by protocol defaults.
func (l AgentLayout) withDefaults() AgentLayout {
	if strings.TrimSpace(l.ConfigRoot) == "" {
		l.ConfigRoot = DefaultConfigRoot
	}
	if strings.TrimSpace(l.WorkspaceDir) == "" {
		l.WorkspaceDir = DefaultWorkspaceDir
	}
	return l
}

// AcpBackend values bind a agent_profile to a sandbox ACP bridge.
const (
	AcpBackendCursor     = "cursor"
	AcpBackendClaudeCode = "claude_code"
	AcpBackendCodeBuddy  = "codebuddy"
	AcpBackendTrae       = "trae"
	AcpBackendOpenCode   = "opencode"
	AcpBackendCodex      = "codex"
)

// NormalizeAcpBackend coerces unknown/empty values to cursor.
func NormalizeAcpBackend(raw string) string {
	switch strings.TrimSpace(raw) {
	case AcpBackendCursor, AcpBackendClaudeCode, AcpBackendCodeBuddy, AcpBackendTrae, AcpBackendOpenCode, AcpBackendCodex:
		return strings.TrimSpace(raw)
	default:
		return AcpBackendCursor
	}
}

// Allowed Agent-level git credential contracts (Studio UI metadata).
const (
	GitCredentialGitHubHTTPS = "github_https"
	GitCredentialGitLabHTTPS = "gitlab_https"
	GitCredentialSSH         = "ssh"
)

// normalizeGitCredentialType keeps only known values; unknown input is cleared
// so dirty agent.json / ZIP imports cannot leak misleading credential checks.
func normalizeGitCredentialType(raw string) string {
	v := strings.TrimSpace(raw)
	switch v {
	case "", GitCredentialGitHubHTTPS, GitCredentialGitLabHTTPS, GitCredentialSSH:
		return v
	default:
		log.Warn().Str("gitCredentialType", v).Msg("clearing invalid agent gitCredentialType")
		return ""
	}
}

// DefaultConfigRootForBackend returns the protocol default config root.
func DefaultConfigRootForBackend(backend string) string {
	switch NormalizeAcpBackend(backend) {
	case AcpBackendCodex:
		return "/root/.codex"
	case AcpBackendClaudeCode:
		return "/root/.claude"
	case AcpBackendCodeBuddy:
		return "/root/.codebuddy"
	case AcpBackendTrae:
		return "/root/.trae"
	case AcpBackendOpenCode:
		return "/root/.config/opencode"
	default:
		return DefaultConfigRoot
	}
}

// Agent is a reusable, user-defined Agent identity: a working directory (file
// tree) plus the MCP servers and environment variables it runs with.
type Agent struct {
	Name string `json:"name"`
	// ProjectID is the Agent's single home project. Empty means unbound. When
	// unbound the Agent may only use the run-scoped artifact-store; the
	// project-scoped platform MCPs (memory-store / context-store /
	// task-scheduler) are rejected at save time and never injected at runtime.
	// Switching or clearing this field purges the Agent's data under the old
	// project (see PmService.PurgeAgentProjectData).
	ProjectID string `json:"projectId,omitempty"`
	// AcpBackend selects the ACP bridge (cursor | claude_code | codebuddy | trae | opencode).
	// Empty defaults to cursor for backward compatibility.
	AcpBackend string `json:"acpBackend,omitempty"`
	// GitCredentialType is the Agent-level credential contract selected in Studio.
	// Runtime writes GitHub/GitLab/SSH by "configured → write"; this field is UI hint only.
	GitCredentialType string `json:"gitCredentialType,omitempty"`
	// GitSshKnownHosts is known_hosts literal text (may contain newlines). Stored as
	// meta, never ${vars.*}; injected as a file before git clone.
	GitSshKnownHosts string `json:"gitSshKnownHosts,omitempty"`
	// GitSshPrivateKey is the SSH private key literal. Stored as meta, never
	// ${vars.*}; injected as ~/.ssh/id_rsa (600) before git clone.
	GitSshPrivateKey string `json:"gitSshPrivateKey,omitempty"`
	// Files is the agent's working directory, copied into ConfigRoot at run.
	Files []AgentFile `json:"files"`
	// MCP lists the MCP servers wired into the sandbox for this agent.
	MCP []MCPServer `json:"mcp"`
	// Env are environment variables injected into the sandbox for this agent.
	Env map[string]string `json:"env"`
	// Layout is the sandbox-injection layout (config root + workspace dir).
	// Always returned with defaults applied so callers can use it verbatim.
	Layout AgentLayout `json:"layout"`
	// Prompts optionally overrides the platform-injected prompt text and rule
	// files for this Agent. Nil = use platform defaults for everything.
	Prompts *models.AgentPrompts `json:"prompts,omitempty"`
}

// agentConfig is the on-disk shape of agent.json (working-dir files live under
// the workspace/ subfolder).
type agentConfig struct {
	ProjectID         string               `json:"projectId,omitempty"`
	AcpBackend        string               `json:"acpBackend,omitempty"`
	GitCredentialType string               `json:"gitCredentialType,omitempty"`
	GitSshKnownHosts  string               `json:"gitSshKnownHosts,omitempty"`
	GitSshPrivateKey  string               `json:"gitSshPrivateKey,omitempty"`
	MCP               []MCPServer          `json:"mcp,omitempty"`
	Env               map[string]string    `json:"env,omitempty"`
	Layout            *AgentLayout         `json:"layout,omitempty"`
	Prompts           *models.AgentPrompts `json:"prompts,omitempty"`
}

// DefaultPlatformMCP returns the platform's built-in MCP server (the run-scoped
// artifact-store) wired via template vars — the run URL/token are resolved per
// run (see runtime.mcpVars), so no secret is persisted. New agents get this by
// default so they can read/write artifacts and use plan/ask tools out of the box.
func DefaultPlatformMCP() []MCPServer {
	return []MCPServer{{
		Name:    ArtifactStoreMCP,
		URL:     "${GRASP_ARTIFACT_URL}",
		Headers: map[string]string{"Authorization": "Bearer ${GRASP_ARTIFACT_TOKEN}"},
	}}
}

// List returns all agents, sorted by name.
func (s *AgentService) List() []Agent {
	out := []Agent{}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if a, ok := s.Get(e.Name()); ok {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns one agent (working-dir files + mcp + env).
func (s *AgentService) Get(name string) (Agent, bool) {
	dir := filepath.Join(s.root, sanitize(name))
	if _, err := os.Stat(dir); err != nil {
		return Agent{}, false
	}
	cfg := s.readConfig(name)
	layout := AgentLayout{}
	if cfg.Layout != nil {
		layout = *cfg.Layout
	}
	backend := NormalizeAcpBackend(cfg.AcpBackend)
	layout = layout.withDefaults()
	if strings.TrimSpace(layout.ConfigRoot) == DefaultConfigRoot && backend != AcpBackendCursor {
		layout.ConfigRoot = DefaultConfigRootForBackend(backend)
	}
	env := cfg.Env
	if env == nil {
		env = map[string]string{}
	}
	return Agent{
		Name:              name,
		ProjectID:         strings.TrimSpace(cfg.ProjectID),
		AcpBackend:        backend,
		GitCredentialType: normalizeGitCredentialType(cfg.GitCredentialType),
		GitSshKnownHosts:  cfg.GitSshKnownHosts,
		GitSshPrivateKey:  cfg.GitSshPrivateKey,
		Files:             s.readFiles(name),
		MCP:               cfg.MCP,
		Env:               env,
		Layout:            layout,
		Prompts:           cfg.Prompts,
	}, true
}

// readFiles loads the agent's working-dir tree from workspace/.
func (s *AgentService) readFiles(name string) []AgentFile {
	dir := filepath.Join(s.root, sanitize(name))
	return readTreeIfDir(filepath.Join(dir, WorkDirName))
}

func readTreeIfDir(dir string) []AgentFile {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil
	}
	return readTree(dir)
}

// readTree walks dir and returns every file as a slash-relative AgentFile.
func readTree(dir string) []AgentFile {
	var out []AgentFile
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if isWorkspacePlaceholder(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		out = append(out, AgentFile{Path: filepath.ToSlash(rel), Content: string(b)})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// safeRel cleans a working-dir-relative path and rejects traversal/absolute paths.
func safeRel(p string) string {
	p = strings.TrimSpace(filepath.ToSlash(p))
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return ""
	}
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "..") {
		return ""
	}
	return p
}

// underRoot joins root/rel and asserts the result stays within root (Zip Slip /
// path-traversal barrier for CodeQL #17/#18/#19).
func underRoot(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("non-local path %q", rel)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(absRoot, rel)
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+sep) {
		return "", fmt.Errorf("path %q escapes root", rel)
	}
	return absFull, nil
}

func (s *AgentService) readConfig(name string) agentConfig {
	var cfg agentConfig
	b, err := os.ReadFile(filepath.Join(s.root, sanitize(name), "agent.json"))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(b, &cfg)
	return cfg
}

// UpdateProjectID writes only agent.json.projectId. Unlike Save it does not
// RemoveAll(workspace), rewrite files, or touch MCP / env / layout / prompts.
// Used by group-level assign so unrelated drafts and workspace stay intact.
func (s *AgentService) UpdateProjectID(name, projectID string) error {
	n := sanitize(name)
	if n == "" {
		return fmt.Errorf("invalid agent name")
	}
	if !s.Exists(n) {
		return fmt.Errorf("agent %q not found", name)
	}
	dir := filepath.Join(s.root, n)
	cfg := s.readConfig(n)
	cfg.ProjectID = strings.TrimSpace(projectID)
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "agent.json"), b, 0o644)
}

// Save writes an agent's working-dir tree + config, creating it if needed. The
// workspace/ tree is fully rewritten so removed files disappear from disk.
func (s *AgentService) Save(a Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveUnlocked(a)
}

func (s *AgentService) saveUnlocked(a Agent) error {
	name := sanitize(a.Name)
	if name == "" {
		return fmt.Errorf("invalid agent name")
	}
	if err := ValidateAgentSSHMeta(a.GitSshKnownHosts, a.GitSshPrivateKey); err != nil {
		return err
	}
	StripSSHEnvKeys(a.Env)
	dir := filepath.Join(s.root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	layout := a.Layout.withDefaults()
	backend := NormalizeAcpBackend(a.AcpBackend)
	if strings.TrimSpace(a.Layout.ConfigRoot) == "" {
		layout.ConfigRoot = DefaultConfigRootForBackend(backend)
	}
	cfg := agentConfig{
		ProjectID:         strings.TrimSpace(a.ProjectID),
		AcpBackend:        backend,
		GitCredentialType: normalizeGitCredentialType(a.GitCredentialType),
		GitSshKnownHosts:  a.GitSshKnownHosts,
		GitSshPrivateKey:  a.GitSshPrivateKey,
		MCP:               a.MCP,
		Env:               a.Env,
		Layout:            &layout,
		Prompts:           a.Prompts,
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
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
	for _, f := range a.Files {
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

// Delete removes an agent and all its files.
func (s *AgentService) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteUnlocked(name)
}

func (s *AgentService) deleteUnlocked(name string) error {
	n := sanitize(name)
	if n == "" {
		return fmt.Errorf("invalid agent name")
	}
	if s.Vcs != nil {
		_ = s.Vcs.DeleteAgent(n)
	}
	return os.RemoveAll(filepath.Join(s.root, n))
}

// Rename atomically renames an agent directory (old -> newName) via os.Rename,
// so the change either fully succeeds or leaves the old agent untouched (no
// half-copied intermediate state).
func (s *AgentService) Rename(old, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, n := sanitize(old), sanitize(newName)
	if o == "" || n == "" {
		return fmt.Errorf("invalid agent name")
	}
	if o == n {
		return nil
	}
	if !s.Exists(o) {
		return fmt.Errorf("agent %q not found", old)
	}
	if s.Exists(n) {
		return fmt.Errorf("agent %q already exists", newName)
	}
	if err := os.Rename(filepath.Join(s.root, o), filepath.Join(s.root, n)); err != nil {
		return err
	}
	if s.Vcs != nil {
		if err := s.Vcs.RenameAgent(o, n); err != nil {
			return err
		}
	}
	return nil
}

// Exists reports whether an agent directory is present.
func (s *AgentService) Exists(name string) bool {
	n := sanitize(name)
	if n == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(s.root, n))
	return err == nil
}

// WorkDir returns the agent's on-disk workspace/ directory if it exists.
func (s *AgentService) WorkDir(name string) string {
	d := filepath.Join(s.root, sanitize(name), WorkDirName)
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return d
	}
	return ""
}

// sanitize prevents path traversal in agent names (no ReplaceAll("..","") incomplete sanitization).
// Path layer accepts Unicode L/N + `._-` so legacy dotted names (e.g. clarify.v1) still resolve.
// Write-identity rules (Create/Rename targets) live in NormalizeAndValidateAgentName.
func sanitize(name string) string {
	return sanitizeAgentPath(name)
}
