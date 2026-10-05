package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

// ReactOpen launches a sandbox and parks the clarify session without
// chatting: the first LLM turn is the user's first message, injected in
// ReactReply together with the opening contract.
func (c *acpProvider) ReactOpen(ctx context.Context, req NodeReq) (out ReactTurn) {
	defer c.drainCarriedUsage(reactKey(req), &out)
	n := c.sandboxAttempts()
	for attempt := 1; ; attempt++ {
		c.host.ClearOutcome(req.RunID, req.NodeID)
		c.host.SetOutcomeAllowed(req.RunID, false)
		sb, acp, home, err := c.openSandbox(ctx, req)
		if err == nil {
			c.parkReactSession(req, sb, acp, home)
			return ReactTurn{}
		}
		if isRetryableSandboxErr(err) && attempt < n && ctx.Err() == nil {
			c.emitRetryNotice(req, attempt, n, err)
			if c.backoff(ctx, attempt) {
				continue
			}
		}
		return ReactTurn{SetupErr: err, Msg: "(沙箱启动失败,无法开始澄清:" + err.Error() + ")",
			Events: []models.AcpEvent{{Kind: "message", Text: "react open failed: " + err.Error()}}}
	}
}

// rehydrateReact rebuilds a lost react session — server restart dropped the
// in-memory session, or its sandbox/ACP died during the human's think-time — in
// a fresh sandbox, re-priming the agent with the persisted Q&A transcript so it
// can continue coherently (its private reasoning is gone, but the visible
// dialogue is restored). Returns nil (sandbox cleaned up) when the rebuild
// itself fails, so the caller surfaces a retryable state rather than silently
// finishing the node with an empty deliverable.
func (c *acpProvider) rehydrateReact(ctx context.Context, req NodeReq, history []models.ReactMessage) *reactSession {
	n := c.sandboxAttempts()
	seeded := c.upstreamArtifacts(req)
	for attempt := 1; ; attempt++ {
		sb, acp, home, err := c.openSandbox(ctx, req)
		if err == nil {
			if req.Caps.Clarify() && !clarifyHasOpenedTurn(history) {
				_ = takeClarifyPending(c.host, req.RunID, req.NodeID)
				sess := c.parkReactSession(req, sb, acp, home)
				log.Info().Str("run", req.RunID).Str("node", req.NodeID).
					Msg("clarify session rehydrated without priming chat")
				return sess
			}
			chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
			var primed *sandbox.ChatResult
			primed, err = c.streamChat(chatCtx, acp, req, c.buildReactRehydratePrompt(req, seeded, history), req.PromptImages)
			cancel()
			c.carryChatUsage(reactKey(req), primed)
			if err == nil {

				_ = takeClarifyPending(c.host, req.RunID, req.NodeID)
				sess := c.parkReactSession(req, sb, acp, home)
				log.Info().Str("run", req.RunID).Str("node", req.NodeID).
					Msg("react session rehydrated in a fresh sandbox")
				return sess
			}
			c.discardSandbox(ctx, req, sb, acp, home, nil)
			if !isRetryableSandboxErr(err) {
				log.Warn().Err(err).Str("node", req.NodeID).Msg("react rehydrate priming failed")
				return nil
			}
		}
		if isRetryableSandboxErr(err) && attempt < n && ctx.Err() == nil {
			c.emitRetryNotice(req, attempt, n, err)
			if c.backoff(ctx, attempt) {
				continue
			}
		}
		log.Warn().Err(err).Str("node", req.NodeID).Msg("react rehydrate failed")
		return nil
	}
}

