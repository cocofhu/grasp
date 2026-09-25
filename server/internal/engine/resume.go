package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	gatenode "github.com/cocofhu/grasp/internal/models/nodereg"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// validateGateForm enforces a human_gate's form contract on resume. A field is
// mandatory when it is itself marked required, or when the chosen action sets
// RequireForm (e.g. a reject action that must carry a review comment). Returns
// an error naming the first missing field so the gate stays pending.
func validateGateForm(gate models.Gate, action string, form map[string]any) error {
	requireAll := false
	for _, a := range gate.Actions {
		if a.ID == action && a.RequireForm {
			requireAll = true
			break
		}
	}
	for _, f := range gate.Form {
		if !f.Required && !requireAll {
			continue
		}
		if isBlank(form[f.Key]) {
			label := f.Label
			if label == "" {
				label = f.Key
			}
			return fmt.Errorf("请先填写「%s」再提交", label)
		}
	}
	return nil
}

// ResumeGate resolves a paused human_gate and continues the FSM.
func (e *Engine) ResumeGate(runID, nodeID, action string, form map[string]any) error {
	return e.ResumeGateAs(runID, nodeID, action, form, "")
}

type resumeGateOpts struct {
	skipFormValidate bool
	callerKind       string
	externalName     string
}

// ResumeGateAs is like ResumeGate but records the reviewer username on outputs
// and audit. Empty reviewer → system + unattributable (no fabricated names).
func (e *Engine) ResumeGateAs(runID, nodeID, action string, form map[string]any, reviewer string) error {
	if e.IsHalted() {
		return errors.New("server is shutting down")
	}
	unlock := e.lockResume(runID + ":" + nodeID)
	defer unlock()
	return e.resumeGateLocked(runID, nodeID, action, form, reviewer, resumeGateOpts{})
}

