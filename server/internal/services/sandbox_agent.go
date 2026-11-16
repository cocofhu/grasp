package services

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// agentContainerExtra carries the launch knobs of agent containers (Solo and
// Agent Studio tests).
type agentContainerExtra struct {
	Repos []models.SessionRepo
	// Resident selects Solo ownership callbacks; its idle policy is shared.
	Resident bool
	// LaunchKind is solo or test.
	LaunchKind string
	// OnEnv receives the env key provenance once the env is final.
	OnEnv func(sources map[string]string)
	// OnFail runs after the row is marked as error.
	OnFail func(err error)
	// OnReady runs after the row is marked as running.
	OnReady func()
	// Restore fills the workspace once SSH and ACP are ready, before the
	// chat session opens. A failure destroys the new sandbox.
	Restore func(ctx context.Context, sb *sandbox.Sandbox) error
	// InitialChatModel selects only the sandbox's first chat. Empty follows the primary model.
	InitialChatModel           string
	InitialChatReasoningEffort *string
}

// Env key provenance labels reported to the Solo environment panel.
// EnvSourceProject remains so sessions recorded before repository env was
// removed still display that source. New starts do not assign it.
const (
	EnvSourcePlatform   = "platform"
	EnvSourceAgent      = "agent"
	EnvSourceProject    = "project"
	EnvSourceCredential = "credential"
	EnvSourceSystem     = "system"
)

// envTracker labels keys that a layering stage added or changed.
type envTracker struct {
	sources map[string]string
	prev    map[string]string
}

func newEnvTracker() *envTracker {
	return &envTracker{sources: map[string]string{}, prev: map[string]string{}}
}

func (t *envTracker) mark(env map[string]string, label string) {
	for k, v := range env {
		if old, ok := t.prev[k]; !ok || old != v {
			t.sources[k] = label
		}
	}
	for k := range t.prev {
		if _, ok := env[k]; !ok {
			delete(t.sources, k)
		}
	}
	t.prev = make(map[string]string, len(env))
	for k, v := range env {
		t.prev[k] = v
	}
}

func (s *SandboxService) startAgentContainer(id uint, name, profile, threadID, runID, sharedToken string, platformSpecs []sandbox.MCPServerSpec, agent Agent, extra agentContainerExtra) {
	// Cover on-demand image pull + create (same budget as Manager createTimeout / g2.3).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	fail := func(err error) {
		log.Warn().Err(err).Str("name", name).Uint("id", id).Msg("agent sandbox start failed")
		s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
			"status": "error", "error": truncErr(err), "cleanup_pending": true, "updated_at": time.Now(),
		})
		if extra.Resident {
			s.releaseSandboxScope(SandboxPurposeSolo, runID, "solo_sandbox_failed")
		}
		if extra.OnFail != nil {
			extra.OnFail(err)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			log.Error().Str("name", name).Uint("id", id).Interface("panic", r).
				Bytes("stack", debug.Stack()).Msg("agent sandbox start panicked")
			fail(fmt.Errorf("panic during sandbox start: %v", r))
		}
	}()

	prepared, err := s.prepareAgentContainer(profile, threadID, runID, sharedToken, name, platformSpecs, agent, extra)
	if err != nil {
		fail(err)
		return
	}
	spec, specs, backend := prepared.spec, prepared.specs, prepared.backend
	home, modelFingerprint := spec.ConfigHome, prepared.modelFingerprint
	configRoot, gitCredentials := spec.ConfigRoot, spec.GitCredentials

	sb, err := s.mgr.Create(ctx, spec)
	if err != nil {
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("create sandbox: %w", err))
		return
	}
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("acp not ready: %w", err))
		return
	}
	if extra.Restore != nil {
		if err := extra.Restore(ctx, sb); err != nil {
			sb.Destroy(context.Background())
			_ = os.RemoveAll(home)
			fail(fmt.Errorf("restore workspace: %w", err))
			return
		}
	}
	acp := sb.ACP().WithSession(sb.WorkspaceDir, runtime.MCPServersJSON(specs))
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("acp connect: %w", err))
		return
	}

	activeAt := time.Now()
	res := s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"model_binding_fingerprint": modelFingerprint,
		"git_bindings_fingerprint":  gitBindingsFingerprint(gitCredentials),
		"status":                    "running", "name": sb.Name, "host": sb.Host, "acp_port": sb.Port,
		"code_server_port": sb.CodeServerPort, "error": "", "home_dir": home,
		"last_active_at": activeAt, "updated_at": activeAt,
	})
	if res.Error != nil || res.RowsAffected == 0 {
		acp.Close()
		sb.Destroy(context.Background())
		_ = os.RemoveAll(home)
		if extra.Resident {
			s.releaseSandboxScope(SandboxPurposeSolo, runID, "solo_sandbox_removed")
			if extra.OnFail != nil {
				extra.OnFail(fmt.Errorf("sandbox record removed during start"))
			}
		}
		return
	}
	s.mu.Lock()
	s.live[id] = &liveSandbox{
		sb: sb, acp: acp, home: home,
		prepared: prepared, soloRouteAgent: &agent,
		modelBindingFingerprint: modelFingerprint,
		gitBindingsFingerprint:  gitBindingsFingerprint(gitCredentials),
	}
	s.mu.Unlock()
	s.registerLiveCodex(id, sb, prepared.codexCredentialID, codexLiveAuthPath(backend, configRoot), prepared.codexVersion)
	log.Info().Str("name", sb.Name).Str("profile", profile).Str("thread", threadID).
		Uint("id", id).Bool("resident", extra.Resident).Msg("agent sandbox opened")
	if extra.OnReady != nil {
		extra.OnReady()
	}
}

// filterAgentPlatformMCP drops artifact-store entries so they are not
// double-injected from Agent mcp[] (platform appends real endpoints), and
// entries of retired platform MCPs.
func filterAgentPlatformMCP(in []sandbox.MCPServerSpec) []sandbox.MCPServerSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]sandbox.MCPServerSpec, 0, len(in))
	for _, sp := range in {
		name := strings.ToLower(strings.TrimSpace(sp.Name))
		if name == ArtifactStoreMCP || strings.Contains(sp.URL, "/mcp/runs/") || runtime.IsRetiredPlatformMCP(sp.Name, sp.URL) {
			continue
		}
		out = append(out, sp)
	}
	return out
}

func dedupeMCPByName(in []sandbox.MCPServerSpec) []sandbox.MCPServerSpec {
	seen := map[string]bool{}
	out := make([]sandbox.MCPServerSpec, 0, len(in))
	for _, sp := range in {
		n := strings.ToLower(strings.TrimSpace(sp.Name))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, sp)
	}
	return out
}

func sanitizeID(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, " ", "")
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}