// ReactReply advances the live dialogue. The clarification finishes when the
// agent raises no further questions (pending ask_question always pauses, even
// under force/max_rounds). Only then is the produces contract ensured (with
// re-prompting) and the sandbox torn down.
func (c *acpProvider) ReactReply(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) (out ReactTurn) {
	var chatOp string
	defer func() {
		if out.OpID == "" && chatOp != "" {
			out.OpID = chatOp
		}
		c.drainCarriedUsage(reactKey(req), &out)
	}()
	key := reactKey(req)
	c.mu.Lock()
	sess := c.sessions[key]
	c.mu.Unlock()

	if sess == nil || sess.acp == nil || !sess.acp.IsConnected() {
		if sess != nil {

			c.mu.Lock()
			delete(c.sessions, key)
			c.mu.Unlock()
			c.discardSandbox(ctx, req, sess.sb, sess.acp, sess.home, nil)
		}

		prior := history
		if len(prior) > 0 && prior[len(prior)-1].Role == "human" {
			prior = prior[:len(prior)-1]
		}
		sess = c.rehydrateReact(ctx, req, prior)
		if sess == nil {

			return ReactTurn{Msg: "(澄清会话已失效,自动重建沙箱失败,请稍后重试)", Done: false, Err: errors.New("澄清会话重建失败")}
		}
	}

	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	defer cancel()
	// Confirm (force) exposes node_complete before the confirm turn so
	// tools/list includes it and tools/call can succeed; the dialogue hides it.
	if req.Caps.Clarify() {
		c.host.SetOutcomeAllowed(req.RunID, force)
	}
	prompt := human
	chatImages := images
	if clarifyInjectOpenPrompt(req, history) {
		prompt = c.buildReactOpenPrompt(req, c.upstreamArtifacts(req)) + "\n\n## 用户消息\n" + strings.TrimRight(human, "\n")
		chatImages = mergePromptImages(req.PromptImages, images)
		if force {
			prompt = c.reactConfirmPrefix(req) + "\n\n" + prompt
		}
	} else if force {
		// Human confirmed: reconcile products against the transcript and call
		// node_complete. When the required products are already settled, the
		// prefix also tells the agent a no-op rewrite is unnecessary.
		prompt = c.reactConfirmPrefix(req) + "\n\n" + strings.TrimRight(human, "\n")
	}
	if extra := liveVariantPromptExtras(req, human); extra != "" {
		prompt = extra + "\n" + prompt
	}
	res, err := c.streamChat(chatCtx, sess.acp, req, prompt, chatImages)
	if res != nil && res.OpID != "" {
		chatOp = res.OpID
	}
	if err != nil {
		log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
			Msg("react reply chat failed")
		return ReactTurn{
			Msg: "(澄清回复失败:" + err.Error() + ")", Err: err,
			Events: []models.AcpEvent{{
				Kind: "message", Text: "react reply chat failed: " + err.Error(),
			}},
			Interrupted: isTurnTimeoutErr(err),
		}
	}
	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel
	var events []models.AcpEvent
	absorbChat(&usage, &usageByModel, &events, res)
	narration := res.Narration
	handoffs := handoffNarrations(res)
	withHandoffs := func(t ReactTurn) ReactTurn {
		t.Handoffs = handoffs
		return t
	}

	pending := takeClarifyPending(c.host, req.RunID, req.NodeID)

	if pending.any() {
		return withHandoffs(ReactTurn{Msg: narration, Questions: pending.Questions, Forms: pending.Forms, Events: events, Usage: usage, UsageByModel: usageByModel})
	}

	// Provider failed after prompt_done (quota / 4xx): surface the real error
	// on the failure card and keep the dialogue open for Retry.
	if fail := chatFailure(res); fail != "" {
		msg := withFailureBanner(narration, "澄清回复失败", fail)
		c.host.ClearOutcome(req.RunID, req.NodeID)
		events = c.snapshotEvents(ctx, sess.sb, events)
		events = append(events, models.AcpEvent{Kind: "message", Text: "react reply chat failed: " + fail})
		return withHandoffs(ReactTurn{Msg: msg, Done: false, Err: errors.New(fail), Events: events, Usage: usage, UsageByModel: usageByModel,
			Interrupted: res.Interrupted})
	}

	if !force {
		// Empty narration with no questions: keep the dialogue open as an empty
		// failure — do not Done / finishReact / node-failed.
		if strings.TrimSpace(narration) == "" {
			c.host.ClearOutcome(req.RunID, req.NodeID)
			events = c.snapshotEvents(ctx, sess.sb, events)
			return withHandoffs(ReactTurn{Msg: narration, Done: false, Events: events, Usage: usage, UsageByModel: usageByModel})
		}
		if gq, msg, ge, gu, gum, ok := c.enforceOpenQuestionsGate(ctx, req, sess); ok {
			events = append(events, ge...)
			usage = models.AddTokenUsage(usage, gu)
			usageByModel = models.AddTokenUsageByModel(usageByModel, gum)
			if strings.TrimSpace(msg) == "" {
				msg = narration
			}
			return withHandoffs(ReactTurn{Msg: msg, Questions: gq, Events: events, Usage: usage, UsageByModel: usageByModel})
		} else if gu != nil || gum != nil {
			events = append(events, ge...)
			usage = models.AddTokenUsage(usage, gu)
			usageByModel = models.AddTokenUsageByModel(usageByModel, gum)
		}
		if req.Caps.WritesSchema(models.SchemaPreflight) {
			if gp, msg, ge, gu, gum, ok := c.enforcePreflightGate(ctx, req, sess); ok {
				events = append(events, ge...)
				usage = models.AddTokenUsage(usage, gu)
				usageByModel = models.AddTokenUsageByModel(usageByModel, gum)
				if strings.TrimSpace(msg) == "" {
					msg = narration
				}
				return withHandoffs(ReactTurn{Msg: msg, Questions: gp.Questions, Forms: gp.Forms, Events: events, Usage: usage, UsageByModel: usageByModel})
			} else if gu != nil || gum != nil {
				events = append(events, ge...)
				usage = models.AddTokenUsage(usage, gu)
				usageByModel = models.AddTokenUsageByModel(usageByModel, gum)
			}
		}
		// The dialogue only ends when the human confirms.
		c.host.ClearOutcome(req.RunID, req.NodeID)
		events = c.snapshotEvents(ctx, sess.sb, events)
		return withHandoffs(ReactTurn{Msg: narration, Done: false, Events: events, Usage: usage, UsageByModel: usageByModel})
	}
	// The reconcile turn above closed the human dialogue, so the wrap-up may end
	// with the hidden turn that induces the transcript into the ledger summary.
	done := c.finishReact(ctx, req, key, sess, narration, history, events, usage, usageByModel, true)
	return withHandoffs(done)
}