func (e *Engine) resumeGateLocked(runID, nodeID, action string, form map[string]any, reviewer string, opts resumeGateOpts) error {
	c, err := e.loadCtx(runID)
	if err != nil {
		return err
	}
	node := c.graph.FindNode(nodeID)
	if node == nil || (node.Type != "human_gate" && node.Type != "proposal_select") {
		return errors.New("gate node not found")
	}
	// A gate on a terminated run is no longer actionable (the run was cancelled
	// or already finished while paused); reject rather than reviving it.
	switch c.run.Status {
	case "cancelled", "failed", "completed":
		return errors.New("run already ended")
	}
	// Resolve the LATEST gate for this node (highest iteration): a run that
	// looped back opens a new gate per visit, and the pending one is always the
	// most recent — the older resolved gates stay as history.
	var gate models.Gate
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).Order("iteration desc, id desc").First(&gate).Error; err != nil {
		return errors.New("no pending gate")
	}
	if gate.Resolved {
		return errors.New("gate already resolved")
	}
	// Validate the form before locking the gate as resolved: a field marked
	// required, or any field when the chosen action requires the form (e.g. a
	// "reject" action that mandates a comment), must be non-blank.
	if node.Type == "human_gate" && !opts.skipFormValidate {
		if !shouldSnapshotPreviewIssues(node) {
			if err := validateGateForm(gate, action, form); err != nil {
				return err
			}
		}
	}
	gate.Resolved = true
	logDB(e.db.Save(&gate), runID, "resolve gate")

	var outcome nodeOutcome
	if node.Type == "proposal_select" {
		// action is the chosen proposal id; finalize the single proposal.
		from := firstNonEmptyStr(str(node.Config["from"]), mcp.ProposalsArtifactName)
		content, ok := e.store.Get(runID, from)
		if !ok {
			return errors.New("no upstream proposals")
		}
		final, id, ok := mcp.SelectProposal(content, action)
		if !ok {
			return errors.New("proposal selection failed")
		}
		outVar := firstNonEmptyStr(str(node.Config["output_var"]), "selected_proposal")
		outcome = e.finalizeProposal(c, node, final, id, outVar)
	} else if node.Type == "human_gate" {
		// human_gate: expose action, assign it to the configured global var,
		// and carry any per-action goto target for direct routing.
		outVar := firstNonEmptyStr(str(node.Config["output_var"]), "action")
		c.setVar(outVar, action)
		e.persistVar(runID, outVar, action)
		// Persist each form field as a global variable too, so downstream nodes
		// and conditional injection can reference the reviewer's input (e.g. the
		// review comment) via {{vars.<key>}}.
		for k, v := range form {
			if k == "" {
				continue
			}
			c.setVar(k, v)
			e.persistVar(runID, k, v)
		}
		// human_gate only when body is page.html (HtmlPreview Issue path).
		// Avoid wiping vars.preview_issues on comment-only gates.
		if shouldSnapshotPreviewIssues(node) {
			var lifeErr error
			if isPassGateAction(action) {
				// Pass must force-clear snapshot vars (bypass empty-list skip) and
				// resolve any residual open issues for this node.
				e.forceClearPreviewIssueVars(c, runID)
				lifeErr = e.markPreviewIssuesResolvedByNode(runID, nodeID)
			} else {
				lifeErr = e.snapshotPreviewIssues(c, runID, nodeID)
			}
			if lifeErr != nil {
				// Gate was already marked resolved above; roll it back so the
				// reviewer can retry instead of leaving "vars snapshotted / DB
				// still open" half-success that falsely re-locks Pass.
				gate.Resolved = false
				logDB(e.db.Save(&gate), runID, "rollback gate after preview-issue lifecycle failure")
				return lifeErr
			}
		}
		reviewerActor := services.ActorFromUsername(reviewer)
		outputs := map[string]any{"action": action, "form": form, "reviewer_id": reviewerActor.Username, outVar: action}
		if reviewerActor.Unattributable {
			outputs["reviewer_unattributable"] = true
		}
		outcome = nodeOutcome{status: "completed", outputs: outputs, outputMd: "审批:" + action}
		for _, a := range parseActions(node.Config["actions"]) {
			if a.ID == action && a.Goto != "" {
				outcome.goto_ = a.Goto
			}
		}
	}
	e.saveState(c, node, outcome)
	c.nodeOutputs[nodeID] = outcome.outputs
	// Retire the upstream producer's parked review session (kept alive so this
	// gate could ReAct-reject into it): the decision is final now, so its
	// sandbox is released. No-op when there was no parked session.
	e.retireGateUpstreamSession(c, node)
	e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "resume", Detail: "action=" + action})
	e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "exit"})
	log.Info().Str("run_id", runID).Str("node_id", nodeID).Str("transition", "resume").Msg("gate resumed")

	// Project audit: gate decision with real Session actor when provided.
	if projectID := services.ResolveProjectIDForRun(e.db, runID); projectID != "" {
		rec := services.AuditRecord{
			ProjectID:    projectID,
			Actor:        services.ActorFromUsername(reviewer),
			Action:       models.AuditActionGateDecide,
			ResourceType: "gate",
			ResourceID:   nodeID,
			RunID:        runID,
			NodeID:       nodeID,
			Outcome:      models.AuditOutcomeOK,
			Summary:      "gate " + action,
			Payload: map[string]any{
				"runId":  runID,
				"action": action,
				"nodeId": nodeID,
				"form":   form,
			},
		}
		if opts.callerKind != "" {
			rec.CallerKind = opts.callerKind
			if opts.externalName != "" {
				rec.Payload["externalName"] = opts.externalName
			}
			rec.Payload["external"] = true
			if opts.callerKind == models.CallerKindExternal {
				rec.Summary = "external gate " + action
			}
		}
		e.recordAudit(rec)
	}

	e.recordGateFeedback(c, node, gate, action, form, reviewer, opts)

	if e.shareRevoker != nil && node.Type == "human_gate" {
		e.shareRevoker.RevokeUnusedForGate(runID, nodeID, gate.Iteration)
	}

	next := e.routeSuccess(c, node, outcome)
	if next == "" {
		e.finish(runID, "completed")
		return nil
	}
	go e.resumeAdmitted(runID, next)
	return nil
}

