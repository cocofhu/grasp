package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// SharedAgentConfig is the workspace Agent baseline used as the extend layer
// at startup (workflow Run + chat test). Shape mirrors Agent without a Name.
// Empty config is valid.
type SharedAgentConfig struct {
	AcpBackend           string            `json:"acpBackend,omitempty"`
	GitCredentialType    string            `json:"gitCredentialType,omitempty"`
	AiCredentialID       string            `json:"aiCredentialId,omitempty"`
	OpenCodeCredentialID string            `json:"openCodeCredentialId,omitempty"`
	Files                []AgentFile       `json:"files"`
	MCP                  []MCPServer       `json:"mcp"`
	Env                  map[string]string `json:"env"`
	Layout               AgentLayout       `json:"layout"`
}

// sharedAgentDisk mirrors agent.json under data/shared-agent/.
type sharedAgentDisk struct {
	AcpBackend           string            `json:"acpBackend,omitempty"`
	GitCredentialType    string            `json:"gitCredentialType,omitempty"`
	AiCredentialID       string            `json:"aiCredentialId,omitempty"`
	OpenCodeCredentialID string            `json:"openCodeCredentialId,omitempty"`
	MCP                  []MCPServer       `json:"mcp,omitempty"`
	Env                  map[string]string `json:"env,omitempty"`
	Layout               *AgentLayout      `json:"layout,omitempty"`
}

// SharedAgentService persists the shared Agent baseline on disk:
//
//	<root>/workspace/**  -- shared working-dir tree
//	<root>/agent.json    -- mcp / env / layout / meta
type SharedAgentService struct {
	root string
	mu   sync.Mutex
}