// ReviseInPlace sends one review turn to the parked session and keeps it alive.
// Unlike ReactReply it never finishes/closes the node: it streams the human's
// annotated instruction, then re-prompts the agent to persist its structured
// product (best-effort, same as the finish path) so the store reflects the
// edit, and returns a non-Done turn. Used by both the node-inline review "send
// (keep editing)" and the approval-gate ReAct reject (against the upstream
// producer's parked session). A dead/lost session is rebuilt from the
// transcript, mirroring ReactReply.
func (c *acpProvider) ReviseInPlace(ctx context.Context, req NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) (out ReactTurn) {
	var chatOp string
	defer func() {
		if out.OpID == "" && chatOp != "" {
			out.OpID = chatOp
		}
		c.drainCarriedUsage(reactKey(req), &out)
	}()
	key := reactKey(req)
	c.mu.Lock()
	sess := c.sessions[key]
	c.mu.Unlock()
	rehydrated := false
	if sess == nil || sess.acp == nil || !sess.acp.IsConnected() {
		if sess != nil {
			c.mu.Lock()
			delete(c.sessions, key)
			c.mu.Unlock()
			c.discardSandbox(ctx, req, sess.sb, sess.acp, sess.home, nil)
		}
		prior := history
		if len(prior) > 0 && prior[len(prior)-1].Role == "human" {
			prior = prior[:len(prior)-1]
		}
		sess = c.rehydrateReact(ctx, req, prior)
		if sess == nil {
			err := errors.New("复审会话已失效,自动重建沙箱失败,请稍后重试")
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).Msg("review revise rehydrate failed")
			return ReactTurn{Msg: "(" + err.Error() + ")", Done: false, Err: err}
		}
		rehydrated = true
	}
	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	res, err := c.streamChat(chatCtx, sess.acp, req, reviewTurnPrompt(req, history, human, rehydrated), images)
	cancel()
	if res != nil && res.OpID != "" {
		chatOp = res.OpID
	}
	if err != nil {
		log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).Msg("review revise chat failed")
		return ReactTurn{Msg: "(复审修改失败:" + err.Error() + ")", Err: err,
			Events: []models.AcpEvent{{Kind: "message", Text: "review revise chat failed: " + err.Error()}}}
	}
	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel
	var events []models.AcpEvent
	absorbChat(&usage, &usageByModel, &events, res)
	handoffs := handoffNarrations(res)

	_ = takeClarifyPending(c.host, req.RunID, req.NodeID)

	if fail := chatFailure(res); fail != "" {
		msg := withFailureBanner(res.Narration, "复审修改失败", fail)
		events = append(events, models.AcpEvent{Kind: "message", Text: "review revise chat failed: " + fail})
		return ReactTurn{Msg: msg, Done: false, Err: errors.New(fail), Events: events, Usage: usage, UsageByModel: usageByModel, Handoffs: handoffs}
	}

	if _, serr := c.ensureRequiredProducts(ctx, req, sess.acp, &events, &usage, &usageByModel); serr != nil {
		log.Warn().Err(serr).Str("node", req.NodeID).Msg("review revise ensure product failed")
	}
	events = c.snapshotEvents(ctx, sess.sb, events)
	return ReactTurn{Msg: res.Narration, Done: false, Events: events, Usage: usage, UsageByModel: usageByModel, Handoffs: handoffs}
}