// shouldSnapshotPreviewIssues reports whether resume should write
// vars.preview_issues for this gate. human_gate only when body_template binds
// page.html (HtmlPreview Issue path). app_preview clears preview_issues on
// review confirm (reviewReply) and no longer resumes via Gate.
func shouldSnapshotPreviewIssues(node *models.Node) bool {
	if node == nil {
		return false
	}
	if node.Type != "human_gate" {
		return false
	}
	for _, p := range gatenode.GatePrimaryProducts(node, nil) {
		if p.Name == "page.html" || p.OutputKey == "page" {
			return true
		}
	}
	return false
}

// previewIssuesVarNonEmpty reports whether an existing preview_issues value
// carries text and/or images (used to avoid empty snapshots wiping prior data).
func previewIssuesVarNonEmpty(v any) bool {
	if v == nil {
		return false
	}
	if ct := models.AsCompositeText(v); ct != nil {
		return strings.TrimSpace(ct.Text) != "" || len(ct.Images) > 0
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) != ""
	default:
		return false
	}
}

// isPassGateAction reports whether the gate action is a Pass/approve path
// (same ids as GateApproval PASS_ACTION_IDS).
func isPassGateAction(action string) bool {
	return action == "pass" || action == "approve"
}

// forceClearPreviewIssueVars clears the PreviewIssue snapshot variables so Pass
// never leaves stale Fail text for downstream consumers.
func (e *Engine) forceClearPreviewIssueVars(c *execCtx, runID string) {
	c.setVar("preview_issues", "")
	e.persistVar(runID, "preview_issues", "")
	c.setVar("preview_issues_count", 0)
	e.persistVar(runID, "preview_issues_count", 0)
}

// markPreviewIssuesResolvedByNode bulk-marks open PreviewIssues for this
// run+node as resolved (engine-owned lifecycle; downstream must not call this).
// Returns the DB error so ResumeGate can abort and roll back rather than
// advancing with open rows still locking Pass on rereview.
func (e *Engine) markPreviewIssuesResolvedByNode(runID, nodeID string) error {
	if err := e.issueService().MarkResolvedByNode(runID, nodeID); err != nil {
		log.Error().Str("run_id", runID).Str("node_id", nodeID).Err(err).
			Msg("mark preview issues resolved: update failed")
		return fmt.Errorf("mark preview issues resolved: %w", err)
	}
	return nil
}

// snapshotPreviewIssues writes the human-reported preview issues for this
// app_preview / human_gate (HtmlPreview) node into two run variables:
//   - preview_issues: a composite {text, images[]} value whose text is a
//     numbered list of the problems and whose images are every attached
//     screenshot, so a downstream node referencing {{vars.preview_issues}}
//     gets the problems injected AND the screenshots attached to its prompt.
//   - preview_issues_count: the number of issues, for optional guard routing.
//
// It is a snapshot at resume time (the point the reviewer submits 退回),
// which is when the full set of reported issues is known. Only status=open
// rows are included; after a successful write, those rows are MarkResolved
// so a later review does not lock Pass on already-handled issues.
// When this node has no open issues but the run already holds a non-empty
// preview_issues (e.g. an earlier gate), leave the prior snapshot intact.
// Load failure aborts without MarkResolved so open rows stay actionable.
// MarkResolved failure is returned so ResumeGate can roll back the gate.
func (e *Engine) snapshotPreviewIssues(c *execCtx, runID, nodeID string) error {
	var issues []models.PreviewIssue
	if err := e.db.Where("run_id = ? AND node_id = ? AND status = ?", runID, nodeID, "open").
		Order("created_at asc").Find(&issues).Error; err != nil {
		// Do not write vars or MarkResolved: downstream must not lose context
		// while open issues are cleared from the gate UI. Resume continues —
		// open rows stay actionable for the next review (NFR).
		log.Error().Str("run_id", runID).Str("node_id", nodeID).Err(err).
			Msg("snapshot preview issues: load failed")
		return nil
	}

	if len(issues) == 0 && previewIssuesVarNonEmpty(c.vars["preview_issues"]) {
		log.Info().Str("run_id", runID).Str("node_id", nodeID).
			Msg("snapshot preview issues: skip empty overwrite of existing vars")
		return nil
	}

	var b strings.Builder
	var images []models.PromptImage
	for i, is := range issues {
		fmt.Fprintf(&b, "%d. ", i+1)
		if s := strings.TrimSpace(is.Selector); s != "" {
			fmt.Fprintf(&b, "[选中: %s] ", s)
		}
		b.WriteString(strings.TrimSpace(is.Body))
		if n := len(is.Images); n > 0 {
			fmt.Fprintf(&b, "(附 %d 张截图)", n)
			images = append(images, is.Images...)
		}
		b.WriteString("\n")
	}

	value := any(strings.TrimSpace(b.String()))
	if len(images) > 0 {
		value = models.CompositeText{Text: strings.TrimSpace(b.String()), Images: images}
	}
	c.setVar("preview_issues", value)
	e.persistVar(runID, "preview_issues", value)
	c.setVar("preview_issues_count", len(issues))
	e.persistVar(runID, "preview_issues_count", len(issues))

	e.recordPreviewFeedback(c, nodeID, issues)

	// Snapshot succeeded — close the lifecycle for this node's open issues.
	return e.markPreviewIssuesResolvedByNode(runID, nodeID)
}

