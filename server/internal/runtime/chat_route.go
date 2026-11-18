package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

const visitModelSwitchFailed = "model_switch_failed"

// User-selected routes update only the tool, while shared workflow node
// handoffs may still prepare repositories through sandboxReprimer.
type sandboxToolSwitcher interface {
	SwitchTool(context.Context, *sandbox.Sandbox, sandbox.Spec, []string) error
}

// routeBackend keeps the node Agent's tool and binding unless this turn's
// choice names a different bound group. Same-group model changes stay out of
// the sandbox fingerprint and are applied with POST /api/model.
func routeBackend(req NodeReq, fallback AcpBackend, binding *modelrouting.Binding) (AcpBackend, *modelrouting.Binding) {
	route := req.ChatRoute
	if route == nil || !route.Set {
		return fallback, binding
	}
	want := strings.TrimSpace(route.ConfigurationID)
	def := ""
	if binding != nil {
		def = strings.TrimSpace(binding.ConfigurationID)
	}
	if want == "" || want == def {
		return fallback, binding
	}
	next := fallback
	if parsed, err := ParseBackend(route.Backend); err == nil {
		next = parsed
	}
	return next, modelrouting.ConfigurationReference(want)
}

func (c *acpProvider) routeTool(req NodeReq) AcpBackend {
	backend, _ := routeBackend(req, c.backend, c.effectiveAgent(req).ModelBinding)
	return backend
}

func (c *acpProvider) defaultConfigurationID(req NodeReq) string {
	binding := c.effectiveAgent(req).ModelBinding
	if binding == nil {
		return ""
	}
	return strings.TrimSpace(binding.ConfigurationID)
}

// AlignChatRoute applies req to the parked visit session. A missing,
// disconnected or unreachable session reports aligned=false so the turn
// rebuilds the sandbox and aligns it there.
func (c *acpProvider) AlignChatRoute(ctx context.Context, req NodeReq) (string, bool, error) {
	notice, aligned, _, err := c.alignParkedRoute(ctx, req)
	return notice, aligned, err
}

// alignParkedRoute is AlignChatRoute that also reports lost=true when the
// parked sandbox stopped answering during alignment, so a turn can rebuild it.
func (c *acpProvider) alignParkedRoute(ctx context.Context, req NodeReq) (notice string, aligned, lost bool, err error) {
	if !req.ResetToDefaultModel && (req.ChatRoute == nil || !req.ChatRoute.Set) {
		return "", false, false, nil
	}
	key := reactKey(req)
	c.mu.Lock()
	sess := c.sessions[key]
	c.mu.Unlock()
	if sess == nil || sess.sb == nil || sess.acp == nil {
		return "", false, false, nil
	}
	if !sess.acp.IsConnected() {
		return "", false, true, nil
	}
	notice, err = c.alignVisitRoute(ctx, req, sess)
	if err != nil {
		if isSandboxUnreachable(sess, err) {
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).Msg("visit sandbox unreachable during model alignment")
			return "", false, true, nil
		}
		return "", true, false, err
	}
	return notice, true, false, nil
}

// errSandboxUnreachable reports that a route could not be applied because the
// parked sandbox no longer answers. It is not a model switch failure.
var errSandboxUnreachable = errors.New("visit sandbox unreachable")

// isSandboxUnreachable tells a lost sandbox apart from a switch the sandbox
// rejected: a dropped ACP connection, transport errors and proxy 502/503/504.
func isSandboxUnreachable(sess *reactSession, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errSandboxUnreachable) || errors.Is(err, sandbox.ErrConnClosed) || errors.Is(err, sandbox.ErrGatewayTransport) {
		return true
	}
	if sess != nil && sess.acp != nil && !sess.acp.IsConnected() {
		return true
	}
	var netErr net.Error
	var urlErr *url.Error
	if errors.As(err, &netErr) || errors.As(err, &urlErr) {
		return true
	}
	var chatErr *sandbox.ChatHTTPError
	if errors.As(err, &chatErr) && isGatewayUnavailable(chatErr.StatusCode) {
		return true
	}
	var gwErr *sandbox.GatewayHTTPError
	return errors.As(err, &gwErr) && isGatewayUnavailable(gwErr.StatusCode)
}