// reviewTurnPrompt prefixes a review turn with its Live contract (Live turns
// only) and, on the first review turn or after a rehydrate lost the agent
// context, the review-phase capability note.
func reviewTurnPrompt(req NodeReq, history []models.ReactMessage, human string, rehydrated bool) string {
	if !req.Caps.ReviewEnabled() {
		return human
	}
	prompt := human
	if extra := liveVariantPromptExtras(req, human); extra != "" {
		prompt = extra + "\n" + prompt
	}
	if rehydrated || humanTurnCount(history) <= 1 {
		prompt = strings.TrimSpace(reviewCapabilityExtras(req)) + "\n\n## 用户消息\n" + prompt
	}
	return prompt
}

func humanTurnCount(history []models.ReactMessage) int {
	n := 0
	for _, m := range history {
		if m.Role == "human" {
			n++
		}
	}
	return n
}

// HasLiveSession reports whether a parked review session is held for the node.
func (c *acpProvider) HasLiveSession(runID, nodeID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	sess := c.sessions[runID+"|"+nodeID]
	return sess != nil && sess.acp != nil && sess.acp.IsConnected()
}

// RetireSession closes and retires the parked session for the node, if any.
func (c *acpProvider) RetireSession(runID, nodeID string) {
	c.closeSession(runID + "|" + nodeID)
}

// CancelSessionTurn aborts the in-flight ACP turn on a parked review session
// without retiring it. Bridge {op:cancel} also clears the sandbox PromptQueue
// (dual-layer sync with the platform review FIFO).
func (c *acpProvider) CancelSessionTurn(runID, nodeID string) {
	c.mu.Lock()
	sess := c.sessions[runID+"|"+nodeID]
	c.mu.Unlock()
	if sess != nil && sess.acp != nil {
		_ = sess.acp.Cancel()
	}
}

// abortTurnWait bounds how long AbortSessionTurn waits for the bridge.
var abortTurnWait = 20 * time.Second

func (c *acpProvider) liveACP(runID, nodeID string) *sandbox.ACPClient {
	c.mu.Lock()
	sess := c.sessions[runID+"|"+nodeID]
	c.mu.Unlock()
	if sess == nil || sess.acp == nil || !sess.acp.IsConnected() {
		return nil
	}
	return sess.acp
}

// SessionBridgeState reports the bridge's mirrored queue_state for a parked
// session. A chat in flight on the client counts as busy, and its op is
// written back when the bridge is still echoing the previous turn.
func (c *acpProvider) SessionBridgeState(runID, nodeID string) (BridgeStatus, bool) {
	a := c.liveACP(runID, nodeID)
	if a == nil {
		return BridgeStatus{}, false
	}
	return ClientBridgeStatus(a)
}

// ClientBridgeStatus is the bridge view for one live ACP client. BridgeState
// clears a lagging echo of lastDone before the caller sees it; a turn this
// client has already entered (turnOpID set) must stay busy, and the current
// op is written back when the bridge op is empty or still that lastDone.
func ClientBridgeStatus(a *sandbox.ACPClient) (BridgeStatus, bool) {
	if a == nil || !a.IsConnected() {
		return BridgeStatus{}, false
	}
	st := a.BridgeState()
	op := strings.TrimSpace(a.InFlightOpID())
	if op == "" {
		return st, true
	}
	st.Busy = true
	st.InFlightOpID = op
	running := strings.TrimSpace(st.RunningOpID)
	done := strings.TrimSpace(st.LastDoneOpID)
	if running == "" || running == done {
		st.RunningOpID = op
	}
	return st, true
}

// AbortSessionTurn cancels whatever turn the parked session's bridge runs.
func (c *acpProvider) AbortSessionTurn(runID, nodeID string) bool {
	a := c.liveACP(runID, nodeID)
	if a == nil {
		return true
	}
	ok := a.AbortRunning(abortTurnWait)
	log.Info().Str("run", runID).Str("node", nodeID).Bool("acked", ok).Msg("abort sandbox turn")
	return ok
}

// ReconcileOnConfirm runs the confirm-time pair against a review producer's
// parked session: one visible turn reconciling the structured products with the
// whole transcript, then the hidden summary turn. Clarify Agents get the
// same pair inside ReactReply(force=true) instead, because their reconcile
// prompt must also require node_complete.
//
// Best-effort like OfferCommitOnConfirm: a dead session or a failed chat only
// yields an empty turn, never a failed confirm.
func (c *acpProvider) ReconcileOnConfirm(ctx context.Context, req NodeReq) ReactTurn {
	c.mu.Lock()
	sess := c.sessions[reactKey(req)]
	c.mu.Unlock()
	if sess == nil || sess.acp == nil || !sess.acp.IsConnected() {
		log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
			Msg("confirm reconcile skipped: no live session")
		return ReactTurn{}
	}

	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel
	var events []models.AcpEvent

	prompt := models.ReviewConfirmReconcile
	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	res, err := c.streamChat(chatCtx, sess.acp, req, prompt, nil)
	cancel()
	if err != nil {
		log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
			Msg("confirm reconcile chat failed")
		return ReactTurn{}
	}
	absorbChat(&usage, &usageByModel, &events, res)
	_ = takeClarifyPending(c.host, req.RunID, req.NodeID)

	agentSummary := c.confirmSummaryTurn(ctx, req, sess, &events, &usage, &usageByModel)
	events = c.snapshotEvents(ctx, sess.sb, events)
	return ReactTurn{Msg: res.Narration, AgentSummary: agentSummary, Done: false,
		Events: events, Usage: usage, UsageByModel: usageByModel}
}