// ReactReply advances a react clarify dialogue OR a post-run ReAct review
// dialogue. For a clarify (react) node completion is agent-driven (finishes
// when the agent raises no further questions, force, or the round cap). For a
// review node the two actions are explicit: force=false does one in-place edit
// and STAYS paused ("继续改"); force=true asks the agent to wrap up leftover
// dirty git (if any), re-validates the store snapshot, and advances the FSM
// ("确认并流转").
// Annotations are folded into the human instruction so the agent edits the
// exact cited spots on revise turns.
//
// Non-force clarify and review turns enqueue onto the platform FIFO and return
// immediately (SandboxChat-aligned); human/agent bubbles materialize on turn_begin.
func (e *Engine) ReactReply(runID, nodeID, humanText string, images []models.PromptImage, annotations []models.ReactAnnotation, force bool) error {
	return e.ReactReplyAs("", runID, nodeID, humanText, images, annotations, force)
}

// ReactReplyAs is ReactReply sent by owner (a pagebridge owner id).
func (e *Engine) ReactReplyAs(owner, runID, nodeID, humanText string, images []models.PromptImage, annotations []models.ReactAnnotation, force bool) error {
	return e.reactReply(owner, runID, nodeID, humanText, images, annotations, force, false)
}

// ReactReplyRetryLast covers the last human turn after an empty/failed agent
// reply without inserting another human row (plan g2.1).
func (e *Engine) ReactReplyRetryLast(runID, nodeID string) error {
	return e.ReactReplyRetryLastAs("", runID, nodeID)
}

// ReactReplyRetryLastAs is ReactReplyRetryLast sent by owner.
func (e *Engine) ReactReplyRetryLastAs(owner, runID, nodeID string) error {
	return e.reactReply(owner, runID, nodeID, "", nil, nil, false, true)
}

