package services

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// soloStartAuthorization checks both the frozen execution Agent and its
// currently selected effective definition. The resolver reads actual current
// secrets/revisions, outside SQL locks; an old in-memory Agent is not a grant.
func (s *SandboxService) soloStartAuthorization(ctx context.Context, profile string, execution Agent) (string, error) {
	current, ok := s.skills.Get(profile)
	if !ok || execution.ModelBinding == nil {
		return "", fmt.Errorf("当前 Agent 授权不可用")
	}
	current = s.effectiveAgent(current)
	current, err := selectSoloConfiguration(s.db, current, execution.ModelBinding.ConfigurationID)
	if err != nil {
		return "", err
	}
	expected, err := s.currentAuthorizationFingerprint(ctx, execution)
	if err != nil {
		return "", err
	}
	actual, err := s.currentAuthorizationFingerprint(ctx, current)
	if err != nil {
		return "", err
	}
	if expected != actual {
		return "", fmt.Errorf("Agent 或当前授权已变化，请核对后重新启动")
	}
	return actual, nil
}

// A resident create owns a renewable physical lease. Close can retire the
// parent immediately, but an in-flight provider result must remain attributable.
// A late result stops its own resource while retaining its files and binding;
// the old create never gains permission to delete a retained workspace.
func (s *SandboxService) startSoloContainer(id uint, name, profile, threadID, runID, token string, platformSpecs []sandbox.MCPServerSpec, agent Agent, extra agentContainerExtra) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	ctx, release, err := s.acquireSandboxOperation(ctx, id, "create")
	if err != nil {
		return
	}
	defer release()
	current := func(tx *gorm.DB) error {
		if extra.Current == nil {
			return nil // Standalone resident callers have no parent chat.
		}
		return extra.Current(tx)
	}
	revoke := func() {
		if s.host != nil && runID != "" {
			// The run namespace survives same-resource resumes. A stale
			// physical owner cannot revoke a successor's restored grant.
			if err := s.commitSandboxOperation(ctx, id, func(*gorm.DB) error { return nil }); err == nil {
				s.host.UnregisterRun(runID)
			}
		}
	}
	fail := func(cause error) {
		log.Warn().Err(cause).Uint("id", id).Msg("resident sandbox startup stopped")
		revoke()
		// Preserve resident files/association after unknown provider outcomes.
		// An old failure cannot publish through a lost physical lease.
		if err := s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
			return s.updateSandboxOperation(ctx, tx, id, map[string]any{"status": "error", "error": truncErr(cause), "destroy_at": nil})
		}); err != nil {
			return // A lost physical owner cannot publish a parent failure either.
		}
		if extra.OnFail != nil {
			extra.OnFail(cause)
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			fail(fmt.Errorf("resident sandbox startup failed"))
		}
	}()
	if err := current(s.db.WithContext(ctx)); err != nil {
		return
	}
	authorization, err := s.soloStartAuthorization(ctx, profile, agent)
	if err != nil {
		fail(err)
		return
	}
	prepared, err := s.prepareAgentContainer(profile, threadID, runID, token, name, platformSpecs, agent, extra)
	if err != nil {
		fail(err)
		return
	}
	spec, specs, backend := prepared.spec, prepared.specs, prepared.backend
	home, configRoot := spec.ConfigHome, spec.ConfigRoot
	latest, err := s.soloStartAuthorization(ctx, profile, agent)
	if err != nil || latest != authorization {
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("当前授权已变化，尚未创建环境"))
		return
	}
	if err := s.commitSandboxOperation(ctx, id, current); err != nil {
		_ = os.RemoveAll(home)
		revoke()
		return
	}
	sb, err := s.mgr.Create(ctx, spec)
	if err != nil {
		_ = os.RemoveAll(home)
		fail(fmt.Errorf("create sandbox: %w", err))
		return
	}
	// Adopt the actual provider identity even if Close changed the parent while
	// Create was in flight. It stays attached to this old sandbox row, never to
	// a successor's parent binding, so uncertain cleanup remains attributable.
	if err := s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(ctx, tx, id, map[string]any{"name": sb.Name, "host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort})
	}); err != nil {
		revoke()
		return
	}
	cleanupStale := func() {
		cleanup, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		revoke()
		// Re-enter the held lease. Only a confirmed stop may clear the closed
		// parent's pending receipt; the provider row/disk remains retained.
		if err := s.stopSoloEnvironment(cleanup, id); err != nil {
			_ = s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
				return s.updateSandboxOperation(ctx, tx, id, map[string]any{
					"desired_state": "stopped", "status": "error", "destroy_at": nil,
					"error": "旧启动已失效，环境停止尚未确认，工作区与关联已保留",
				})
			})
		}
	}
	if err := current(s.db.WithContext(ctx)); err != nil {
		if soloLifecycleObsolete(err) {
			cleanupStale()
		} else {
			fail(err)
		}
		return
	}
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		fail(fmt.Errorf("acp not ready: %w", err))
		return
	}
	acp := sb.ACP().WithSession(sb.EffectiveWorkspaceDir(), mcpServersJSON(specs))
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		fail(fmt.Errorf("acp connect: %w", err))
		return
	}
	latest, err = s.soloStartAuthorization(ctx, profile, agent)
	if err != nil || latest != authorization {
		acp.Close()
		// Stop rather than delete: files and the exact applied association are
		// retained. Recovery must reject this obsolete grant before unpausing.
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		if s.mgr.Gateway() != nil {
			_ = s.mgr.Gateway().Stop(cleanup, sb.Name)
		}
		done()
		_ = s.commitSandboxOperation(ctx, id, func(tx *gorm.DB) error {
			return s.updateSandboxOperation(ctx, tx, id, map[string]any{"desired_state": "stopped"})
		})
		fail(fmt.Errorf("当前授权已变化，环境已保留且未恢复为运行状态"))
		return
	}
	err = s.commitSandboxOperationPrepared(ctx, id, extra.PublishReady, func(tx *gorm.DB) error {
		if err := current(tx); err != nil {
			return err
		}
		return s.updateSandboxOperation(ctx, tx, id, map[string]any{
			"model_binding_fingerprint": prepared.modelFingerprint, "git_bindings_fingerprint": gitBindingsFingerprint(spec.GitCredentials),
			"authorization_fingerprint": authorization, "status": "running", "error": "", "destroy_at": nil,
		})
	})
	if err != nil {
		acp.Close()
		if soloLifecycleObsolete(current(s.db.WithContext(ctx))) {
			cleanupStale()
		} else {
			fail(err)
		}
		return
	}
	s.mu.Lock()
	s.live[id] = &liveSandbox{sb: sb, acp: acp, home: home, prepared: prepared, soloRouteAgent: &agent, modelBindingFingerprint: prepared.modelFingerprint, gitBindingsFingerprint: gitBindingsFingerprint(spec.GitCredentials)}
	s.mu.Unlock()
	s.registerLiveCodex(id, sb, prepared.codexCredentialID, codexLiveAuthPath(backend, configRoot), prepared.codexVersion)
	log.Info().Str("name", sb.Name).Uint("id", id).Msg("resident sandbox opened")
	if extra.OnReady != nil {
		extra.OnReady()
	}
}
