package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	gatenode "github.com/cocofhu/grasp/internal/models/nodereg"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"

	"github.com/rs/zerolog/log"
)

// reviewVarName resolves the control variable that decides whether this node
// enters the post-run ReAct review phase: a per-node config["review_var"]
// override, else the node type's default (nodereg.Spec.ReviewVar). Empty when
// the node type is not review-capable.
func reviewVarName(node *models.Node) string {
	if v := strings.TrimSpace(str(node.Config["review_var"])); v != "" {
		return v
	}
	return nodereg.DefaultReviewVar(node.Type)
}

// reviewEnabled reports whether this node should enter the interactive review
// phase. Natural-language semantics aligned with "是否要 review":
//
//	undefined ⇒ skip (legacy pipelines without the var never stop for review)
//	DEFINED and FALSY  ⇒ skip
//	DEFINED and TRUTHY ⇒ enter interactive review
//
// app_preview always enters review after a healthy set_preview (pure ReAct
// waiting_human; no Gate shell — main interaction is parked ReAct + VNC).
// A node type with no review variable is never review-capable.
func (e *Engine) reviewEnabled(c *execCtx, node *models.Node) bool {
	if node != nil && node.Type == "app_preview" {
		return true
	}
	name := reviewVarName(node)
	if name == "" {
		return false
	}
	raw, ok := c.vars[name]
	if !ok {
		return false // undefined ⇒ skip (zero behavior change for old graphs)
	}
	return truthy(raw) // truthy ⇒ review, falsy ⇒ skip
}

// hasDownstreamReactGate reports whether some approval gate reachable forward of
// this producer node binds its product as the primary upstream — in which case
// the producer's sandbox session must be kept alive so the gate can issue a
// ReAct reject against it. app_preview is its own producer and handled inline.
func (e *Engine) hasDownstreamReactGate(c *execCtx, node *models.Node) bool {
	if node == nil {
		return false
	}
	visited := map[string]bool{node.ID: true}
	queue := []string{node.ID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, ed := range c.graph.OutEdges(cur) {
			if visited[ed.Target] {
				continue
			}
			visited[ed.Target] = true
			gn := c.graph.FindNode(ed.Target)
			if gn == nil {
				continue
			}
			switch gn.Type {
			case "human_gate":
				if e.gateProducerNodeID(c, gn) == node.ID {
					return true
				}
			case "proposal_select":
				// Prefer the artifact's recorded producer when it already exists;
				// while the producer is still running the artifact is missing, so
				// fall back to "does this node write the select's source artifact?".
				if e.gateProducerNodeID(c, gn) == node.ID {
					return true
				}
				from := firstNonEmptyStr(str(gn.Config["from"]), mcp.ProposalsArtifactName)
				if e.nodeProducesArtifact(node, from) {
					return true
				}
			}
			queue = append(queue, ed.Target)
		}
	}
	return false
}

// nodeProducesArtifact reports whether node writes the given reserved/produces
// artifact name (used to bind a still-running producer to a downstream
// proposal_select before the artifact row exists).
func (e *Engine) nodeProducesArtifact(node *models.Node, name string) bool {
	name = strings.TrimSpace(name)
	if node == nil || name == "" {
		return false
	}
	if spec, ok := nodereg.Get(node.Type); ok && strings.TrimSpace(spec.ArtifactName) == name {
		return true
	}
	if produces := strings.TrimSpace(str(node.Config["produces"])); produces == name {
		return true
	}
	return false
}

// gateProducerNodeID resolves the upstream producer node whose product an
// approval gate reviews (and whose parked session a ReAct reject edits):
//   - human_gate: the primary upstream node bound by its body template.
//   - proposal_select: the node that wrote the upstream proposals.json.
//
// app_preview no longer creates a Gate; its parked session is finished via
// reviewReply (force) rather than GateReactRevise / GateReactInfo.
//
// Returns "" when it cannot be resolved.
func (e *Engine) gateProducerNodeID(c *execCtx, gate *models.Node) string {
	switch gate.Type {
	case "human_gate":
		return gatenode.GatePrimaryUpstreamNodeID(gate)
	case "proposal_select":
		from := firstNonEmptyStr(str(gate.Config["from"]), mcp.ProposalsArtifactName)
		var art models.Artifact
		if err := e.db.Where("run_id = ? AND name = ?", c.run.ID, from).
			Order("updated_at desc, created_at desc").First(&art).Error; err == nil && art.NodeID != "" {
			return art.NodeID
		}
		return ""
	default:
		return ""
	}
}