// DefaultSharedAgentRoot derives the shared-config root next to profiles
// (data/profiles → data/shared-agent).
func DefaultSharedAgentRoot(profilesRoot string) string {
	profilesRoot = strings.TrimSpace(profilesRoot)
	if profilesRoot == "" {
		profilesRoot = "data/profiles"
	}
	return filepath.Join(filepath.Dir(profilesRoot), "shared-agent")
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

// Get returns the shared config. Missing files → empty valid config.
func (s *SharedAgentService) Get() SharedAgentConfig {
	cfg := s.readConfig()
	layout := AgentLayout{}
	if cfg.Layout != nil {
		layout = *cfg.Layout
	}
	backend := strings.TrimSpace(cfg.AcpBackend)
	if backend != "" {
		backend = NormalizeAcpBackend(backend)
	}
	if strings.TrimSpace(layout.ConfigRoot) == "" {
		layout.ConfigRoot = DefaultConfigRootForBackend(backend)
	}
	layout = layout.withDefaults()
	env := cfg.Env
	if env == nil {
		env = map[string]string{}
	}
	files := s.readFiles()
	if files == nil {
		files = []AgentFile{}
	}
	mcps := cfg.MCP
	if mcps == nil {
		mcps = []MCPServer{}
	}
	return SharedAgentConfig{
		AcpBackend:           backend,
		GitCredentialType:    normalizeGitCredentialType(cfg.GitCredentialType),
		AiCredentialID:       strings.TrimSpace(cfg.AiCredentialID),
		OpenCodeCredentialID: strings.TrimSpace(cfg.OpenCodeCredentialID),
		Files:                files,
		MCP:                  mcps,
		Env:                  env,
		Layout:               layout,
	}
}

// Save writes the shared config (full replace of workspace + agent.json).
func (s *SharedAgentService) Save(cfg SharedAgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := RejectSecretEnvKeys(cfg.Env); err != nil {
		return err
	}
	dir := s.root
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	layout := cfg.Layout.withDefaults()
	backend := strings.TrimSpace(cfg.AcpBackend)
	if backend != "" {
		var err error
		if backend, err = ParseAcpBackend(backend); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.Layout.ConfigRoot) == "" {
			layout.ConfigRoot = DefaultConfigRootForBackend(backend)
		}
	}
	disk := sharedAgentDisk{
		AcpBackend:           backend,
		GitCredentialType:    normalizeGitCredentialType(cfg.GitCredentialType),
		AiCredentialID:       strings.TrimSpace(cfg.AiCredentialID),
		OpenCodeCredentialID: strings.TrimSpace(cfg.OpenCodeCredentialID),
		MCP:                  cfg.MCP,
		Env:                  cfg.Env,
		Layout:               &layout,
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
func (s *SharedAgentService) WorkDir() string {
	d := filepath.Join(s.root, WorkDirName)
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return d
	}
	return ""
}

// AsAgent views the shared config as an Agent-shaped value for merge helpers.
func (c SharedAgentConfig) AsAgent() Agent {
	env := c.Env
	if env == nil {
		env = map[string]string{}
	}
	return Agent{
		Name:                 "",
		AcpBackend:           c.AcpBackend,
		GitCredentialType:    c.GitCredentialType,
		AiCredentialID:       c.AiCredentialID,
		OpenCodeCredentialID: c.OpenCodeCredentialID,
		Files:                c.Files,
		MCP:                  c.MCP,
		Env:                  env,
		Layout:               c.Layout,
	}
}

// ExtendOverlay merges shared (base) then agent (overlay); Agent wins per key.
// Credential ids are kept from the Agent. Shared ids are not applied here
// because this path cannot check credential kinds; use ExtendOverlayWithKind.
func ExtendOverlay(shared SharedAgentConfig, agent Agent) Agent {
	return ExtendOverlayWithKind(shared, agent, nil)
}

// ExtendOverlayWithKind is ExtendOverlay plus the shared-credential fallback.
// kindOf reports a credential id's env key. A nil kindOf keeps the Agent's
// own ids and does not copy a shared id.
func ExtendOverlayWithKind(shared SharedAgentConfig, agent Agent, kindOf runtime.CredentialKindFunc) Agent {
	base := shared.AsAgent()
	out := Agent{
		Name:                 agent.Name,
		CredentialBindings:   agent.CredentialBindings,
		GitCredentialID:      agent.GitCredentialID,
		SshHostsCredentialID: agent.SshHostsCredentialID,
		Capabilities:         agent.Capabilities,
		AcpBackend:           pickNonEmpty(agent.AcpBackend, base.AcpBackend),
		GitCredentialType:    pickNonEmpty(agent.GitCredentialType, base.GitCredentialType),
		Files:                mergeFiles(base.Files, agent.Files),
		MCP:                  mergeMCP(base.MCP, agent.MCP),
		Layout:               mergeLayout(base.Layout, agent.Layout),
	}
	if out.AcpBackend != "" {
		out.AcpBackend = NormalizeAcpBackend(out.AcpBackend)
	}
	out.Env = envauth.OverlayEnv(runtime.SharedEnvForBackend(base.Env, base.AcpBackend, out.AcpBackend), agent.Env)
	out.Layout = out.Layout.withDefaults()
	if out.Env == nil {
		out.Env = map[string]string{}
	}
	out.AiCredentialID, out.OpenCodeCredentialID = runtime.MergeCodingCredentialIDs(
		out.AcpBackend,
		agent.AiCredentialID, agent.OpenCodeCredentialID,
		shared.AiCredentialID, shared.OpenCodeCredentialID,
		kindOf,
	)
	return out
}

// ClearCredentialSelection forgets a shared generic-credential pointer that
// still names credentialID. Clearing or revoking the credential must not leave
// a stale id that injection would treat as an explicit choice.
func (s *SharedAgentService) ClearCredentialSelection(credentialID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return
	}
	path := filepath.Join(s.root, "agent.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var disk sharedAgentDisk
	if err := json.Unmarshal(b, &disk); err != nil {
		return
	}
	cleared := false
	if strings.TrimSpace(disk.AiCredentialID) == credentialID {
		disk.AiCredentialID = ""
		cleared = true
	}
	if strings.TrimSpace(disk.OpenCodeCredentialID) == credentialID {
		disk.OpenCodeCredentialID = ""
		cleared = true
	}
	if !cleared {
		return
	}
	out, err := json.MarshalIndent(&disk, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		log.Warn().Err(err).Str("credential", credentialID).Msg("clear shared credential selection")
	}
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

func (s *SharedAgentService) readConfig() sharedAgentDisk {
	var cfg sharedAgentDisk
	b, err := os.ReadFile(filepath.Join(s.root, "agent.json"))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(b, &cfg)
	return cfg
}

func (s *SharedAgentService) readFiles() []AgentFile {
	return readTreeIfDir(filepath.Join(s.root, WorkDirName))
}
