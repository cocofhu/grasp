package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/execution"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// SandboxRecoveryPreparer regenerates configuration from persisted execution
// context and current credential resolvers. Its temporary ConfigHome belongs
// to the caller. It does not create a guest or send a model request.
type SandboxRecoveryPreparer interface {
	PrepareSandboxRecovery(NodeReq) (sandbox.Spec, error)
}

func (c *acpProvider) PrepareSandboxRecovery(req NodeReq) (sandbox.Spec, error) {
	spec, err := c.spec(req)
	if err != nil {
		return sandbox.Spec{}, err
	}
	// The archive contains the authoritative clones, including deletions and
	// uncommitted files. The freshly resolved Git catalog still supplies auth.
	sandbox.ApplyRepoEnv(spec.Env, nil)
	if route := req.ChatRoute; route != nil && route.Set {
		spec.Env[EnvInitialChatModel] = route.ModelID
		spec.Env[EnvACPBridgeReasoningEffort] = route.ReasoningEffort
	}
	return spec, nil
}

func (r *ProviderRegistry) PrepareSandboxRecovery(req NodeReq) (sandbox.Spec, error) {
	provider, err := r.providerFor(req)
	if err != nil {
		return sandbox.Spec{}, err
	}
	preparer, ok := provider.(SandboxRecoveryPreparer)
	if !ok {
		return sandbox.Spec{}, fmt.Errorf("execution backend does not support configuration recovery")
	}
	return preparer.PrepareSandboxRecovery(req)
}

// A WebSocket can still appear connected after an external pause. Resolve the
// saved environment before model alignment or the next human reply, including
// when the old in-memory dialogue has not observed the disconnect yet.
func (c *acpProvider) refreshParkedReact(ctx context.Context, req NodeReq, sess *reactSession, history []models.ReactMessage) (*reactSession, error) {
	if sess == nil || sess.acp == nil || !sess.acp.IsConnected() {
		return sess, nil
	}
	restored, found, err := c.restoreReactEnvironment(ctx, req, history)
	if err != nil {
		return nil, fmt.Errorf("恢复工作区失败，已保留恢复点：%w", err)
	}
	if found {
		return restored, nil
	}
	return sess, nil
}

// restoreReactEnvironment reconnects a saved environment before considering a
// fresh one. A failed restore is retryable and cannot destroy its recovery point.
func (c *acpProvider) restoreReactEnvironment(ctx context.Context, req NodeReq, history []models.ReactMessage) (*reactSession, bool, error) {
	if execution.Worktree() {
		return nil, false, nil
	}
	registry, ok := c.registry.(RunSandboxRecovery)
	if !ok {
		return nil, false, nil
	}
	sb, found, warm, err := registry.ResumeRunSandbox(ctx, req.RunID, req.NodeID)
	if err != nil {
		return nil, true, err
	}
	if !found {
		return nil, false, nil
	}
	sb.Password = req.Token
	layout := c.agentLayout(models.AgentProfile(req.Config), c.effectiveAgent(req))
	sb.ConfigRoot = layout.ConfigRoot
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		return nil, true, err
	}
	model, _ := sb.ExecScript(ctx, 10*time.Second, "bash", "printenv "+EnvACPBridgeModel+" || true")
	group, _ := sb.ExecScript(ctx, 10*time.Second, "bash", "printenv "+EnvModelConfigurationID+" || true")
	effort, _ := sb.ExecScript(ctx, 10*time.Second, "bash", "printenv "+EnvACPBridgeReasoningEffort+" || true")
	acp := sb.ACP().WithSession(sb.WorkspaceDir, c.mcpServers(req)).WithIdleTimeoutFunc(c.agentIdle).
		WithBridgeModel(strings.TrimSpace(model)).WithExecution(c.executionForEnvironment(req, map[string]string{EnvACPBridgeModel: strings.TrimSpace(model), EnvModelConfigurationID: strings.TrimSpace(group), EnvACPBridgeReasoningEffort: strings.TrimSpace(effort)}))
	if err := acp.Connect(ctx); err != nil {
		acp.Close()
		return nil, true, err
	}
	key := reactKey(req)
	c.mu.Lock()
	previous := c.sessions[key]
	c.live[key] = sb
	c.mu.Unlock()
	if previous != nil && previous.acp != nil {
		previous.acp.Close()
	}
	if !warm && len(history) > 0 {
		chatCtx, cancel := c.turnCtx(ctx, req)
		result, err := c.streamChat(chatCtx, acp, req, c.buildReactRehydratePrompt(req, c.upstreamArtifacts(req), history, sb), req.PromptImages)
		cancel()
		c.carryChatUsage(key, result)
		if err != nil {
			acp.Close()
			return nil, true, err
		}
	}
	c.registerReattachedCodexLogin(req, sb)
	c.adoptSharedSandbox(req, sb)
	return c.parkReactSession(req, sb, acp, ""), true, nil
}

func (c *acpProvider) CancelRunResources(ctx context.Context, runID string) error {
	if registry, ok := c.registry.(interface {
		CancelRunSandboxes(context.Context, string) error
	}); ok {
		return registry.CancelRunSandboxes(ctx, runID)
	}
	return nil
}