// maybeEnterReview turns a freshly-completed producer outcome into a paused
// review pause when review is requested; otherwise returns it unchanged. The
// live session was already parked by the provider (KeepAliveForReview).
func (e *Engine) maybeEnterReview(c *execCtx, node *models.Node, completed nodeOutcome) nodeOutcome {
	if !e.reviewEnabled(c, node) {
		return completed
	}
	return e.enterReview(c, node, completed)
}

// enterReview seeds a review ReactConversation (agent turn = product summary +
// review instruction) for this visit and returns a paused outcome so the run
// waits for human input. Idempotent per iteration: a conversation already open
// for this visit is reused. Marks the run's review phase so MCP ask_question is
// permitted and set_*/get_* stay authorized across turns.
func (e *Engine) enterReview(c *execCtx, node *models.Node, completed nodeOutcome) nodeOutcome {
	iter := c.iter[node.ID]
	var conv models.ReactConversation
	err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).First(&conv).Error
	if err == nil {
		// Already seeded (defensive): keep paused unless already concluded.
		if conv.Done {
			return completed
		}
		e.host.SetActiveReview(c.run.ID, true)
		// Carry completed.usage so saveState still merges production-phase tokens
		// onto this StateRun (nil usage would leave the timeline as "—").
		return nodeOutcome{status: "paused", outputMd: "等待人工复审(ReAct)…",
			outputs: completed.outputs, events: completed.events, usage: completed.usage}
	}
	summary := e.reviewSummaryMarkdown(c, node)
	conv = models.ReactConversation{RunID: c.run.ID, NodeID: node.ID, Iteration: iter, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: summary, At: time.Now().Format(time.RFC3339)}}}
	logDB(e.db.Create(&conv), c.run.ID, "seed review conversation")
	e.host.SetActiveReview(c.run.ID, true)
	log.Info().Str("run_id", c.run.ID).Str("node_id", node.ID).Msg("entered post-run ReAct review phase")
	return nodeOutcome{status: "paused", outputMd: "等待人工复审(ReAct)…",
		outputs: completed.outputs, events: completed.events, usage: completed.usage}
}

// reviewSummaryMarkdown renders the node's product as the opening review turn.
func (e *Engine) reviewSummaryMarkdown(c *execCtx, node *models.Node) string {
	var body string
	if spec, ok := nodereg.Get(node.Type); ok {
		switch {
		case node.Type == "plan":
			if s, ok := e.store.Get(c.run.ID, mcp.PlanArtifactName); ok {
				body = mcp.RenderPlanMarkdown(s)
			}
		case node.Type == "visual":
			body = "已生成可视化网页 " + visualPageName + "。请在预览中取点标注要调整的元素,或直接描述修改点。"
		case node.Type == "app_preview":
			body = "应用预览已就绪(set_preview 可达)。请先开启「取点标注」再点选问题元素,或直接对话说明要改哪里;确认无误后点「确认并流转」。结束/Cancel 会话不会拆掉预览服务。"
		case spec.Render != nodereg.RenderNone:
			if render := nodereg.Renderer(spec.Render); render != nil {
				if s, ok := e.store.Get(c.run.ID, spec.ArtifactName); ok {
					body = render(s)
				}
			}
		}
	}
	if strings.TrimSpace(body) == "" {
		body = "本节点产物已生成。"
	}
	return body + "\n\n---\n请审阅以上产物:可逐字段/元素标注并说明要改哪里,我会在同一沙箱里就地修改;确认无误后点「确认并流转」结束复审。"
}

// finalizeProduct re-derives a node's outcome from its (possibly just-edited)
// product, dispatching to the type-specific finalizer so the review-finish path
// behaves exactly like the initial run's contract finalization.
func (e *Engine) finalizeProduct(c *execCtx, node *models.Node, res runtime.NodeResult) nodeOutcome {
	switch node.Type {
	case "visual":
		return e.finalizeVisual(c, node, res)
	case "plan":
		return e.finalizePlan(c, node, res)
	case "app_preview":
		return e.finalizeAppPreview(c, node, res)
	default:
		return e.finalizeAgentProducts(c, node, res)
	}
}