// confirmSummaryTurn sends the hidden induction turn and returns the parsed
// agentSummary. Its narration never reaches the transcript — only the ACP
// events land on the timeline — so an unparseable answer yields "" instead of
// promoting prose the agent did not mean as a summary.
//
// Best-effort throughout: this runs after the human already confirmed, so a
// failure must cost the ledger a summary rather than block the transition.
func (c *acpProvider) confirmSummaryTurn(ctx context.Context, req NodeReq, sess *reactSession,
	events *[]models.AcpEvent, usage **models.TokenUsage, usageByModel *models.TokenUsageByModel) string {
	if sess == nil || sess.acp == nil || !sess.acp.IsConnected() {
		return ""
	}
	prompt := models.ConfirmSummaryContract
	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	res, err := c.streamChat(chatCtx, sess.acp, req, prompt, nil)
	cancel()
	if err != nil {
		log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
			Msg("confirm summary chat failed")
		return ""
	}
	absorbChat(usage, usageByModel, events, res)
	// The summary turn must not resurrect the dialogue: a stray ask_question
	// here would otherwise leak into the next node's pending questions.
	_ = takeClarifyPending(c.host, req.RunID, req.NodeID)

	agentSummary := parseAgentSummary(res.Narration)
	if agentSummary == "" {
		// The response can quote user feedback, so only the miss is recorded.
		log.Debug().Str("run", req.RunID).Str("node", req.NodeID).Str("node_type", req.NodeType).
			Msg("confirm summary missing or unparseable")
	}
	return agentSummary
}

// reactConfirmPrefix is the confirm-turn instruction prepended to the human
// message; when the required products are already settled it also appends
// the skip-rewrite note.
func (c *acpProvider) reactConfirmPrefix(req NodeReq) string {
	out := models.ClarifyConfirmSuffix + requiredProductsLine(req.Caps)
	if c.clarifyProductsSettled(req) {
		out += models.ClarifyConfirmProductsReadyNote
	}
	return out
}

// requiredProductsLine names the tools that write the Agent's required
// products, so the confirm turn knows exactly what to reconcile.
func requiredProductsLine(caps *models.AgentCapabilities) string {
	var tools []string
	for _, s := range nodereg.RequiredSchemas(caps, "") {
		tool := s.SetTool
		if tool == "" {
			tool = "write_artifact"
		}
		tools = append(tools, "`"+tool+"`("+s.ArtifactName+")")
	}
	if len(tools) == 0 {
		return ""
	}
	return "\n\n必填产物:" + strings.Join(tools, "、") + "。"
}

// clarifyProductsSettled reports whether the store already holds every
// required product written by this node, with no open questions left and
// work_kind / root_cause consistent. Any doubt returns false so the confirm
// prompt keeps the full "补齐或修正" wording.
func (c *acpProvider) clarifyProductsSettled(req NodeReq) bool {
	if c == nil || c.host == nil || req.Caps == nil {
		return false
	}
	workKind := ""
	if req.Caps.WritesSchema(models.SchemaClarifiedRequirement) {
		cr, err := c.host.ReadArtifact(req.RunID, req.Token, mcp.ClarifiedRequirementArtifactName)
		if err != nil || !json.Valid([]byte(cr)) || len(mcp.ClarifiedOpenQuestions(cr)) > 0 {
			return false
		}
		workKind = mcp.ClarifiedWorkKind(cr)
	}
	for _, s := range nodereg.RequiredSchemas(req.Caps, workKind) {
		if !artifactOwnedByNode(c.host, req.RunID, req.Token, req.NodeID, s.ArtifactName) {
			return false
		}
	}
	if !req.Caps.WritesSchema(models.SchemaRootCause) {
		return true
	}
	switch workKind {
	case "":
		return false
	case "bug":
		raw, err := c.host.ReadArtifact(req.RunID, req.Token, mcp.RootCauseArtifactName)
		return err == nil && parseRootCauseJSON(raw) == nil
	default:
		return !artifactPresent(c.host, req.RunID, req.Token, mcp.RootCauseArtifactName)
	}
}

