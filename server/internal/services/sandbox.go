package services

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/solobackup"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func randID() string { return uuid.NewString()[:8] }

// SandboxService owns admission, activity, recovery and cleanup for all sandbox purposes.
// Backend differences are hidden behind the manager; capacity and idle policy are shared.
type SandboxService struct {
	db     *gorm.DB
	mgr    *sandbox.Manager
	skills *AgentService
	host   *mcp.Host
	// shared provides workspace files, MCP servers and ordinary environment defaults.
	shared *SharedAgentService

	profilesRoot         string
	mcpEndpoint          string
	env                  map[string]string
	credentialReferences func() map[string]string
	modelBinding         func(string, *modelrouting.Binding) (modelrouting.Resolved, error)
	selectedCredential   runtime.SelectedCredentialFunc
	gitCredential        runtime.GitCredentialFunc
	project              func(id string) (*models.Project, bool)
	// Atomic tunables are read from lifecycle and admission paths under different locks.
	idleTTL    atomic.Int64
	max        atomic.Int64
	capacityMu sync.Mutex
	backups    solobackup.Store
	backupKeep atomic.Int64
	soloBusy   func(uint) bool
	// soloOnDestroy tells the Solo service a purpose=solo sandbox is gone.
	soloOnDestroy   func(sandboxID uint)
	soloOnRecovered func(sandboxID uint)
	// soloOnPaused reports a running resident the provider paused.
	soloOnPaused func(sandboxID uint)

	mu sync.Mutex
	// Per-resident locks serialize recovery with explicit destruction without
	// making one unavailable sandbox block every other session.
	soloRecoveryLocks sync.Map
	// lastTouch tracks activity concurrent with an idle-reclamation probe.
	lastTouch map[uint]time.Time
	live      map[uint]*liveSandbox
	// runActive marks per-run node sandboxes (by container name) that are
	// currently executing. These are owned by the runtime provider, not by
	// this service's ACP pool; the flag protects them from being stopped or
	// swept while in use.
	runActive           map[string]bool
	runTasks            map[string]int
	runRecoveryPreparer runtime.SandboxRecoveryPreparer
	// codexLogins pushes the platform's Codex login to interactive sandboxes
	// (optional; SetCodexLogins). Sandboxes never write a login back.
	codexLogins *runtime.CodexLoginHub
	// soloRouteRuntime defaults to the real manager and ACP bridge. Tests can
	// supply the same operations without requiring Docker or SSH.
	soloRouteRuntime soloRouteOps
	// soloBridgeReady defaults to sandbox.WaitForACPReady; tests replace it.
	soloBridgeReady func(context.Context, *sandbox.Sandbox) error
}

// liveSandbox is the in-memory connection state for a running sandbox.
type liveSandbox struct {
	modelBindingFingerprint string
	gitBindingsFingerprint  string
	sb                      *sandbox.Sandbox
	acp                     *sandbox.ACPClient
	home                    string
	prepared                *preparedAgentContainer
	// soloRouteAgent identifies the applied route between re-prime completion
	// and Solo's persistence write. It never changes the saved Agent profile.
	soloRouteAgent *Agent
	switching      bool
	busy           bool
}

// SandboxOptions configures the service.
type SandboxOptions struct {
	ModelBinding func(string, *modelrouting.Binding) (modelrouting.Resolved, error)
	ProfilesRoot string
	MCPEndpoint  string
	// Env is the vendor-neutral sandbox env (e.g. CURSOR_API_KEY for the
	// reference image), injected into interactive test sandboxes.
	Env     map[string]string
	IdleTTL time.Duration
	Max     int
	// SharedAgent optional baseline used by Open / OpenAgentSandbox.
	SharedAgent *SharedAgentService
	// CredentialReferences resolves ${credential:<id>} values for MCP
	// templates without placing those values in the process environment.
	CredentialReferences func() map[string]string
	// SelectedCredential resolves an explicitly selected extra environment credential.
	// A missing id returns ok=false and must not fall back to another row.
	SelectedCredential runtime.SelectedCredentialFunc
	// GitCredential resolves the secret and host of a repository's credential.
	GitCredential runtime.GitCredentialFunc
	// Project reads a repository's current credential and commit author.
	Project func(id string) (*models.Project, bool)
}