// finalizeAppPreview accepts a healthy set_preview registration as the product
// contract (no structured artifact / node_complete required).
func (e *Engine) finalizeAppPreview(c *execCtx, node *models.Node, res runtime.NodeResult) nodeOutcome {
	if !e.host.HasHealthyPreviewPorts(c.run.ID, node.ID) {
		return nodeOutcome{status: "failed", err: "预览契约未满足:无可达 set_preview",
			outputMd: "应用预览失败:无可达预览端口", events: res.Events, usage: res.Usage, usageByModel: res.UsageByModel}
	}
	out := res.Outputs
	if out == nil {
		out = map[string]any{}
	}
	out["preview_ready"] = true
	return nodeOutcome{status: "completed", outputMd: "应用预览已就绪", outputs: out, events: res.Events, usage: res.Usage, usageByModel: res.UsageByModel}
}

// isReviewNode reports whether a node type uses the post-run ReAct review path
// (a review-capable producer, not the classic react clarify node).
func isReviewNode(nodeType string) bool {
	return nodeType != "react" && !nodereg.IsGrasp(nodeType) && nodereg.ReviewCapable(nodeType)
}

// reviewReply finalizes a post-run review dialogue when force=true: if the
// parked sandbox still has uncommitted code, the agent decides what to commit
// (skipping temp files); then the store snapshot is re-validated and the FSM
// advances (Done+RetireSession+routeSuccess, or keep paused on failure).
// Non-force revise turns are handled by EnqueueReviewTurn / pumpReviewSession
// (platform FIFO + turn_begin materialization). The caller (ReactReply) already
// appended the human turn to conv and holds the per-conversation lock.
func (e *Engine) reviewReply(c *execCtx, node *models.Node, conv *models.ReactConversation, human string, images []models.PromptImage, force bool) error {
	runID, nodeID := c.run.ID, node.ID
	_ = human
	_ = images
	req := e.nodeReq(c, node) // SetActiveNode + KeepAliveForReview (no ClearOutcome)
	e.host.SetActiveReview(runID, true)

	if !force {
		return errors.New("internal: review non-force must use EnqueueReviewTurn")
	}

	// Force finish: leave the pending inbox immediately (same as clarify force /
	// ResumeGate), then optional git wrap-up, retire session, finalize, and
	// roll back Done on validation failure so the item re-enters the inbox.
	// Must not call ReactReply / finishAgentOutcome / TakeOutcome / routeFailure.
	// Human turn is already persisted by ReactReply.
	// Ready-gate is enforced by ReactReply before taking the lock.
	conv.Done = true
	logDB(e.db.Save(conv), runID, "confirm leave pending (review force)")

	humanMsg := lastHumanMessage(conv.Messages)

	if rp, ok := e.provider.(runtime.ReviewProvider); ok {
		// Reconcile the products against the whole transcript (plus the hidden
		// summary turn) before the git wrap-up retires the session.
		rec := rp.ReconcileOnConfirm(context.Background(), req)
		agentMsg := models.ReactMessage{Role: "agent", Text: rec.Msg, At: time.Now().Format(time.RFC3339)}
		if strings.TrimSpace(rec.Msg) != "" {
			conv.Messages = append(conv.Messages, agentMsg)
			logDB(e.db.Save(conv), runID, "save review confirm reconcile")
		}
		e.flushMcpCalls(runID, nodeID)
		e.flushTokenUsage(runID, nodeID, rec.Usage, rec.UsageByModel)
		e.recordFeedback(e.confirmRoundFeedbackEvent(runID, nodeID, models.FeedbackKindReview,
			conv.Iteration, humanMsg, agentMsg, rec.AgentSummary))

		t := rp.OfferCommitOnConfirm(context.Background(), req)
		if strings.TrimSpace(t.Msg) != "" {
			conv.Messages = append(conv.Messages, models.ReactMessage{
				Role: "agent", Text: t.Msg, At: time.Now().Format(time.RFC3339),
			})
			logDB(e.db.Save(conv), runID, "save review git wrap-up")
		}
		e.flushMcpCalls(runID, nodeID)
		e.flushTokenUsage(runID, nodeID, t.Usage, t.UsageByModel)
		rp.RetireSession(runID, nodeID)
	}

	outcome := e.finalizeProduct(c, node, runtime.NodeResult{})
	outcome = e.afterDefaultChecks(c, node, outcome)
	if outcome.status == "failed" {
		// Keep paused/waiting_human so the reviewer can fix and retry.
		conv.Done = false
		logDB(e.db.Save(conv), runID, "reopen review after force validation failure")
		e.host.SetActiveReview(runID, true)
		e.broker.Publish(runID, jsonMsg("react", runID, nodeID))
		errMsg := strings.TrimSpace(outcome.err)
		if errMsg == "" {
			errMsg = "复审确认校验失败"
		}
		log.Warn().Str("run_id", runID).Str("node_id", nodeID).Str("err", errMsg).
			Msg("review force confirm failed validation; staying in review")
		return errors.New(errMsg)
	}

	// app_preview: inject action=pass so legacy when/action edges still match;
	// fail edges see no fail action and do not route. Always clear preview_issues.
	if node.Type == "app_preview" {
		if outcome.outputs == nil {
			outcome.outputs = map[string]any{}
		}
		outcome.outputs["action"] = "pass"
		outVar := firstNonEmptyStr(str(node.Config["output_var"]), "action")
		outcome.outputs[outVar] = "pass"
		c.setVar(outVar, "pass")
		e.persistVar(runID, outVar, "pass")
		e.forceClearPreviewIssueVars(c, runID)
		if lifeErr := e.markPreviewIssuesResolvedByNode(runID, nodeID); lifeErr != nil {
			conv.Done = false
			logDB(e.db.Save(conv), runID, "reopen review after preview-issue lifecycle failure")
			e.host.SetActiveReview(runID, true)
			e.broker.Publish(runID, jsonMsg("react", runID, nodeID))
			return lifeErr
		}
	}

	logDB(e.db.Save(conv), runID, "finish review conversation")
	if e.shareRevoker != nil {
		e.shareRevoker.RevokeUnusedForGate(runID, nodeID, conv.Iteration)
	}
	e.host.SetActiveReview(runID, false)
	if rp, ok := e.provider.(runtime.ReviewProvider); ok {
		rp.RetireSession(runID, nodeID) // idempotent
	}

	e.saveState(c, node, outcome)
	detail := "复审完成"
	if node.Type == "app_preview" {
		detail = "复审完成 action=pass"
	}
	e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "resume", Detail: detail})
	c.nodeOutputs[nodeID] = outcome.outputs
	e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "exit"})
	next := e.routeSuccess(c, node, outcome)
	if next == "" {
		e.finish(runID, "completed")
		return nil
	}
	go e.resumeAdmitted(runID, next)
	return nil
}