// enforceOpenQuestionsGate implements the clarification gate: when the agent
// tries to finish (raised no ask_question this turn) but its clarified
// requirement still lists unresolved open_questions, re-prompt it (same session)
// to surface those as ask_question so the user resolves every one. Returns the
// freshly raised questions, the wrap-up narration, the turn's events, and ok=true
// when the gate held (i.e. new questions were raised and the node must keep
// clarifying). ok=false lets the caller finish normally: no artifact yet, no
// open questions, or the agent declined to ask again (avoids an infinite loop).
func (c *acpProvider) enforceOpenQuestionsGate(ctx context.Context, req NodeReq, sess *reactSession) ([]models.ReactQuestion, string, []models.AcpEvent, *models.TokenUsage, models.TokenUsageByModel, bool) {
	content, err := c.host.ReadArtifact(req.RunID, req.Token, mcp.ClarifiedRequirementArtifactName)
	if err != nil {
		return nil, "", nil, nil, nil, false
	}
	if !json.Valid([]byte(content)) {
		log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
			Msg("react open-questions gate: clarified requirement unparseable; skipping")
		return nil, "", nil, nil, nil, false
	}
	open := mcp.ClarifiedOpenQuestions(content)
	if len(open) == 0 {
		return nil, "", nil, nil, nil, false
	}
	prompt := models.ClarifiedOpenQuestionsRetryFor(open)
	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	res, err := c.streamChat(chatCtx, sess.acp, req, prompt, nil)
	cancel()
	if err != nil {
		log.Warn().Err(err).Str("node", req.NodeID).Msg("react open-questions gate re-prompt failed")
		return nil, "", nil, nil, nil, false
	}
	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel
	var events []models.AcpEvent
	absorbChat(&usage, &usageByModel, &events, res)
	pending := takeClarifyPending(c.host, req.RunID, req.NodeID)
	if !pending.any() {

		log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
			Int("open_questions", len(open)).
			Msg("react open-questions gate: agent declined to ask; finishing with unresolved notes")
		return nil, "", events, usage, usageByModel, false
	}
	return pending.Questions, res.Narration, events, usage, usageByModel, true
}

// enforcePreflightGate keeps a preflight node open until preflight.json exists
// and is confirmed with empty unresolved. Missing / incomplete product →
// re-prompt (agent may ask_form / ask_question / set_preflight). ok=true when
// new human interaction was raised or the gate still blocks finish.
func (c *acpProvider) enforcePreflightGate(ctx context.Context, req NodeReq, sess *reactSession) (clarifyPending, string, []models.AcpEvent, *models.TokenUsage, models.TokenUsageByModel, bool) {
	content, err := c.host.ReadArtifact(req.RunID, req.Token, mcp.PreflightArtifactName)
	reason := ""
	if err != nil {
		reason = "尚无 preflight.json"
	} else if inc := mcp.PreflightIncomplete(content); inc != "" {
		reason = inc
	} else {
		return clarifyPending{}, "", nil, nil, nil, false
	}
	prompt := models.PreflightRetryFor(reason)
	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	res, err := c.streamChat(chatCtx, sess.acp, req, prompt, nil)
	cancel()
	if err != nil {
		log.Warn().Err(err).Str("node", req.NodeID).Msg("preflight gate re-prompt failed")
		return clarifyPending{}, "", nil, nil, nil, false
	}
	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel
	var events []models.AcpEvent
	absorbChat(&usage, &usageByModel, &events, res)
	pending := takeClarifyPending(c.host, req.RunID, req.NodeID)
	if pending.any() {
		return pending, res.Narration, events, usage, usageByModel, true
	}
	// Re-check: agent may have set_preflight without asking again.
	content, err = c.host.ReadArtifact(req.RunID, req.Token, mcp.PreflightArtifactName)
	if err == nil && mcp.PreflightIncomplete(content) == "" {
		return clarifyPending{}, "", events, usage, usageByModel, false
	}
	log.Warn().Str("run", req.RunID).Str("node", req.NodeID).Str("reason", reason).
		Msg("preflight gate: still incomplete after re-prompt; keeping dialogue open")
	// Keep open with empty pending — engine sees Done=false via force path only;
	// return ok=true with narration so ReactReply does not finish.
	return clarifyPending{}, res.Narration, events, usage, usageByModel, true
}

