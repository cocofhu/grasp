package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

const visitModelSwitchFailed = "model_switch_failed"

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

// AlignChatRoute applies req to the parked visit session. A missing session
// reports aligned=false so the caller can try again after the sandbox exists.
func (c *acpProvider) AlignChatRoute(ctx context.Context, req NodeReq) (string, bool, error) {
	if !req.ResetToDefaultModel && (req.ChatRoute == nil || !req.ChatRoute.Set) {
		return "", false, nil
	}
	key := reactKey(req)
	c.mu.Lock()
	sess := c.sessions[key]
	c.mu.Unlock()
	if sess == nil || sess.sb == nil || sess.acp == nil {
		return "", false, nil
	}
	notice, err := c.alignVisitRoute(ctx, req, sess)
	if err != nil {
		return "", true, err
	}
	return notice, true, nil
}

func (c *acpProvider) alignVisitRoute(ctx context.Context, req NodeReq, sess *reactSession) (string, error) {
	if req.ResetToDefaultModel || req.ChatRoute == nil || !req.ChatRoute.Set {
		if err := c.restoreDefaultRoute(ctx, req, sess); err != nil {
			return "", err
		}
		if err := c.alignChatSettings(ctx, sess, "", ""); err != nil {
			return "", err
		}
		c.stampRouteExecution(sess, nil)
		return "", nil
	}
	if err := c.applyChatRoute(ctx, req, sess, req.ChatRoute); err != nil {
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
	if sess.sb.Image != "" && want.Image != "" && sess.sb.Image != want.Image && !sess.sb.IsLocal() {
		return fmt.Errorf("default model group requires a different sandbox image")
	}
	if err := c.reprimeSession(ctx, req, sess, want); err != nil {
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
		if sess.sb.Image != "" && want.Image != "" && sess.sb.Image != want.Image && !sess.sb.IsLocal() {
			return fmt.Errorf("model group requires a different sandbox image")
		}
		if err := c.reprimeSession(ctx, req, sess, want); err != nil {
			return err
		}
		if wantID == defaultID {
			sess.routeConfigurationID = ""
		} else {
			sess.routeConfigurationID = wantID
		}
	}
	return c.alignChatSettings(ctx, sess, route.ModelID, route.ReasoningEffort)
}

func (c *acpProvider) rollbackChatRoute(ctx context.Context, req NodeReq, sess *reactSession) error {
	applied := req.AppliedRoute
	if applied == nil || !applied.Set {
		if err := c.restoreDefaultRoute(ctx, req, sess); err != nil {
			return err
		}
		c.stampRouteExecution(sess, nil)
		return c.alignChatSettings(ctx, sess, "", "")
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
	if err := c.alignChatSettings(ctx, sess, "", ""); err != nil {
		return err
	}
	c.stampRouteExecution(sess, nil)
	return nil
}

func (c *acpProvider) alignChatSettings(ctx context.Context, sess *reactSession, model, effort string) error {
	if sess == nil || sess.sb == nil {
		return fmt.Errorf("no sandbox")
	}
	chat := "default"
	if sess.acp != nil && strings.TrimSpace(sess.acp.ChatID()) != "" {
		chat = sess.acp.ChatID()
	}
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	cur, err := sandbox.GetChatModelSettingsFor(ctx, sess.sb.Host, sess.sb.Port, sess.sb.Password, chat)
	if err != nil {
		if model == "" && effort == "" {
			log.Warn().Err(err).Str("chat", chat).Msg("reset chat model: settings unread")
			return nil
		}
		return err
	}
	if chatSettingsMatch(cur, model, effort) {
		return nil
	}
	if err := sandbox.SetChatModelSettingsFor(ctx, sess.sb.Host, sess.sb.Port, sess.sb.Password, chat, model, effort); err != nil {
		return err
	}
	return nil
}

func chatSettingsMatch(cur sandbox.ChatModelSettings, model, effort string) bool {
	gotEffort := ""
	if cur.ReasoningEffort != nil {
		gotEffort = strings.TrimSpace(*cur.ReasoningEffort)
	}
	return strings.TrimSpace(cur.Selected) == strings.TrimSpace(model) && gotEffort == strings.TrimSpace(effort)
}

func (c *acpProvider) reprimeSession(ctx context.Context, req NodeReq, sess *reactSession, want sandbox.Spec) error {
	reprime, ok := c.mgr.(sandboxReprimer)
	if !ok || sess == nil || sess.sb == nil {
		return fmt.Errorf("sandbox cannot be re-primed")
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
	if err := reprime.Reprime(ctx, sess.sb, want, prev); err != nil {
		return err
	}
	if err := sandbox.WaitForACPReady(ctx, sess.sb.Host, sess.sb.Port, sess.sb.Password, reprimeReadyWait); err != nil {
		return err
	}
	if want.Image != "" {
		sess.sb.Image = want.Image
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
	} else {
		model := strings.TrimSpace(route.ModelID)
		if model == "" {
			model = strings.TrimSpace(sess.acp.BridgeModel())
		}
		exec.ModelID = model
		exec.ReasoningEffort = strings.TrimSpace(route.ReasoningEffort)
		if id := strings.TrimSpace(route.ConfigurationID); id != "" {
			exec.ConfigurationID = id
		}
		if backend := strings.TrimSpace(route.Backend); backend != "" {
			exec.Backend = backend
		}
	}
	sess.acp = sess.acp.WithExecution(exec)
}
