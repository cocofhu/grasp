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
	Reprime(context.Context, *sandbox.Sandbox, sandbox.Spec, []string) error
	Ready(context.Context, *sandbox.Sandbox) error
	Connect(context.Context, *sandbox.Sandbox, []sandbox.MCPServerSpec) (*sandbox.ACPClient, error)
}

type liveSoloRouteOps struct{ manager *sandbox.Manager }

func (o liveSoloRouteOps) Reprime(ctx context.Context, sb *sandbox.Sandbox, spec sandbox.Spec, keys []string) error {
	return o.manager.Reprime(ctx, sb, spec, keys)
}

func (liveSoloRouteOps) Ready(ctx context.Context, sb *sandbox.Sandbox) error {
	return sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second)
}

func (liveSoloRouteOps) Connect(ctx context.Context, sb *sandbox.Sandbox, specs []sandbox.MCPServerSpec) (*sandbox.ACPClient, error) {
	acp := sb.ACP().WithSession(sb.WorkspaceDir, mcpServersJSON(specs))
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
// namespace. It is deliberately separate from Solo.Restart, which reclones.
func (s *SandboxService) AlignSoloModelRoute(ctx context.Context, id uint, agent Agent) (bool, error) {
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
		attached, attachErr := s.mgr.Attach(ctx, row.Name)
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
	ops := s.soloRouteRuntime
	if ops == nil {
		ops = liveSoloRouteOps{s.mgr}
	}
	// Persist the handoff obligation before native-session state can be lost.
	// A process exit between re-prime and reconnect must not skip history when
	// the retained sandbox is attached on the next request.
	if err := s.db.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ?", row.ThreadID, id).Update("model_context_pending", true).Error; err != nil {
		_ = os.RemoveAll(prepared.spec.ConfigHome)
		return false, fmt.Errorf("保存上下文交接状态失败: %w", err)
	}
	if ls.acp != nil {
		ls.acp.Close()
	}
	apply := func(applyCtx context.Context, target, old *preparedAgentContainer) (*sandbox.ACPClient, error) {
		keys := append(sandbox.ReprimeEnvKeys(old.spec), runtime.ModelRoutingEnvKeys()...)
		keys = append(keys, "AGENT_PROVIDER", "CONFIG_ROOT", "CODEX_HOME", runtime.EnvACPBridgeReasoningEffort)
		if err := ops.Reprime(applyCtx, ls.sb, target.spec, keys); err != nil {
			return nil, err
		}
		if err := ops.Ready(applyCtx, ls.sb); err != nil {
			return nil, err
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
			s.invalidateSoloRoute(id, ls)
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
			s.invalidateSoloRoute(id, ls)
			return false, fmt.Errorf("切换工具失败，恢复原配置也失败: %w", err)
		}
		return false, fmt.Errorf("切换工具失败，已恢复原配置: %w", err)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Sandbox{}).Where("id = ?", id).Updates(map[string]any{"model_binding_fingerprint": modelFingerprint, "host": ls.sb.Host, "acp_port": ls.sb.Port}).Error; err != nil {
			return err
		}
		return tx.Model(&models.SoloSession{}).Where("id = ? AND sandbox_id = ?", row.ThreadID, id).Update("model_context_pending", true).Error
	}); err != nil {
		acp.Close()
		if !rollbackAvailable {
			s.invalidateSoloRoute(id, ls)
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
			s.invalidateSoloRoute(id, ls)
			return false, fmt.Errorf("保存切换状态失败，恢复原配置失败: %w", err)
		}
		return false, fmt.Errorf("保存切换状态失败，已恢复原配置: %w", err)
	}
	oldHome := ls.home
	s.mu.Lock()
	ls.acp, ls.prepared, ls.home = acp, prepared, prepared.spec.ConfigHome
	ls.soloRouteAgent = &agent
	ls.modelBindingFingerprint = modelFingerprint
	ls.codexAuthInjected = prepared.codexLogin
	ls.codexAuthCredentialID = runtime.ModelCredentialIDForWriteBack(prepared.spec.Env)
	ls.codexAuthPath = codexLiveAuthPath(prepared.backend, prepared.spec.ConfigRoot)
	ls.codexAuthRejected = false
	s.mu.Unlock()
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
func (s *SandboxService) invalidateSoloRoute(id uint, ls *liveSandbox) {
	s.mu.Lock()
	ls.modelBindingFingerprint = ""
	s.mu.Unlock()
	_ = s.db.Model(&models.Sandbox{}).Where("id = ?", id).Update("model_binding_fingerprint", "").Error
}
