package services

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func randID() string { return uuid.NewString()[:8] }

// SandboxService manages long-lived, interactive sandboxes used for Agent
// chat-testing. Each sandbox is a Docker container bound to one Agent profile
// (its rules/skills/mcp/env), kept alive across multiple chat turns and
// reclaimed by an idle TTL sweeper. State is persisted (models.Sandbox) so the
// pool survives restarts; the live ACP WebSocket connection is re-established
// lazily on the next chat after a restart.
//
// Per-run workflow node sandboxes are NOT managed here — they remain ephemeral
// inside the runtime provider.
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
	chatTimeout          time.Duration
	// ttl / runTTL / max are runtime-tunable via the settings page, so they are
	// held atomically (read from many sites, some already under s.mu) rather
	// than guarded by s.mu — avoiding any lock-reentrancy at the read points.
	ttl    atomic.Int64 // idle TTL for interactive test sandboxes (ns)
	runTTL atomic.Int64 // retention TTL for finished run node sandboxes (ns)
	max    atomic.Int64 // max concurrently live interactive test sandboxes
	// maxSolo caps concurrently live resident Solo sandboxes.
	maxSolo atomic.Int64
	// soloOnDestroy tells the Solo service a purpose=solo sandbox is gone.
	soloOnDestroy func(sandboxID uint)
	// maxRetention caps how far leases can push a run sandbox's deadline (ns).
	maxRetention atomic.Int64

	mu sync.Mutex
	// leaseMu serializes lease read-modify-write so concurrent renewals
	// (activity, manual, preview, retire) can't drop each other's lease.
	leaseMu sync.Mutex
	// lastTouch throttles activity-lease writes per sandbox id.
	lastTouch map[uint]time.Time
	live      map[uint]*liveSandbox
	// runActive marks per-run node sandboxes (by container name) that are
	// currently executing. These are owned by the runtime provider, not by
	// this service's ACP pool; the flag protects them from being stopped or
	// swept while in use.
	runActive map[string]bool
	// agentOnDestroy revokes thread-bound agent MCP sessions when a
	// purpose=agent|pm sandbox is torn down (optional; SetAgentSandboxDestroyHook).
	agentOnDestroy AgentSandboxDestroyHook
	// testScheduler hooks Register/Restore and Unregister for purpose=test
	// read-only task-scheduler injection (optional; SetTestSchedulerHooks).
	testScheduler TestSchedulerHooks
	// codexLoginWriteBack stores a refreshed Codex auth.json into the workspace
	// credential. Nil skips write-back. The callback must not log the file body.
	codexLoginWriteBack func(credentialID, content string) error
}

// TestSchedulerHooks wires purpose=test scheduler session lifecycle.
type TestSchedulerHooks struct {
	Register   func(profile, runID, token string)
	Unregister func(token string)
}

// liveSandbox is the in-memory connection state for a running sandbox.
type liveSandbox struct {
	modelBindingFingerprint string
	gitBindingsFingerprint  string
	sb                      *sandbox.Sandbox
	acp                     *sandbox.ACPClient
	home                    string
	busy                    bool
	codexAuthCredentialID   string
	codexAuthInjected       string
	codexAuthPath           string
	// codexAuthRejected is set from the latest turn's CLI error, not from
	// narration. A login refusal keeps the previously saved file.
	codexAuthRejected bool
}

// resolveSandboxImage picks the per-acpBackend image from live config (nil-safe).
func resolveSandboxImage(backend string) string {
	return config.GetConfig().ResolveSandboxImage(backend)
}

// SandboxOptions configures the service.
type SandboxOptions struct {
	ModelBinding func(string, *modelrouting.Binding) (modelrouting.Resolved, error)
	ProfilesRoot string
	MCPEndpoint  string
	// Env is the vendor-neutral sandbox env (e.g. CURSOR_API_KEY for the
	// reference image), injected into interactive test sandboxes.
	Env         map[string]string
	ChatTimeout time.Duration
	TTL         time.Duration
	// RunTTL is how long a finished run's node sandbox is retained (kept alive)
	// for debugging before the idle sweeper reclaims it.
	RunTTL time.Duration
	// MaxRetention caps lease-extended run sandbox lifetime. 0 = 24h.
	MaxRetention time.Duration
	Max          int
	// MaxSolo caps resident Solo sandboxes. 0 = 5.
	MaxSolo int
	// SharedAgent optional baseline used by Open / OpenAgentSandbox.
	SharedAgent *SharedAgentService
	// CredentialReferences resolves ${credential:<id>} values for MCP
	// templates without placing those values in the process environment.
	CredentialReferences func() map[string]string
	// SelectedCredential resolves an explicitly selected extra environment credential.
	// A missing id returns ok=false and must not fall back to another row.
	SelectedCredential runtime.SelectedCredentialFunc
	// GitCredential resolves the secret and host of a selected Git authorization.
	GitCredential runtime.GitCredentialFunc
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
	Endpoints map[string]string `json:"endpoints,omitempty"`
}

// NewSandboxService builds the service.
func NewSandboxService(db *gorm.DB, mgr *sandbox.Manager, skills *AgentService, host *mcp.Host, opts SandboxOptions) *SandboxService {
	if opts.TTL <= 0 {
		opts.TTL = 30 * time.Minute
	}
	if opts.Max <= 0 {
		opts.Max = 5
	}
	if opts.ChatTimeout <= 0 {
		opts.ChatTimeout = 10 * time.Minute
	}
	if opts.RunTTL <= 0 {
		opts.RunTTL = opts.TTL
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
		chatTimeout:          opts.ChatTimeout,
		live:                 map[uint]*liveSandbox{},
		runActive:            map[string]bool{},
		lastTouch:            map[uint]time.Time{},
	}
	if opts.MaxRetention <= 0 {
		opts.MaxRetention = 24 * time.Hour
	}
	s.ttl.Store(int64(opts.TTL))
	s.runTTL.Store(int64(opts.RunTTL))
	s.maxRetention.Store(int64(opts.MaxRetention))
	s.max.Store(int64(opts.Max))
	if opts.MaxSolo <= 0 {
		opts.MaxSolo = 5
	}
	s.maxSolo.Store(int64(opts.MaxSolo))
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

// TTL / RunTTL / MaxTestSandboxes return the live tunable values.
func (s *SandboxService) TTL() time.Duration    { return time.Duration(s.ttl.Load()) }
func (s *SandboxService) RunTTL() time.Duration { return time.Duration(s.runTTL.Load()) }
func (s *SandboxService) MaxTestSandboxes() int { return int(s.max.Load()) }

// Manager exposes the underlying docker manager (for preview proxy / probes).
func (s *SandboxService) Manager() *sandbox.Manager { return s.mgr }

// SetTTLs updates the interactive and run-sandbox TTLs at runtime (settings
// page). Non-positive values are ignored so a partial update can't zero a TTL.
func (s *SandboxService) SetTTLs(runTTL, testTTL time.Duration) {
	if testTTL > 0 {
		s.ttl.Store(int64(testTTL))
	}
	if runTTL > 0 {
		s.runTTL.Store(int64(runTTL))
	}
}

// SetMaxTestSandboxes updates the interactive sandbox cap at runtime.
func (s *SandboxService) SetMaxTestSandboxes(n int) {
	if n > 0 {
		s.max.Store(int64(n))
	}
}