func (e *Engine) reactReply(owner, runID, nodeID, humanText string, images []models.PromptImage, annotations []models.ReactAnnotation, force, retryLast bool) error {
	if e.IsHalted() {
		return errors.New("server is shutting down")
	}
	if retryLast && force {
		return errors.New("retryLast cannot be combined with force")
	}
	var err error
	if !retryLast {
		images, err = blob.IngestPromptImages(context.Background(), e.blobs, images)
		if err != nil {
			return fmt.Errorf("ingest attachments: %w", err)
		}
	}

	cPeek, peekErr := e.loadCtx(runID)
	if peekErr == nil {
		if n := cPeek.graph.FindNode(nodeID); n != nil {
			if isReviewNode(n.Type) {
				if retryLast {
					return errors.New("retryLast is only supported for clarify/approve dialogues")
				}
				// Review !force: enqueue onto the platform FIFO (SandboxChat-aligned)
				// and return immediately — human/agent bubbles materialize on turn_begin.
				if !force {
					var convPeek models.ReactConversation
					if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
						Order("iteration desc, id desc").First(&convPeek).Error; err != nil {
						return errors.New("no react conversation")
					}
					if convPeek.Done {
						return errors.New("react already done")
					}
					_, err := e.EnqueueReviewTurnAs(owner, runID, nodeID, humanText, images, annotations, "node", "")
					return err
				}
				// Review force (确认并流转): only when ready (no active turn, empty queue).
				// Cancel ≠ confirm — do not preempt via RetireSession while busy.
				if !e.ReviewSessionReady(runID, nodeID) {
					return errors.New("复审进行中或待发送队列非空,请先 Cancel 或等待完成后再确认")
				}
			} else if nodereg.ClarifyInteractive(n.Type) && !force {
				// Classic clarify !force: same FIFO / WS / refresh-resume as review.
				var convPeek models.ReactConversation
				if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
					Order("iteration desc, id desc").First(&convPeek).Error; err != nil {
					return errors.New("no react conversation")
				}
				if convPeek.Done {
					return errors.New("react already done")
				}
				if retryLast {
					_, err := e.enqueueClarifyRetryLast(owner, runID, nodeID)
					return err
				}
				_, err := e.EnqueueClarifyTurnAs(owner, runID, nodeID, humanText, images, annotations)
				return err
			} else if nodereg.ClarifyInteractive(n.Type) && force {
				// Clarify force finish: only when session idle (no in-flight / queue).
				if !e.ReviewSessionReady(runID, nodeID) {
					return errors.New("澄清进行中或待发送队列非空,请先 Cancel 或等待完成后再结束")
				}
			}
		}
	}

	// Hold the per-conversation lock across the whole reply (including the slow
	// provider call) so a duplicate submit — e.g. after a page refresh re-enables
	// the "确认完成" button while this request is still in flight — waits here,
	// then reads conv.Done == true below and is rejected with "react already done"
	// instead of appending a second turn and advancing the FSM twice.
	unlock := e.lockResume(runID + ":" + nodeID)
	defer unlock()

	c, err := e.loadCtx(runID)
	if err != nil {
		return err
	}
	node := c.graph.FindNode(nodeID)
	if node == nil || (!nodereg.ClarifyInteractive(node.Type) && !isReviewNode(node.Type)) {
		return errors.New("react node not found")
	}
	if err := e.checkAgentProfileProject(c, node); err != nil {
		return err
	}
	// Reply to the LATEST conversation for this node (highest iteration): a
	// loop-back opens a fresh dialogue per visit; older done ones are history.
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return errors.New("no react conversation")
	}
	if conv.Done {
		return errors.New("react already done")
	}

	// Review force: human turn is recorded, then git wrap-up + finalize.
	// Review !force already returned via EnqueueReviewTurn above.
	if isReviewNode(node.Type) {
		if !force {
			return errors.New("internal: review non-force must enqueue")
		}
		now := time.Now().Format(time.RFC3339)
		conv.Messages = append(conv.Messages, models.ReactMessage{Role: "human", Text: humanText, At: now,
			Images: images, Annotations: annotations})
		logDB(e.db.Save(&conv), runID, "save react human turn")
		return e.reviewReply(c, node, &conv, renderReviewHuman(humanText, annotations), images, true)
	}

	// Classic clarify !force already returned via EnqueueClarifyTurn above.
	// Only force wrap-up remains on the synchronous path.
	if !force {
		return errors.New("internal: clarify non-force must enqueue")
	}

	now := time.Now().Format(time.RFC3339)
	effective := renderReviewHuman(humanText, annotations)
	humanMsg := models.ReactMessage{Role: "human", Text: humanText, At: now,
		Images: images, Annotations: annotations}
	conv.Messages = append(conv.Messages, humanMsg)
	logDB(e.db.Save(&conv), runID, "save react human turn")

	// Leave the pending inbox immediately (mirrors ResumeGate resolving before
	// slow work): refresh/reopen must not resurface this item while wrap-up runs.
	// Roll back Done when the agent stays open (questions / unfinished).
	conv.Done = true
	logDB(e.db.Save(&conv), runID, "confirm leave pending (force)")

	req := e.nodeReq(c, node)
	t := e.provider.ReactReply(context.Background(), req, conv.Messages, effective, images, force)
	agentMsg := models.ReactMessage{Role: "agent", Text: t.Msg,
		At: time.Now().Format(time.RFC3339), Questions: t.Questions, Forms: t.Forms}
	conv.Messages = append(conv.Messages, agentMsg)

	// Auto-clarify: if this node runs in auto mode and the agent asked more
	// questions, keep answering with the recommended option set (all recommended
	// for multi-select, or the first as fallback) instead of pausing for another
	// human reply.
	if !force && !t.Done && len(t.Questions) > 0 && len(t.Forms) == 0 && e.autoReactEnabled(c, node) {
		t = e.autoAdvanceReact(c, node, &conv, req, t)
	}

	if !t.Done {
		conv.Done = false
		logDB(e.db.Save(&conv), runID, "reopen react after force stay-open")
		// Persist this turn's MCP tool calls now (the node stays paused without a
		// saveState) so the timeline reflects every react round, not just the
		// opening one.
		e.flushMcpCalls(runID, nodeID)
		e.flushTokenUsage(runID, nodeID, t.Usage, t.UsageByModel)
		e.broker.Publish(runID, jsonMsg("react", runID, nodeID))
		return errors.New("仍有待确认问题或收尾未完成，无法确认并流转")
	}

	logDB(e.db.Save(&conv), runID, "finish react conversation")

	// The confirm round is the one that carries the dialogue's AgentSummary,
	// and it is recorded here rather than in the FIFO pump because force
	// wrap-up stays on this synchronous path.
	e.recordFeedback(e.confirmRoundFeedbackEvent(runID, nodeID, models.FeedbackKindClarify,
		conv.Iteration, humanMsg, agentMsg, t.AgentSummary))

	if t.Err != nil {
		outcome := nodeOutcome{status: "failed", err: t.Err.Error(), outputMd: t.Msg,
			retryable: true, events: t.Events, usage: t.Usage, usageByModel: t.UsageByModel}
		e.saveState(c, node, outcome)
		e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "resume", Detail: "react 失败"})
		next := e.routeFailure(c, node, outcome)
		if next == "" {
			e.finish(runID, "failed")
			return nil
		}
		go e.resumeAdmitted(runID, next)
		return nil
	}

	// Finalize: require node_complete, enforce produces, then optional RPC.
	outcome := e.finishAgentOutcome(c, node, t.Result, func(r runtime.NodeResult) nodeOutcome {
		return e.finalizeAgentProducts(c, node, r)
	})
	e.saveState(c, node, outcome)
	e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "resume", Detail: "react 完成"})

	if outcome.status == "failed" {
		next := e.routeFailure(c, node, outcome)
		if next == "" {
			e.finish(runID, "failed")
			return nil
		}
		go e.resumeAdmitted(runID, next)
		return nil
	}

	c.nodeOutputs[nodeID] = t.Result.Outputs
	e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "exit"})

	next := e.routeSuccess(c, node, outcome)
	if next == "" {
		e.finish(runID, "completed")
		return nil
	}
	go e.resumeAdmitted(runID, next)
	return nil
}