// finishReact runs the shared completion path for a react node: ensure the
// declared produces artifact exists (re-prompting the agent to write it),
// snapshot the event log, capture any code changes, tear the session down and
// return a Done ReactTurn. If the agent raises ask_question during ensure*,
// the session stays open and Questions are returned (Done=false) so the
// engine can auto_clarify or wait for a human — never OutcomeRetry-mis-fail.
// wantSummary asks for the hidden confirm-time induction turn. It deliberately
// runs at the very end — after products and node_complete are confirmed present
// — so it can neither delay nor derail a confirm that is about to be rejected,
// and cannot come between the reconcile turn and the outcome gate.
func (c *acpProvider) finishReact(ctx context.Context, req NodeReq, key string, sess *reactSession, narration string, history []models.ReactMessage, events []models.AcpEvent, usage *models.TokenUsage, usageByModel models.TokenUsageByModel, wantSummary bool) ReactTurn {

	if pending, err := c.ensureRequiredProducts(ctx, req, sess.acp, &events, &usage, &usageByModel); pending.any() {
		msg := narration
		return ReactTurn{Msg: msg, Questions: pending.Questions, Forms: pending.Forms, Done: false, Events: events, Usage: usage, UsageByModel: usageByModel}
	} else if err != nil {
		events = c.snapshotEvents(ctx, sess.sb, events)
		c.closeSession(key)
		return ReactTurn{Done: true, Err: err, Msg: err.Error(), Events: events, Usage: usage,
			Result: NodeResult{Events: events, Usage: usage, UsageByModel: usageByModel}}
	}
	if req.Caps.WritesSchema(models.SchemaPreflight) {
		if gp, msg, ge, gu, gum, ok := c.enforcePreflightGate(ctx, req, sess); ok {
			events = append(events, ge...)
			usage = models.AddTokenUsage(usage, gu)
			usageByModel = models.AddTokenUsageByModel(usageByModel, gum)
			if strings.TrimSpace(msg) == "" {
				msg = narration
			}
			return ReactTurn{Msg: msg, Questions: gp.Questions, Forms: gp.Forms, Done: false, Events: events, Usage: usage, UsageByModel: usageByModel}
		} else if gu != nil || gum != nil {
			events = append(events, ge...)
			usage = models.AddTokenUsage(usage, gu)
			usageByModel = models.AddTokenUsageByModel(usageByModel, gum)
		}
	}
	pending, err := c.ensureOutcome(ctx, req, sess.acp, &events, &usage, &usageByModel)
	if pending.any() {
		return ReactTurn{Msg: narration, Questions: pending.Questions, Forms: pending.Forms, Done: false, Events: events, Usage: usage, UsageByModel: usageByModel}
	}
	if err != nil {
		events = c.snapshotEvents(ctx, sess.sb, events)
		c.closeSession(key)
		return ReactTurn{Done: true, Err: err, Msg: err.Error(), Events: events, Usage: usage,
			Result: NodeResult{Events: events, Usage: usage, UsageByModel: usageByModel}}
	}
	// The node is definitely finishing from here on, so the induction turn can
	// no longer influence the outcome — only the ledger summary.
	var agentSummary string
	if wantSummary {
		agentSummary = c.confirmSummaryTurn(ctx, req, sess, &events, &usage, &usageByModel)
	}
	events = c.snapshotEvents(ctx, sess.sb, events)
	out := map[string]any{"clarified_requirement": narration, "content": narration, "transcript": renderTranscript(history)}
	git := c.captureChanges(ctx, sess.sb, req, out)
	c.closeSession(key)
	return ReactTurn{Msg: narration, Done: true, AgentSummary: agentSummary, Events: events, Usage: usage,
		Result: NodeResult{OutputMd: narration, Outputs: out, Events: events, Git: git, Usage: usage, UsageByModel: usageByModel}}
}

func (c *acpProvider) buildReactOpenPrompt(req NodeReq, seeded []string) string {
	return c.buildAgentPrompt(req, seeded) + models.ClarifyOpenSuffix
}

// buildReactRehydratePrompt re-opens a dialogue after a crash/restart: the base
// open prompt plus the persisted Q&A transcript as recovery context, instructing
// the agent to resume without re-asking and to await the next human reply.
func (c *acpProvider) buildReactRehydratePrompt(req NodeReq, seeded []string, history []models.ReactMessage) string {
	var b strings.Builder
	b.WriteString(c.buildReactOpenPrompt(req, seeded))
	b.WriteString("\n\n## 会话恢复(重要)\n之前的澄清对话因故中断,现在在新会话中恢复。以下是此前的完整问答记录,请据此恢复上下文并继续:不要重复已经问过的问题,先不要提出新问题,等待我接下来的回复。\n\n")
	b.WriteString(renderTranscript(history))
	return b.String()
}

func reactKey(req NodeReq) string { return req.RunID + "|" + req.NodeID }