// GateReactRevise enqueues a ReAct reject-and-annotate from a pending approval
// gate onto the upstream producer's review session controller (same FIFO /
// Cancel surface as node-inline ReactReply). Returns immediately after enqueue;
// the pump persists turns, refreshes gate BodyMd, and keeps the gate pending.
// Requires the upstream session to be alive; otherwise the caller should fall
// back to a normal reject.
func (e *Engine) GateReactRevise(runID, gateNodeID, text string, images []models.PromptImage, annotations []models.ReactAnnotation) error {
	if e.IsHalted() {
		return errors.New("server is shutting down")
	}
	c, _, gateNode, err := e.loadPendingGate(runID, gateNodeID)
	if err != nil {
		return err
	}
	producerID := e.gateProducerNodeID(c, gateNode)
	if producerID == "" {
		return errors.New("无法定位上游生产节点,无法就地修改")
	}
	if c.graph.FindNode(producerID) == nil {
		return errors.New("上游生产节点不存在")
	}
	rp, ok := e.provider.(runtime.ReviewProvider)
	if !ok || !rp.HasLiveSession(runID, producerID) {
		return errors.New("上游会话已不存在,请改用普通打回(冷启动)")
	}
	_, err = e.EnqueueReviewTurn(runID, producerID, text, images, annotations, "gate", gateNodeID)
	return err
}

// GateReactInfo resolves a gate's upstream producer node and whether its review
// session is still alive (so the UI can decide to offer the ReAct reject entry).
func (e *Engine) GateReactInfo(runID, gateNodeID string) (producerID string, alive bool) {
	c, err := e.loadCtx(runID)
	if err != nil {
		return "", false
	}
	gn := c.graph.FindNode(gateNodeID)
	if gn == nil {
		return "", false
	}
	producerID = e.gateProducerNodeID(c, gn)
	if producerID == "" {
		return "", false
	}
	if rp, ok := e.provider.(runtime.ReviewProvider); ok {
		alive = rp.HasLiveSession(runID, producerID)
	}
	return producerID, alive
}

