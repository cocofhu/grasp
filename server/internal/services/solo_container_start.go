package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cocofhu/grasp/internal/models"
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

// retainSoloCreateResult records only the original creation receipt. It never
// grants permission to execute. A changed business grant may stop that exact
// resource, but the physical owner and external execution dataset still fence
// this write, including after the caller's startup wait has expired.
func (s *SandboxService) retainSoloCreateResult(ctx context.Context, id uint, originalName, profile, threadID string, agent Agent, authorization string, sb *sandbox.Sandbox) (bool, error) {
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	latest, grantErr := s.soloStartAuthorization(cleanup, profile, agent)
	changed := grantErr != nil || latest != authorization
	fields := map[string]any{"name": sb.Name, "host": sb.Host, "acp_port": sb.Port, "code_server_port": sb.CodeServerPort}
	if changed {
		fields["desired_state"] = "stopped"
	}
	if err := s.commitSandboxOperation(cleanup, id, func(tx *gorm.DB) error {
		if err := guardSoloCreationIdentity(tx, id, originalName, profile, threadID); err != nil {
			return err
		}
		return s.updateSandboxOperation(cleanup, tx, id, fields)
	}); err != nil {
		return changed, err
	}
	if changed && s.mgr.Gateway() != nil {
		// Stop is best effort; the receipt remains unknown until the ordinary
		// stop reconciler independently observes stopped. Never delete here.
		_ = s.mgr.Gateway().Stop(cleanup, sb.Name)
	}
	return changed, nil
}

func guardSoloCreationIdentity(tx *gorm.DB, id uint, name, profile, threadID string) error {
	var count int64
	if err := tx.Model(&models.Sandbox{}).Where("id = ? AND name = ? AND profile = ? AND purpose = ? AND thread_id = ?", id, name, profile, SandboxPurposeSolo, threadID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errSandboxOperationLost
	}
	return nil
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
	receiptName := name
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
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		if err := s.commitSandboxOperation(cleanup, id, func(tx *gorm.DB) error {
			if err := guardSoloCreationIdentity(tx, id, receiptName, profile, threadID); err != nil {
				return err
			}
			return s.updateSandboxOperation(cleanup, tx, id, map[string]any{"status": "error", "error": truncErr(cause), "destroy_at": nil})
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
	if sb != nil {
		changed, retainErr := s.retainSoloCreateResult(ctx, id, name, profile, threadID, agent, authorization, sb)
		if retainErr != nil {
			// Keep the original correlation receipt if adoption lost its
			// physical owner/dataset; never claim a successor's resource.
			fail(fmt.Errorf("原环境创建结果待核对，绑定与工作区未删除"))
			return
		}
		receiptName = sb.Name
		if sb.IsLocal() {
			// Readiness may fail after a real local process started. Retain its
			// private stop receipt as well as SQL identity, so Close can stop
			// that exact launch without guessing from a directory or new PID.
			s.mu.Lock()
			s.live[id] = &liveSandbox{sb: sb, home: home, prepared: prepared, soloRouteAgent: &agent, modelBindingFingerprint: prepared.modelFingerprint, gitBindingsFingerprint: gitBindingsFingerprint(spec.GitCredentials)}
			s.mu.Unlock()
		}
		if changed {
			fail(fmt.Errorf("当前授权已变化，原环境身份已保留，停止状态待确认"))
			return
		}
	}
	if err != nil {
		if sb == nil && !errors.Is(err, sandbox.ErrCreateOutcomeUnknown) {
			_ = os.RemoveAll(home)
		}
		fail(fmt.Errorf("create sandbox: %w", err))
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
		// Recheck the original lease/dataset before stopping even this exact
		// known ID. A lost owner must not stop its successor's running guest.
		_, _ = s.retainSoloCreateResult(ctx, id, receiptName, profile, threadID, agent, authorization, sb)
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