// ResumeFrom re-drives a terminated (failed/cancelled) run from a chosen node,
// reusing the persisted variables, node outputs and artifacts already produced
// by the original run. This is "continue from the failure position": after a
// transient fault (e.g. a sandbox/ACP hiccup) has cleared, the operator retries
// the offending node instead of re-running the whole workflow from scratch.
//
// When nodeID is empty it resumes from the node that failed (the most recent
// failed execution). The re-entry opens a fresh StateRun for that node (the old
// failed row stays as history) and the FSM continues from there.
func (e *Engine) ResumeFrom(runID, nodeID string) error {
	if e.IsHalted() {
		return errors.New("server is shutting down")
	}
	// Serialize with any other resume of this run so a double click can't spawn
	// two drivers; the second caller observes the already-running status below.
	unlock := e.lockResume(runID + ":resume")
	defer unlock()

	c, err := e.loadCtx(runID)
	if err != nil {
		return err
	}
	// Default to the best resume point when the caller omits nodeID.
	if nodeID == "" {
		nodeID = e.pickAutoResumeNode(runID)
		if nodeID == "" {
			var cnt int64
			e.db.Model(&models.StateRun{}).Where("run_id = ?", runID).Count(&cnt)
			if cnt == 0 {
				return errors.New("run 无任何节点执行记录，无法续跑")
			}
			return errors.New("没有可续跑的节点")
		}
	}
	// Only a terminated-but-retryable run, or a running run whose target node
	// failed (react sandbox setup), can be manually resumed. A queued run
	// already has (or will get) a driver; a completed run has nothing to retry;
	// a waiting_human run continues through its gate/react reply.
	switch c.run.Status {
	case "failed", "cancelled":
	case "running":
		var sr models.StateRun
		if err := e.db.Where("run_id = ? AND node_id = ? AND status = ?", runID, nodeID, "failed").
			Order("iteration desc, id desc").First(&sr).Error; err != nil {
			return fmt.Errorf("run 当前状态 %q 不可续跑", c.run.Status)
		}
	default:
		return fmt.Errorf("run 当前状态 %q 不可续跑", c.run.Status)
	}
	if c.graph.FindNode(nodeID) == nil {
		return fmt.Errorf("节点 %q 不在该 run 的流程图中", nodeID)
	}
	// Guard against a second concurrent driver. The previous execute() goroutine
	// may still be unwinding (finish/saveState/endExecute) after status flips to
	// failed — poll briefly rather than rejecting an immediate resume.
	// When Cancel left a zombie driver blocked inside RunAgent, the DB is already
	// terminal but execRuns stays set: force-clear the slot (generation bump) so
	// the operator can continue instead of being stuck on "正在执行中".
	if !e.waitExecIdle(runID) {
		if c.run.Status == "failed" || c.run.Status == "cancelled" {
			log.Warn().Str("run_id", runID).Str("status", c.run.Status).
				Msg("resume-from: force-clear zombie exec slot on terminal run")
			e.forceEndExecute(runID)
			e.finalizeActiveStateRuns(runID, c.run.Status)
			// Abort the blocked provider call so it cannot race the new driver
			// on node_complete (outcomes are keyed only by run+node).
			if ab, ok := e.provider.(runtime.RunAborter); ok {
				ab.AbortRun(runID)
			}
		} else {
			return errors.New("run 正在执行中")
		}
	}
	// Restore the variable state as it was when the target node last started, so
	// the re-run sees exactly "当时的状态" instead of values that later nodes have
	// since mutated. Persisted to RunVariable so execute()'s fresh loadCtx picks
	// it up. Skipped for nodes with no recorded entry snapshot (e.g. older runs).
	if snap, ok := e.nodeStartVars(runID, nodeID); ok {
		e.restoreVars(c, snap)
	}
	// A cancelled/failed approve visit may have already delivered FirstMessage;
	// release the latch so the fresh visit opened by this resume can claim again.
	if c.run.FirstMessage != nil {
		e.releaseApproveFirstMessageLatch(runID)
		c.run.FirstMessageDeliveredAt = nil
	}
	// Record the manual resume in the trace before handing off to admission.
	// loadCtx already re-restored the MCP token from the persisted Run.McpToken
	// (finish() unregistered it on failure), so in-sandbox artifact writes
	// authorize again once the run is admitted.
	e.appendTrace(c, models.TraceEntry{NodeID: nodeID, Event: "resume", Detail: "从失败位置继续"})
	log.Info().Str("run_id", runID).Str("node_id", nodeID).Str("transition", "resume-from").Msg("run resumed from node")

	go e.resumeAdmitted(runID, nodeID)
	return nil
}