// retireGateUpstreamSession releases the parked review session of a gate's
// upstream producer once the gate is resolved (approve/select/reject decided).
// If the producer still has uncommitted code, the agent decides what to commit
// first so gate-ReAct edits are not dropped with the sandbox.
func (e *Engine) retireGateUpstreamSession(c *execCtx, gateNode *models.Node) {
	rp, ok := e.provider.(runtime.ReviewProvider)
	if !ok {
		return
	}
	producerID := e.gateProducerNodeID(c, gateNode)
	if producerID == "" {
		return
	}
	if producer := c.graph.FindNode(producerID); producer != nil {
		req := e.nodeReq(c, producer)
		t := rp.OfferCommitOnConfirm(context.Background(), req)
		e.flushMcpCalls(c.run.ID, producerID)
		e.flushTokenUsage(c.run.ID, producerID, t.Usage, t.UsageByModel)
	}
	rp.RetireSession(c.run.ID, producerID)
}

// refreshProducerOutputs re-derives a producer node's outputs from its (edited)
// product and persists them onto the bound iteration's StateRun (completed or
// waiting_human during post-run review), so downstream {{nodes.<id>.outputs.*}}
// interpolation and UI snapshots reflect the live product.
// On finalize failure (e.g. missing page.html) this is a no-op — prior outputs
// are left intact.
func (e *Engine) refreshProducerOutputs(c *execCtx, producer *models.Node) {
	oc := e.finalizeProduct(c, producer, runtime.NodeResult{})
	if oc.status != "completed" || oc.outputs == nil {
		return
	}
	c.nodeOutputs[producer.ID] = oc.outputs
	iter := c.iter[producer.ID]
	var sr models.StateRun
	q := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, producer.ID)
	if iter > 0 {
		q = q.Where("iteration = ?", iter)
	} else {
		q = q.Order("iteration desc, id desc")
	}
	if err := q.First(&sr).Error; err != nil {
		return
	}
	// Never revive or rewrite terminal failure/cancel rows.
	if sr.Status == "failed" || sr.Status == "cancelled" {
		return
	}
	sr.Outputs = oc.outputs
	logDB(e.db.Save(&sr), c.run.ID, "refresh producer outputs after revise")
}

// refreshPendingGatesForProducer re-interpolates BodyMd for unresolved gates
// whose upstream pointer (or primary product) binds to producerID, then notifies
// UI subscribers. Mirrors the gate-body refresh in GateReactRevise / SaveGateArtifact.
func (e *Engine) refreshPendingGatesForProducer(c *execCtx, producerID string) {
	var gates []models.Gate
	if err := e.db.Where("run_id = ? AND resolved = ?", c.run.ID, false).Find(&gates).Error; err != nil {
		return
	}
	c2, err2 := e.loadCtx(c.run.ID)
	if err2 != nil {
		log.Warn().Err(err2).Str("run_id", c.run.ID).Str("producer", producerID).
			Msg("reload ctx after revise failed; pending gate bodies not refreshed")
		return
	}
	for i := range gates {
		gate := &gates[i]
		gateNode := c2.graph.FindNode(gate.NodeID)
		if gateNode == nil {
			continue
		}
		if !gateBindsProducer(gate, gateNode, producerID) {
			continue
		}
		if bt, _ := gateNode.Config["body_template"].(string); strings.TrimSpace(bt) != "" {
			gate.BodyMd = e.interpolate(c2, bt)
			logDB(e.db.Save(gate), c.run.ID, "refresh gate body after producer revise")
		} else if gateNode.Type == "proposal_select" {
			from := firstNonEmptyStr(str(gateNode.Config["from"]), mcp.ProposalsArtifactName)
			if s, ok := e.store.Get(c.run.ID, from); ok {
				gate.BodyMd = mcp.RenderProposalsMarkdown(s)
				logDB(e.db.Save(gate), c.run.ID, "refresh proposal_select body after producer revise")
			}
		}
		e.broker.Publish(c.run.ID, jsonMsg("artifact_edit", c.run.ID, gate.NodeID))
	}
}

// gateBindsProducer reports whether a pending gate's upstream pointer or primary
// product list references producerID.
func gateBindsProducer(gate *models.Gate, gateNode *models.Node, producerID string) bool {
	if gate.UpstreamNodeID == producerID {
		return true
	}
	for _, p := range gatenode.GatePrimaryProducts(gateNode, nil) {
		if p.NodeID == producerID {
			return true
		}
	}
	return false
}

// renderReviewHuman folds review annotations into the human instruction text
// sent to the agent, so the agent edits exactly the cited fields/elements.
func renderReviewHuman(text string, anns []models.ReactAnnotation) string {
	block := models.RenderAnnotations(anns)
	text = strings.TrimSpace(text)
	switch {
	case block != "" && text != "":
		return block + "\n" + text
	case block != "":
		return block + "\n(按上述标注修改)"
	default:
		return text
	}
}
