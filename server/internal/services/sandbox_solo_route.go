package services

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// soloRouteOps keeps the workspace-preserving operation separate from session
// selection. The Solo FIFO invokes it only before a new turn starts.
type soloRouteOps interface {
	SwitchTool(context.Context, *sandbox.Sandbox, sandbox.Spec, []string) error
	Ready(context.Context, *sandbox.Sandbox) error
	Connect(context.Context, *sandbox.Sandbox, []sandbox.MCPServerSpec) (*sandbox.ACPClient, error)
}

type liveSoloRouteOps struct{ manager *sandbox.Manager }

func (o liveSoloRouteOps) SwitchTool(ctx context.Context, sb *sandbox.Sandbox, spec sandbox.Spec, keys []string) error {
	return o.manager.SwitchTool(ctx, sb, spec, keys)
}

func (liveSoloRouteOps) Ready(ctx context.Context, sb *sandbox.Sandbox) error {
	return sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second)
}

func (liveSoloRouteOps) Connect(ctx context.Context, sb *sandbox.Sandbox, specs []sandbox.MCPServerSpec) (*sandbox.ACPClient, error) {
	acp := sb.ACP().WithSession(sb.EffectiveWorkspaceDir(), mcpServersJSON(specs))
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		return nil, err
	}
	return acp, nil
}

func (s *SandboxService) soloPlatformSpecs(row *models.Sandbox) []sandbox.MCPServerSpec {
	base := strings.TrimRight(config.ResolveMCPAdvertise(s.mcpEndpoint), "/")
	if base == "" {
		return nil
	}
	return []sandbox.MCPServerSpec{{
		Name: ArtifactStoreMCP, URL: base + "/mcp/runs/" + url.PathEscape(row.RunID),
		Headers: map[string]string{"Authorization": "Bearer " + row.Token},
	}}
}

// appliedSoloAgent resolves the route the container actually uses. Current
// selection may already point at a later turn, so it must never be used here.
func (s *SandboxService) appliedSoloAgent(row *models.Sandbox, ls *liveSandbox, agent Agent) (Agent, error) {
	configurationID := ""
	if ls != nil && ls.soloRouteAgent != nil && ls.soloRouteAgent.ModelBinding != nil {
		configurationID = ls.soloRouteAgent.ModelBinding.ConfigurationID
	} else {
		var session models.SoloSession
		if s.db.Where("id = ? AND sandbox_id = ?", row.ThreadID, row.ID).First(&session).Error == nil {
			configurationID = session.AppliedConfigurationID
			if configurationID == "" {
				configurationID = session.InitialConfigurationID
			}
		}
	}
	if agent.ModelBinding != nil && (configurationID == "" || configurationID == agent.ModelBinding.ConfigurationID) {
		agent.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: agent.ModelBinding.ConfigurationID}
		return agent, nil
	}
	return selectSoloConfiguration(s.db, agent, configurationID)
}

func (s *SandboxService) routeImage(backend string) string {
	image := resolveSandboxImage(backend)
	if image == "" && s.mgr != nil {
		image = s.mgr.Image
	}
	return image
}

