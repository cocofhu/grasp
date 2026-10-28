package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// SharedAgentConfig is the workspace Agent baseline used as the extend layer
// at startup (workflow Run + chat test). Shape mirrors Agent without a Name.
// Empty config is valid.
type SharedAgentConfig struct {
	AcpBackend   string                `json:"acpBackend,omitempty"`
	ModelBinding *modelrouting.Binding `json:"modelBinding,omitempty"`
	GitBindings  *models.GitBindings   `json:"gitBindings,omitempty"`
	Files        []AgentFile           `json:"files"`
	MCP          []MCPServer           `json:"mcp"`
	Env          map[string]string     `json:"env"`
	Layout       AgentLayout           `json:"layout"`
}

// sharedAgentDisk mirrors agent.json under data/shared-agent/.
type sharedAgentDisk struct {
	AcpBackend   string                `json:"acpBackend,omitempty"`
	ModelBinding *modelrouting.Binding `json:"modelBinding,omitempty"`
	GitBindings  *models.GitBindings   `json:"gitBindings,omitempty"`
	MCP          []MCPServer           `json:"mcp,omitempty"`
	Env          map[string]string     `json:"env,omitempty"`
	Layout       *AgentLayout          `json:"layout,omitempty"`
}

// SharedAgentService persists the shared Agent baseline on disk:
//
//	<root>/workspace/**  -- shared working-dir tree
//	<root>/agent.json    -- mcp / env / layout / meta
type SharedAgentService struct {
	root         string
	mu           sync.Mutex
	resolveModel func(*modelrouting.Binding) (string, error)
	validateGit  func(*models.GitBindings) error
}

// SetBindingResolvers validates explicitly selected workspace defaults.
func (s *SharedAgentService) SetBindingResolvers(model func(*modelrouting.Binding) (string, error), git func(*models.GitBindings) error) {
	s.resolveModel, s.validateGit = model, git
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
	layout = layout.withDefaults()
	env := runtime.ModelIndependentEnv(cfg.Env)
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
		AcpBackend: cfg.AcpBackend, ModelBinding: cfg.ModelBinding, GitBindings: cfg.GitBindings,
		Files:  files,
		MCP:    mcps,
		Env:    env,
		Layout: layout,
	}
}

// Save writes the shared config (full replace of workspace + agent.json).
func (s *SharedAgentService) Save(cfg SharedAgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := RejectSecretEnvKeys(cfg.Env); err != nil {
		return err
	}
	if cfg.ModelBinding != nil {
		if err := modelrouting.ValidateConfigurationReference(cfg.ModelBinding); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidAgent, err)
		}
		if s.resolveModel == nil {
			return fmt.Errorf("%w: model configuration resolver unavailable", ErrInvalidAgent)
		}
		backend, err := s.resolveModel(cfg.ModelBinding)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidAgent, err)
		}
		if _, err := ParseAcpBackend(backend); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidAgent, err)
		}
		cfg.AcpBackend = backend
		if err := validateAdditionalModelConfigurations(cfg.ModelBinding, s.resolveModel, nil); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidAgent, err)
		}
	} else {
		cfg.AcpBackend = ""
	}
	git, err := models.NormalizeGitBindings(cfg.GitBindings)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAgent, err)
	}
	cfg.GitBindings = git
	if len(git.Items) > 0 {
		if s.validateGit == nil {
			return fmt.Errorf("%w: Git credential resolver unavailable", ErrInvalidAgent)
		}
		if err := s.validateGit(git); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidAgent, err)
		}
	}
	cfg.Env = runtime.ModelIndependentEnv(cfg.Env)
	dir := s.root
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	layout := cfg.Layout.withDefaults()
	disk := sharedAgentDisk{
		AcpBackend: cfg.AcpBackend, ModelBinding: cfg.ModelBinding, GitBindings: cfg.GitBindings,
		MCP:    cfg.MCP,
		Env:    cfg.Env,
		Layout: &layout,
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

// ExtendOverlay merges ordinary defaults and the selected model/Git inheritance.
func ExtendOverlay(shared SharedAgentConfig, agent Agent) Agent {
	out := agent
	if agent.InheritModel {
		out.ModelBinding, out.AcpBackend = shared.ModelBinding, shared.AcpBackend
	}
	if agent.InheritGit {
		out.GitBindings = shared.GitBindings
	}
	out.Files = mergeFiles(shared.Files, agent.Files)
	out.MCP = mergeMCP(shared.MCP, agent.MCP)
	out.Layout = mergeLayout(shared.Layout, agent.Layout).withDefaults()
	if agent.InheritModel && (strings.TrimSpace(agent.Layout.ConfigRoot) == "" || agent.Layout.ConfigRoot == DefaultConfigRootForBackend(agent.AcpBackend)) {
		out.Layout.ConfigRoot = DefaultConfigRootForBackend(out.AcpBackend)
	}
	out.Env = envauth.OverlayEnv(runtime.ModelIndependentEnv(shared.Env), agent.Env)
	if out.Env == nil {
		out.Env = map[string]string{}
	}
	if agent.InheritModel {
		runtime.ApplyInheritedModelEnv(out.Env, shared.Env)
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