func isGatewayUnavailable(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func (c *acpProvider) alignVisitRoute(ctx context.Context, req NodeReq, sess *reactSession) (string, error) {
	if req.ResetToDefaultModel || req.ChatRoute == nil || !req.ChatRoute.Set {
		if err := c.restoreDefaultRoute(ctx, req, sess); err != nil {
			return "", err
		}
		if err := c.alignChatSettings(ctx, sess, "", "", ""); err != nil {
			return "", err
		}
		c.stampRouteExecution(sess, nil)
		return "", nil
	}
	if err := c.applyChatRoute(ctx, req, sess, req.ChatRoute); err != nil {
		if isSandboxUnreachable(sess, err) {
			return "", fmt.Errorf("%w: %w", errSandboxUnreachable, err)
		}
		if rerr := c.rollbackChatRoute(ctx, req, sess); rerr != nil {
			log.Error().Err(rerr).Str("run", req.RunID).Str("node", req.NodeID).Msg("visit model rollback failed")
		}
		return visitModelSwitchFailed, nil
	}
	c.stampRouteExecution(sess, req.ChatRoute)
	return "", nil
}

func (c *acpProvider) restoreDefaultRoute(ctx context.Context, req NodeReq, sess *reactSession) error {
	if sess.routeConfigurationID == "" || sess.routeConfigurationID == c.defaultConfigurationID(req) {
		sess.routeConfigurationID = ""
		return nil
	}
	bare := req
	bare.ChatRoute = nil
	bare.AppliedRoute = nil
	bare.ResetToDefaultModel = false
	want, err := c.spec(bare)
	if err != nil {
		return err
	}
	if err := c.switchSessionTool(ctx, req, sess, want); err != nil {
		return err
	}
	sess.routeConfigurationID = ""
	return nil
}

func (c *acpProvider) applyChatRoute(ctx context.Context, req NodeReq, sess *reactSession, route *ChatRoute) error {
	if route == nil {
		return fmt.Errorf("missing chat route")
	}
	wantID := strings.TrimSpace(route.ConfigurationID)
	defaultID := c.defaultConfigurationID(req)
	if wantID == "" {
		wantID = defaultID
	}
	currentID := sess.routeConfigurationID
	if currentID == "" {
		currentID = defaultID
	}
	if wantID != currentID {
		routed := req
		routed.ChatRoute = route
		routed.ResetToDefaultModel = false
		want, err := c.spec(routed)
		if err != nil {
			return err
		}
		if err := c.switchSessionTool(ctx, req, sess, want); err != nil {
			return err
		}
		if wantID == defaultID {
			sess.routeConfigurationID = ""
		} else {
			sess.routeConfigurationID = wantID
		}
	}
	return c.alignChatSettings(ctx, sess, route.ModelID, route.ReasoningEffort, route.ServiceTier)
}

func (c *acpProvider) rollbackChatRoute(ctx context.Context, req NodeReq, sess *reactSession) error {
	applied := req.AppliedRoute
	if applied == nil || !applied.Set {
		if err := c.restoreDefaultRoute(ctx, req, sess); err != nil {
			return err
		}
		c.stampRouteExecution(sess, nil)
		return c.alignChatSettings(ctx, sess, "", "", "")
	}
	if err := c.applyChatRoute(ctx, req, sess, applied); err != nil {
		return err
	}
	c.stampRouteExecution(sess, applied)
	return nil
}

func (c *acpProvider) resetLiveChatModel(ctx context.Context, sb *sandbox.Sandbox, acp *sandbox.ACPClient) error {
	if sb == nil || acp == nil {
		return nil
	}
	sess := &reactSession{sb: sb, acp: acp}
	if err := c.alignChatSettings(ctx, sess, "", "", ""); err != nil {
		return err
	}
	c.stampRouteExecution(sess, nil)
	return nil
}

func (c *acpProvider) alignChatSettings(ctx context.Context, sess *reactSession, model, effort, tier string) error {
	if sess == nil || sess.sb == nil {
		return fmt.Errorf("no sandbox")
	}
	chat := "default"
	if sess.acp != nil && strings.TrimSpace(sess.acp.ChatID()) != "" {
		chat = sess.acp.ChatID()
	}
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	tier = strings.TrimSpace(tier)
	cur, err := sandbox.GetChatModelSettingsFor(ctx, sess.sb.Host, sess.sb.Port, sess.sb.Password, chat)
	if err != nil {
		// An unread session is not the primary model. Reset and align both
		// fail so a shared sandbox cannot keep a chat-selected model.
		return err
	}
	var wantTier *string
	if cur.ServiceTier != nil {
		wantTier = &tier
	} else if tier != "" {
		return fmt.Errorf("runtime does not support service tiers")
	}
	if chatSettingsMatch(cur, model, effort, tier) {
		return nil
	}
	if err := sandbox.SetChatModelSettingsFor(ctx, sess.sb.Host, sess.sb.Port, sess.sb.Password, chat, model, effort, wantTier); err != nil {
		return err
	}
	return nil
}

func chatSettingsMatch(cur sandbox.ChatModelSettings, model, effort, tier string) bool {
	gotEffort := ""
	if cur.ReasoningEffort != nil {
		gotEffort = strings.TrimSpace(*cur.ReasoningEffort)
	}
	gotTier := ""
	if cur.ServiceTier != nil {
		gotTier = strings.TrimSpace(*cur.ServiceTier)
	}
	return strings.TrimSpace(cur.Selected) == strings.TrimSpace(model) && gotEffort == strings.TrimSpace(effort) && gotTier == strings.TrimSpace(tier)
}

func (c *acpProvider) switchSessionTool(ctx context.Context, req NodeReq, sess *reactSession, want sandbox.Spec) error {
	switcher, ok := c.mgr.(sandboxToolSwitcher)
	if !ok || sess == nil || sess.sb == nil {
		return fmt.Errorf("sandbox cannot switch tools in place")
	}
	if want.ConfigHome != "" && want.ConfigHome != sess.home {
		defer removeHome(want.ConfigHome)
	}
	prev := append([]string{}, sess.primedEnvKeys...)
	if len(prev) == 0 && c.shared != nil {
		prev = c.shared.primedEnvKeys(req.RunID, sess.sb)
	}
	if len(prev) == 0 {
		prev = append(ModelRoutingEnvKeys(), "AGENT_PROVIDER", "CONFIG_ROOT", "CODEX_HOME", EnvACPBridgeReasoningEffort)
	}
	chat := ""
	if sess.acp != nil {
		chat = sess.acp.ChatID()
		sess.acp.Close()
	}
	if err := switcher.SwitchTool(ctx, sess.sb, want, prev); err != nil {
		return err
	}
	if err := sandbox.WaitForACPReady(ctx, sess.sb.Host, sess.sb.Port, sess.sb.Password, reprimeReadyWait); err != nil {
		return err
	}
	next := sess.sb.ACP().
		WithChat(chat).
		WithSession(sess.sb.WorkspaceDir, c.mcpServers(req)).
		WithIdleTimeoutFunc(c.agentIdle).
		WithBridgeModel(want.Env[EnvACPBridgeModel]).
		WithExecution(c.executionForEnvironment(req, want.Env))
	if err := next.Connect(ctx); err != nil {
		return err
	}
	sess.acp = next
	sess.primedEnvKeys = sandbox.ReprimeEnvKeys(want)
	if c.shared != nil {
		c.shared.notePrimed(req.RunID, sess.sb, want)
	}
	return nil
}

func (c *acpProvider) stampRouteExecution(sess *reactSession, route *ChatRoute) {
	if sess == nil || sess.acp == nil {
		return
	}
	exec := sess.acp.Execution()
	if exec == nil {
		exec = &models.ChatExecution{Backend: string(c.backend)}
	}
	if route == nil || !route.Set {
		exec.ModelID = strings.TrimSpace(sess.acp.BridgeModel())
		exec.ReasoningEffort = ""
		exec.ServiceTier = ""
	} else {
		model := strings.TrimSpace(route.ModelID)
		if model == "" {
			model = strings.TrimSpace(sess.acp.BridgeModel())
		}
		exec.ModelID = model
		exec.ReasoningEffort = strings.TrimSpace(route.ReasoningEffort)
		exec.ServiceTier = strings.TrimSpace(route.ServiceTier)
		if id := strings.TrimSpace(route.ConfigurationID); id != "" {
			exec.ConfigurationID = id
		}
		if backend := strings.TrimSpace(route.Backend); backend != "" {
			exec.Backend = backend
		}
	}
	sess.acp = sess.acp.WithExecution(exec)
}