// SandboxView is the API shape: the persisted record plus live-derived flags.
type SandboxView struct {
	models.Sandbox
	ContainerStatus string `json:"containerStatus"`
	Busy            bool   `json:"busy"`
	Connected       bool   `json:"connected"`
	HasCodeServer   bool   `json:"hasCodeServer"`
	HasACP          bool   `json:"hasAcp"`
	// Password is the sandbox Token also injected as ROOT_PASSWORD /
	// ACP_BRIDGE_PASSWORD. Exposed so operators can log into
	// code-server / ACP when opening the published host:port directly
	// (remote-dev Environment.password parity). Empty when unset.
	Password string `json:"password,omitempty"`
	// Endpoints are user-visible host:port addresses. GetView only returns
	// session/ide/ssh; CDP/noVNC (9222/6080) are internal and never included.
	// List leaves this nil so JSON omits the field.
	Endpoints         map[string]string `json:"endpoints,omitempty"`
	EndpointTransport string            `json:"endpointTransport,omitempty"`
}

// NewSandboxService builds the service.
func NewSandboxService(db *gorm.DB, mgr *sandbox.Manager, skills *AgentService, host *mcp.Host, opts SandboxOptions) *SandboxService {
	if opts.IdleTTL <= 0 {
		opts.IdleTTL = time.Hour
	}
	if opts.Max <= 0 {
		opts.Max = 15
	}
	s := &SandboxService{
		db: db, mgr: mgr, skills: skills, host: host,
		shared:               opts.SharedAgent,
		profilesRoot:         opts.ProfilesRoot,
		mcpEndpoint:          opts.MCPEndpoint,
		env:                  opts.Env,
		credentialReferences: opts.CredentialReferences,
		modelBinding:         opts.ModelBinding,
		selectedCredential:   opts.SelectedCredential,
		gitCredential:        opts.GitCredential,
		project:              opts.Project,
		live:                 map[uint]*liveSandbox{},
		runActive:            map[string]bool{},
		runTasks:             map[string]int{},
		lastTouch:            map[uint]time.Time{},
	}
	s.idleTTL.Store(int64(opts.IdleTTL))
	s.max.Store(int64(opts.Max))
	s.backupKeep.Store(1)
	if host != nil {
		var agents AgentGetter
		if skills != nil {
			agents = skills
		}
		host.AddScopeSource(SandboxScopes(db, agents))
	}
	return s
}

// SetSharedAgent wires the shared workspace files and ordinary defaults.
func (s *SandboxService) SetSharedAgent(shared *SharedAgentService) {
	if s == nil {
		return
	}
	s.shared = shared
}

// effectiveAgent merges shared workspace defaults without granting authorization.
func (s *SandboxService) effectiveAgent(agent Agent) Agent {
	if s == nil || s.shared == nil {
		return ExtendOverlay(SharedAgentConfig{}, agent)
	}
	return ExtendOverlay(s.shared.Get(), agent)
}

// IdleTTL and MaxSandboxes expose the shared live policy.
func (s *SandboxService) IdleTTL() time.Duration { return time.Duration(s.idleTTL.Load()) }
func (s *SandboxService) MaxSandboxes() int      { return int(s.max.Load()) }

// Manager exposes the underlying sandbox manager for probes and data-plane access.
func (s *SandboxService) Manager() *sandbox.Manager { return s.mgr }

func (s *SandboxService) SetIdleTTL(ttl time.Duration) {
	if ttl > 0 {
		s.idleTTL.Store(int64(ttl))
	}
}

func (s *SandboxService) SetMaxSandboxes(n int) {
	if n > 0 {
		s.max.Store(int64(n))
	}
}