// parkReactSession stores a live sandbox+ACP session for later ReactReply.
func (c *acpProvider) parkReactSession(req NodeReq, sb *sandbox.Sandbox, acp *sandbox.ACPClient, home string) *reactSession {
	key := reactKey(req)
	sess := &reactSession{sb: sb, acp: acp, home: home}
	c.mu.Lock()
	c.sessions[key] = sess
	c.live[key] = sb
	host, port := "", 0
	if sb != nil {
		host, port = sb.Host, sb.Port
	}
	c.mu.Unlock()
	if c.timeline != nil && sb != nil {
		c.timeline.startIngest(req.RunID, req.NodeID, host, port, sb.Password, acpTurnBusy(acp))
	}
	return sess
}

// reactHistoryHasDialogue reports whether history contains a real turn
// (non-empty text, questions, or images). A clarify session parked for the
// user's first message has none.
func reactHistoryHasDialogue(history []models.ReactMessage) bool {
	for _, m := range history {
		if strings.TrimSpace(m.Text) != "" || len(m.Questions) > 0 || len(m.Forms) > 0 || len(m.Images) > 0 {
			return true
		}
	}
	return false
}

// clarifyInjectOpenPrompt is true until a clarify dialogue has completed a
// real opening LLM turn. Failed / interrupted agent bubbles do not count —
// retry must still receive the open contract (ReactOpen never chatted).
func clarifyInjectOpenPrompt(req NodeReq, history []models.ReactMessage) bool {
	if !req.Caps.Clarify() {
		return false
	}
	prior := history
	if len(prior) > 0 && prior[len(prior)-1].Role == "human" {
		prior = prior[:len(prior)-1]
	}
	return !clarifyHasOpenedTurn(prior)
}

// clarifyHasOpenedTurn reports a successful clarify agent turn (not a
// platform failure / interrupt placeholder). Used to decide open-prompt
// injection and whether rehydrate may skip priming.
func clarifyHasOpenedTurn(history []models.ReactMessage) bool {
	for _, m := range history {
		if m.Role != "agent" {
			continue
		}
		if m.Interrupted || isClarifyFailedOpenText(m.Text) {
			continue
		}
		if strings.TrimSpace(m.Text) != "" || len(m.Questions) > 0 || len(m.Images) > 0 {
			return true
		}
	}
	return false
}

// chatFailureMaxLen caps provider error bodies so a long stderr dump cannot
// blow up the persisted React conversation JSON.
const chatFailureMaxLen = 2000

// chatFailure returns the provider-side failure text when the bridge reported
// prompt_done but the turn actually failed (stopReason=failed and/or an
// error_text body). Empty means the turn completed normally.
func chatFailure(res *sandbox.ChatResult) string {
	if res == nil {
		return ""
	}
	text := strings.TrimSpace(res.ErrorText)
	if !res.Failed && text == "" {
		return ""
	}
	if text == "" {
		text = "stopReason=failed"
	}
	return truncateChatFailure(text)
}

func truncateChatFailure(s string) string {
	if len(s) <= chatFailureMaxLen {
		return s
	}
	return s[:chatFailureMaxLen] + "…"
}

// withFailureBanner builds "(<prefix>:<fail>)", appending after any narration
// so a partial reply is preserved rather than replaced.
func withFailureBanner(narration, prefix, fail string) string {
	banner := "(" + prefix + ":" + fail + ")"
	n := strings.TrimSpace(narration)
	if n == "" {
		return banner
	}
	return n + "\n" + banner
}

func isClarifyFailedOpenText(text string) bool {
	s := strings.TrimSpace(text)
	switch {
	case s == "(已中断)":
		return true
	case strings.HasPrefix(s, "(澄清回复失败:"):
		return true
	case strings.HasPrefix(s, "(澄清会话已失效"):
		return true
	default:
		return false
	}
}

func mergePromptImages(a, b []models.PromptImage) []models.PromptImage {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	// Preallocate from one side only — avoid len(a)+len(b) (CodeQL go/allocation-size-overflow).
	out := make([]models.PromptImage, 0, len(a))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// chatResultToEvents flattens a ChatResult into ordered AcpEvents for the run
// timeline. Thin wrapper over the shared sandbox converter (single source of
// truth for the event shape). Do not add a second truncate path here — thought
// fullness is owned by ChatResult.AcpEvents (plan g1.2).
func chatResultToEvents(res *sandbox.ChatResult) []models.AcpEvent {
	return res.AcpEvents()
}

// handoffNarrations is the text already sealed by a same-turn continuation.
// It comes from this turn's ChatResult, not the session event log, so an older
// turn cannot be copied into the new row.
func handoffNarrations(res *sandbox.ChatResult) []string {
	if res == nil {
		return nil
	}
	var out []string
	for _, seg := range res.Segments {
		text := strings.TrimSpace(seg.Narration)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}