// AlignSoloModelRoute replaces only the running coding tool and its config.
// It retains the sandbox, repository files, pending filesystem changes and MCP
// namespace. A cold resume must validate the previously applied grant first.
func (s *SandboxService) AlignSoloModelRoute(ctx context.Context, id uint, agent Agent) (bool, error) {
	ctx, release, err := s.acquireSandboxOperation(ctx, id)
	if err != nil {
		return false, err
	}
	defer release()
	var row models.Sandbox
	if err := s.db.First(&row, id).Error; err != nil {
		return false, fmt.Errorf("sandbox not found")
	}
	if row.Purpose != SandboxPurposeSolo || row.Status != "running" {
		return false, fmt.Errorf("只能切换运行中的 Solo 沙箱")
	}
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(agent)
	if err != nil {
		return false, err
	}
	authorization, err := s.currentAuthorizationFingerprint(ctx, agent)
	if err != nil {
		return false, err
	}
	// Model switching is not permission to refresh unrelated Git grants.
	if row.GitBindingsFingerprint != gitFingerprint {
		return false, fmt.Errorf("Git 授权已变更或尚未同步，请重新打开沙箱")
	}
	s.mu.Lock()
	ls := s.live[id]
	if ls != nil && (ls.busy || ls.switching) {
		s.mu.Unlock()
		return false, fmt.Errorf("sandbox busy")
	}
	if modelFingerprint == row.ModelBindingFingerprint {
		s.mu.Unlock()
		if row.AuthorizationFingerprint != "" && row.AuthorizationFingerprint != authorization {
			return false, fmt.Errorf("当前环境授权已变化，请保留工作区并重新核验环境")
		}
		return false, nil
	}
	if ls == nil {
		ls = &liveSandbox{}
		s.live[id] = ls
	}
	ls.switching = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); ls.switching = false; s.mu.Unlock() }()

	profile, ok := s.skills.Get(row.Profile)
	if !ok {
		return false, fmt.Errorf("Agent %q 不存在", row.Profile)
	}
	var session models.SoloSession
	if err := s.db.Where("id = ? AND sandbox_id = ?", row.ThreadID, row.ID).First(&session).Error; err != nil {
		return false, fmt.Errorf("Solo 会话与当前沙箱不匹配")
	}
	previousKnown := true
	var previousAgent Agent
	if ls.soloRouteAgent != nil {
		// A removed/disabled old group must not prevent choosing a valid one.
		// The live snapshot records the environment that is actually running.
		previousAgent = *ls.soloRouteAgent
	} else {
		previousAgent, err = s.appliedSoloAgent(&row, ls, s.effectiveAgent(profile))
		if err != nil {
			// A valid new route can still recover a retained workspace after the
			// old group was removed. Its old secrets may no longer be resolvable,
			// in which case forward switching is allowed but rollback is not.
			previousAgent = s.effectiveAgent(profile)
			previousKnown = false
		}
	}
	if ls.sb == nil {
		if s.mgr == nil {
			return false, fmt.Errorf("sandbox manager unavailable")
		}
		attached, attachErr := s.mgr.AttachNoEnsure(ctx, row.Name)
		err = attachErr
		if err != nil {
			return false, fmt.Errorf("attach: %w", err)
		}
		attached.Password = row.Token
		if previousAgent.Layout.WorkspaceDir != "" {
			attached.WorkspaceDir = previousAgent.Layout.WorkspaceDir
		}
		attached.ConfigRoot = previousAgent.Layout.ConfigRoot
		s.mu.Lock()
		ls.sb = attached
		s.mu.Unlock()
	}
	runningImage := ls.sb.Image
	if runningImage == "" && ls.prepared != nil {
		runningImage = ls.prepared.spec.Image
	}
	if runningImage == "" && previousKnown {
		runningImage = s.routeImage(previousAgent.AcpBackend)
	}
	if !ls.sb.IsLocal() && runningImage != "" && runningImage != s.routeImage(agent.AcpBackend) {
		return false, fmt.Errorf("目标编码工具使用不同的沙箱镜像，当前工作区无法原地切换")
	}
	if !ls.sb.IsLocal() && agent.Layout.WorkspaceDir != "" && agent.Layout.WorkspaceDir != ls.sb.WorkspaceDir {
		return false, fmt.Errorf("目标工具的工作目录与当前会话不一致")
	}
	platformSpecs := s.soloPlatformSpecs(&row)
	extra := agentContainerExtra{Resident: true, LaunchKind: "solo", Repos: SoloRepoSpecsList(effectiveSessionRepos(&session)), InitialChatModel: chatModelID(&session), InitialChatReasoningEffort: &session.CurrentReasoningEffort}
	selectedConfiguration := chatConfigurationID(&session)
	if selectedConfiguration == "" {
		if original := s.effectiveAgent(profile); original.ModelBinding != nil {
			selectedConfiguration = original.ModelBinding.ConfigurationID
		}
	}
	if agent.ModelBinding != nil && selectedConfiguration != agent.ModelBinding.ConfigurationID {
		// A choice made after this turn took its route snapshot belongs to a
		// later turn. Never boot that new model under this turn's older group.
		extra.InitialChatModel = ""
		extra.InitialChatReasoningEffort = nil
	}
	prepared, err := s.prepareAgentContainer(row.Profile, row.ThreadID, row.RunID, row.Token, row.Name, platformSpecs, agent, extra)
	if err != nil {
		return false, err
	}
	if gitBindingsFingerprint(prepared.spec.GitCredentials) != row.GitBindingsFingerprint {
		_ = os.RemoveAll(prepared.spec.ConfigHome)
		return false, fmt.Errorf("Git 授权已变更或尚未同步，请重新打开沙箱")
	}
	modelFingerprint = prepared.modelFingerprint
	rollbackAvailable := true
	previous := ls.prepared
	if previous == nil {
		if previousKnown {
			extra.InitialChatModel = appliedChatModelID(&session)
			extra.InitialChatReasoningEffort = &session.AppliedReasoningEffort
			previous, err = s.prepareAgentContainer(row.Profile, row.ThreadID, row.RunID, row.Token, row.Name, platformSpecs, previousAgent, extra)
		}
		if previous == nil || err != nil {
			rollbackAvailable = false
			previous = &preparedAgentContainer{spec: sandbox.Spec{ConfigRoot: ls.sb.ConfigRoot}}
		} else {
			// Retain a reconstructed rollback bundle for the live sandbox.
			s.mu.Lock()
			ls.prepared = previous
			ls.home = previous.spec.ConfigHome
			s.mu.Unlock()
		}
	}
	previous = soloPreparedSettings(previous, appliedChatModelID(&session), session.AppliedReasoningEffort)
	previousAuthorization, previousErr := s.currentAuthorizationFingerprint(ctx, previousAgent)
	if previousErr != nil || row.AuthorizationFingerprint == "" || previousAuthorization != row.AuthorizationFingerprint {
		rollbackAvailable = false
	}
	ops := s.soloRouteRuntime
	if ops == nil {
		ops = liveSoloRouteOps{s.mgr}
	}
	// Persist the handoff obligation before native-session state can be lost.
	// A process exit between re-prime and reconnect must not skip history when
	// the retained sandbox is attached on the next request.
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireSoloRecoveryParent(tx, &row, soloRecoveryGrant{session: session}); err != nil {
			return err
		}
		return tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ?", row.ThreadID, id).Update("model_context_pending", true).Error
	}); err != nil {
		_ = os.RemoveAll(prepared.spec.ConfigHome)
		return false, fmt.Errorf("保存上下文交接状态失败: %w", err)
	}
	if ls.acp != nil {
		ls.acp.Close()
	}
	apply := func(applyCtx context.Context, target, old *preparedAgentContainer) (*sandbox.ACPClient, error) {
		if err := requireSoloRecoveryParent(s.db.WithContext(applyCtx), &row, soloRecoveryGrant{session: session}); err != nil {
			return nil, err
		}
		selectedAgent, expected := agent, authorization
		if target == previous {
			selectedAgent, expected = previousAgent, previousAuthorization
		}
		current, grantErr := s.currentAuthorizationFingerprint(applyCtx, selectedAgent)
		if grantErr != nil || current != expected {
			return nil, fmt.Errorf("切换期间授权已变化，未应用旧凭据")
		}
		keys := append(sandbox.ReprimeEnvKeys(old.spec), runtime.ModelRoutingEnvKeys()...)
		keys = append(keys, "AGENT_PROVIDER", "CONFIG_ROOT", "CODEX_HOME", runtime.EnvACPBridgeReasoningEffort)
		if err := ops.SwitchTool(applyCtx, ls.sb, target.spec, keys); err != nil {
			return nil, err
		}
		if err := ops.Ready(applyCtx, ls.sb); err != nil {
			return nil, err
		}
		current, grantErr = s.currentAuthorizationFingerprint(applyCtx, selectedAgent)
		if grantErr != nil || current != expected {
			return nil, fmt.Errorf("工具启动期间授权已变化，未连接旧授权会话")
		}
		acp, err := ops.Connect(applyCtx, ls.sb, target.specs)
		if err != nil {
			return nil, err
		}
		return acp, nil
	}
	acp, err := apply(ctx, prepared, previous)
	if err != nil {
		if !rollbackAvailable {
			s.invalidateSoloRoute(ctx, id, ls)
			_ = os.RemoveAll(prepared.spec.ConfigHome)
			return false, fmt.Errorf("切换工具失败，原配置已失效；工作区已保留，请重新选择模型重试: %w", err)
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		restored, rollbackErr := apply(rollbackCtx, previous, prepared)
		cancel()
		s.mu.Lock()
		ls.acp = restored
		s.mu.Unlock()
		if rollbackErr == nil {
			rollbackErr = s.rememberSoloEndpoint(id, ls.sb)
		}
		_ = os.RemoveAll(prepared.spec.ConfigHome)
		if rollbackErr != nil {
			s.invalidateSoloRoute(ctx, id, ls)
			return false, fmt.Errorf("切换工具失败，恢复原配置也失败: %w", err)
		}
		return false, fmt.Errorf("切换工具失败，已恢复原配置: %w", err)
	}
	currentAuthorization, grantErr := s.currentAuthorizationFingerprint(ctx, agent)
	if grantErr != nil || currentAuthorization != authorization {
		acp.Close()
		s.invalidateSoloRoute(ctx, id, ls)
		_ = os.RemoveAll(prepared.spec.ConfigHome)
		return false, s.blockSoloRecoveryAuthorization(ctx, &row, soloRecoveryGrant{session: session}, fmt.Errorf("工具连接期间授权已变化，未发布运行状态"))
	}
	if err := s.commitSandboxOperationPrepared(ctx, id, func(tx *gorm.DB) error {
		return requireSoloRecoveryParent(tx, &row, soloRecoveryGrant{session: session})
	}, func(tx *gorm.DB) error {
		if err := s.updateSandboxOperation(ctx, tx, id, map[string]any{"model_binding_fingerprint": modelFingerprint, "authorization_fingerprint": authorization, "host": ls.sb.Host, "acp_port": ls.sb.Port}); err != nil {
			return err
		}
		return tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ?", row.ThreadID, id, session.LifecycleEpoch, session.LifecycleAttemptID).
			Updates(map[string]any{"model_context_pending": true, "applied_configuration_id": agent.ModelBinding.ConfigurationID}).Error
	}); err != nil {
		acp.Close()
		if soloLifecycleObsolete(err) {
			s.invalidateSoloRoute(ctx, id, ls)
			_ = os.RemoveAll(prepared.spec.ConfigHome)
			return false, err
		}
		if !rollbackAvailable {
			s.invalidateSoloRoute(ctx, id, ls)
			_ = os.RemoveAll(prepared.spec.ConfigHome)
			return false, fmt.Errorf("保存切换状态失败，原配置已失效；工作区已保留，请重试: %w", err)
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		restored, rollbackErr := apply(rollbackCtx, previous, prepared)
		cancel()
		s.mu.Lock()
		ls.acp = restored
		s.mu.Unlock()
		if rollbackErr == nil {
			rollbackErr = s.rememberSoloEndpoint(id, ls.sb)
		}
		_ = os.RemoveAll(prepared.spec.ConfigHome)
		if rollbackErr != nil {
			s.invalidateSoloRoute(ctx, id, ls)
			return false, fmt.Errorf("保存切换状态失败，恢复原配置失败: %w", err)
		}
		return false, fmt.Errorf("保存切换状态失败，已恢复原配置: %w", err)
	}
	oldHome := ls.home
	s.mu.Lock()
	ls.acp, ls.prepared, ls.home = acp, prepared, prepared.spec.ConfigHome
	ls.soloRouteAgent = &agent
	ls.modelBindingFingerprint = modelFingerprint
	s.mu.Unlock()
	s.registerLiveCodex(id, ls.sb, prepared.codexCredentialID, codexLiveAuthPath(prepared.backend, prepared.spec.ConfigRoot), prepared.codexVersion)
	if oldHome != "" && oldHome != ls.home {
		_ = os.RemoveAll(oldHome)
	}
	return true, nil
}

func (s *SandboxService) rememberSoloEndpoint(id uint, sb *sandbox.Sandbox) error {
	return s.db.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{"host": sb.Host, "acp_port": sb.Port}).Error
}