// nodeStartVars returns the global-variable snapshot captured when a node's most
// recent execution started, or ok=false when there is none to restore (the node
// never ran, or the run predates the VarsBefore capture).
func (e *Engine) nodeStartVars(runID, nodeID string) (map[string]any, bool) {
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		return nil, false
	}
	if sr.VarsBefore == nil {
		return nil, false
	}
	return sr.VarsBefore, true
}

// restoreVars rewinds the run's variables (in memory + persisted) to a snapshot,
// so a resumed node re-runs against the exact state it had at that time. Values
// created by later nodes (not in the snapshot) are dropped; existing rows keep
// their declared Type and just have their value rewound.
func (e *Engine) restoreVars(c *execCtx, snap map[string]any) {
	c.vars = map[string]any{}
	for k, v := range snap {
		c.vars[k] = v
	}
	err := e.db.Transaction(func(tx *gorm.DB) error {
		names := make([]string, 0, len(snap))
		for k := range snap {
			names = append(names, k)
		}
		del := tx.Where("run_id = ?", c.run.ID)
		if len(names) > 0 {
			del = del.Where("name NOT IN ?", names)
		}
		if err := del.Delete(&models.RunVariable{}).Error; err != nil {
			return err
		}
		for k, v := range snap {
			var rv models.RunVariable
			if err := tx.Where("run_id = ? AND name = ?", c.run.ID, k).First(&rv).Error; err == nil {
				rv.Value = v
				if err := tx.Save(&rv).Error; err != nil {
					return err
				}
			} else if err := tx.Create(&models.RunVariable{RunID: c.run.ID, Name: k, Type: inferType(v), Value: v}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Error().Str("run_id", c.run.ID).Err(err).Msg("restore vars-at-that-time failed")
	}
}

// pickAutoResumeNode chooses the best node to resume when the caller omits
// nodeID. It tries, in order: the latest failed/cancelled StateRun, then the
// latest still-running StateRun (orphaned by an interrupt), then the latest
// StateRun of any status (re-run the last visited node).
func (e *Engine) pickAutoResumeNode(runID string) string {
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND status IN ?", runID, []string{"failed", "cancelled"}).
		Order("iteration desc, id desc").First(&sr).Error; err == nil {
		return sr.NodeID
	}
	if err := e.db.Where("run_id = ? AND status = ?", runID, "running").
		Order("iteration desc, id desc").First(&sr).Error; err == nil {
		return sr.NodeID
	}
	if err := e.db.Where("run_id = ?", runID).
		Order("iteration desc, id desc").First(&sr).Error; err == nil {
		return sr.NodeID
	}
	return ""
}

// waitExecIdle polls until no execute() goroutine is driving runID, or times out.
func (e *Engine) waitExecIdle(runID string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if !e.isExecuting(runID) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// isExecuting reports whether a run currently has an active FSM driver.
func (e *Engine) isExecuting(runID string) bool {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	return e.execRuns[runID]
}

// Cancel marks a run cancelled.
func (e *Engine) Cancel(runID string) error {
	var run models.Run
	if err := e.db.First(&run, "id = ?", runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("run not found")
		}
		return fmt.Errorf("load run: %w", err)
	}
	switch run.Status {
	case "completed":
		return fmt.Errorf("run already finished: %s", run.Status)
	case "failed", "cancelled":
		// Idempotent heal: Cancel-during-agent can leave StateRuns "running" and
		// a zombie execRuns claim that blocks ResumeFrom. Re-finalize + drop the
		// slot so the operator can continue without restarting the server.
		e.finalizeActiveStateRuns(runID, run.Status)
		e.forceEndExecute(runID)
		if ab, ok := e.provider.(runtime.RunAborter); ok {
			ab.AbortRun(runID)
		}
		return nil
	}
	e.finish(runID, "cancelled")
	// Drop the in-memory driver claim immediately so ResumeFrom is not blocked
	// for the remainder of a long RunAgent call. The late driver observes the
	// terminal status and exits without routing; its endExecute is gen-guarded.
	e.forceEndExecute(runID)
	if run.FirstMessage != nil {
		e.releaseApproveFirstMessageLatch(runID)
	}
	return nil
}

// jsonMsg builds a minimal WS notification frame. The fields are engine-owned
// identifiers (run/node ids), so no marshaling can fail — hence no error return.
func jsonMsg(typ, runID, nodeID string) []byte {
	b, _ := json.Marshal(struct {
		Type   string `json:"type"`
		RunID  string `json:"runId"`
		NodeID string `json:"nodeId"`
	}{typ, runID, nodeID})
	return b
}

func artifactEditMsg(runID, nodeID, name, previewArtifact string) []byte {
	b, _ := json.Marshal(struct {
		Type            string `json:"type"`
		RunID           string `json:"runId"`
		NodeID          string `json:"nodeId"`
		Name            string `json:"name,omitempty"`
		PreviewArtifact string `json:"previewArtifact,omitempty"`
	}{"artifact_edit", runID, nodeID, name, previewArtifact})
	return b
}
