package services

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
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
	// shared is optional project SharedAgent baseline (SSH meta / Token env).
	// When set, Open / OpenAgentSandbox merge via ExtendOverlay before inject.
	shared *SharedAgentService

	profilesRoot                string
	mcpEndpoint                 string
	env                         map[string]string
	projectCredentials          func(projectID string) map[string]string
	projectCredentialReferences func(projectID string) map[string]string
	openCodeCredential          func(projectID, credentialID string) map[string]string
	chatTimeout                 time.Duration
	// ttl / runTTL / max are runtime-tunable via the settings page, so they are
	// held atomically (read from many sites, some already under s.mu) rather
	// than guarded by s.mu — avoiding any lock-reentrancy at the read points.
	ttl    atomic.Int64 // idle TTL for interactive test sandboxes (ns)
	runTTL atomic.Int64 // retention TTL for finished run node sandboxes (ns)
	max    atomic.Int64 // max concurrently live interactive test sandboxes

	mu   sync.Mutex
	live map[uint]*liveSandbox
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
	// openCodeCatalog resolves whether OpenCode knows a provider id natively.
	openCodeCatalog runtime.OpenCodeCatalog
	// codexLoginWriteBack stores a refreshed Codex auth.json into the project
	// credential. Nil skips write-back. The callback must not log the file body.
	codexLoginWriteBack func(projectID, content string) error
}

// TestSchedulerHooks wires purpose=test scheduler session lifecycle.
type TestSchedulerHooks struct {
	Register   func(projectID, profile, runID, token string)
	Unregister func(token string)
}

// liveSandbox is the in-memory connection state for a running sandbox.
type liveSandbox struct {
	sb                *sandbox.Sandbox
	acp               *sandbox.ACPClient
	home              string
	busy              bool
	codexAuthInjected string
	codexAuthPath     string
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
	Max    int
	// SharedAgent optional project baseline used by Open / OpenAgentSandbox.
	SharedAgent *SharedAgentService
	// ProjectCredentials resolves UI-managed project credential environment
	// values for interactive, PM, cron, and project-context sandboxes.
	ProjectCredentials func(projectID string) map[string]string
	// ProjectCredentialReferences resolves ${credential:<id>} values for MCP
	// templates without placing those values in the process environment.
	ProjectCredentialReferences func(projectID string) map[string]string
	// OpenCodeCredential resolves the model-vendor key selected by one Agent.
	OpenCodeCredential func(projectID, credentialID string) map[string]string
	// OpenCodeCatalog lets a gateway absent from OpenCode's provider catalog be
	// declared with an adapter in opencode.json. Nil keeps `custom`-only.
	OpenCodeCatalog runtime.OpenCodeCatalog
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
		shared:                      opts.SharedAgent,
		profilesRoot:                opts.ProfilesRoot,
		mcpEndpoint:                 opts.MCPEndpoint,
		env:                         opts.Env,
		projectCredentials:          opts.ProjectCredentials,
		projectCredentialReferences: opts.ProjectCredentialReferences,
		openCodeCredential:          opts.OpenCodeCredential,
		chatTimeout:                 opts.ChatTimeout,
		openCodeCatalog:             opts.OpenCodeCatalog,
		live:                        map[uint]*liveSandbox{},
		runActive:                   map[string]bool{},
	}
	s.ttl.Store(int64(opts.TTL))
	s.runTTL.Store(int64(opts.RunTTL))
	s.max.Store(int64(opts.Max))
	return s
}

// SetSharedAgent wires the project SharedAgent baseline (SSH meta / Token env).
// Nil-safe; used by Open / OpenAgentSandbox ExtendOverlay before inject.
func (s *SandboxService) SetSharedAgent(shared *SharedAgentService) {
	if s == nil {
		return
	}
	s.shared = shared
}

// effectiveAgent merges SharedAgent under Agent when a project id is known
// (opts / Agent.ProjectID). Satisfies SSH 选源 Agent meta → Shared meta → env.
func (s *SandboxService) effectiveAgent(agent Agent, projectID string) Agent {
	if s == nil || s.shared == nil {
		return agent
	}
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		pid = strings.TrimSpace(agent.ProjectID)
	}
	if pid == "" {
		return agent
	}
	return ExtendOverlay(s.shared.Get(pid), agent)
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