func soloPreparedSettings(prepared *preparedAgentContainer, model, effort string) *preparedAgentContainer {
	out := *prepared
	out.spec.Env = maps.Clone(prepared.spec.Env)
	if out.spec.Env == nil {
		out.spec.Env = map[string]string{}
	}
	if model == "" {
		delete(out.spec.Env, runtime.EnvInitialChatModel)
	} else {
		out.spec.Env[runtime.EnvInitialChatModel] = model
	}
	if effort == "" {
		delete(out.spec.Env, runtime.EnvACPBridgeReasoningEffort)
	} else {
		out.spec.Env[runtime.EnvACPBridgeReasoningEffort] = effort
	}
	return &out
}

// An incomplete rollback leaves the actual tool uncertain. Force the next
// attempt to re-apply a complete route rather than trusting its old fingerprint.
func (s *SandboxService) invalidateSoloRoute(ctx context.Context, id uint, ls *liveSandbox) {
	s.mu.Lock()
	ls.modelBindingFingerprint = ""
	s.mu.Unlock()
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = s.commitSandboxOperation(commitCtx, id, func(tx *gorm.DB) error {
		return s.updateSandboxOperation(commitCtx, tx, id, map[string]any{"model_binding_fingerprint": "", "authorization_fingerprint": ""})
	})
}
