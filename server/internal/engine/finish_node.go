package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// finishTrigger names what ended a node's execution: the FSM running it to
// the end (auto) or a person clicking「确认并流转」(confirm).
type finishTrigger string

const (
	triggerAuto    finishTrigger = "auto"
	triggerConfirm finishTrigger = "confirm"
)

// judgeKind is the verdict of stepJudge.
type judgeKind int

const (
	judgePass judgeKind = iota
	// judgeVerdictFail: the node concluded fail (a failing verdict product or
	// node_complete status=failed).
	judgeVerdictFail
	// judgeCheckFail: the node did not satisfy its contract (missing or
	// invalid product, business check rejected, execution error).
	judgeCheckFail
)

// finishOpts is what the entry point knows about the execution that ended.
type finishOpts struct {
	trigger finishTrigger
	// retire releases the node's parked session after the git and handoff
	// steps, before judging.
	retire bool
	// conv is the visit's dialogue, when it has one: wrap-up turns are
	// appended to it and a reopen flips it back open.
	conv *models.ReactConversation
	// resumeDetail, when set, is traced as the resume event before routing.
	resumeDetail string
}

// finishResult tells the entry point how to continue.
type finishResult struct {
	// next is the node to enter next ("" when none).
	next string
	// ended: the run reached a terminal status inside finishNode.
	ended bool
	// abandoned: this driver lost exec ownership; nothing was routed.
	abandoned bool
	// reopen is non-nil when the dialogue was reopened instead of routing; it
	// carries the reason shown to the person who confirmed.
	reopen error
}

// finishNode is the single end of every node execution: ② git → ③ handoff →
// ④ judge → (a failed check on a result that had already passed reopens the
// dialogue, everything else) ⑤ route. Each step looks at objective state
// only — live session, workspace, node_complete freshness, judged result,
// whether the visit already held a passed result, trigger and out edges.
func (e *Engine) finishNode(c *execCtx, node *models.Node, oc nodeOutcome, opts finishOpts) finishResult {
	held := e.hasPassedResult(c, node)
	wrapEvents := e.stepGit(c, node, opts.conv)
	oc, handoffEvents := e.stepHandoff(c, node, oc, held, opts.conv)
	wrapEvents = append(wrapEvents, handoffEvents...)
	if oc.status == "cancelled" {
		return finishResult{abandoned: true}
	}
	if opts.retire {
		if rp, ok := e.provider.(runtime.ReviewProvider); ok {
			rp.RetireSession(c.run.ID, node.ID)
		}
	}
	oc, kind := e.stepJudge(c, node, oc, held)
	if len(wrapEvents) > 0 {
		oc.events = append(wrapEvents, oc.events...)
	}
	if kind == judgeCheckFail && held {
		return finishResult{reopen: e.stepReopen(c, node, oc, opts.conv)}
	}
	return e.stepRoute(c, node, oc, opts)
}

// hasPassedResult reports whether the node's current visit already holds a
// result that passed (its row waits for a person, or was completed, with a
// successful node_complete) or that a person took over manually. Confirming
// such a visit revises an existing result rather than ending an execution.
func (e *Engine) hasPassedResult(c *execCtx, node *models.Node) bool {
	if e.db == nil || c == nil || node == nil {
		return false
	}
	q := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, node.ID)
	if iter := c.iter[node.ID]; iter > 0 {
		q = q.Where("iteration = ?", iter)
	}
	var sr models.StateRun
	if err := q.Order("iteration desc, id desc").First(&sr).Error; err != nil {
		return false
	}
	if sr.Status != "waiting_human" && sr.Status != "completed" {
		return false
	}
	return sr.IsManual() || str(sr.Outputs["outcome_status"]) == mcp.OutcomeSuccess
}

// liveReviewProvider returns the provider when node runs an Agent whose
// session is still parked; steps that need the model are skipped otherwise.
func (e *Engine) liveReviewProvider(c *execCtx, node *models.Node) (runtime.ReviewProvider, bool) {
	if node == nil || node.Caps == nil {
		return nil, false
	}
	rp, ok := e.provider.(runtime.ReviewProvider)
	if !ok || !rp.HasLiveSession(c.run.ID, node.ID) {
		return nil, false
	}
	return rp, true
}

// stepGit (②): when the workspace may be committed and is dirty, the agent
// gets one commit prompt; committed working branches are then pushed.
func (e *Engine) stepGit(c *execCtx, node *models.Node, conv *models.ReactConversation) []models.AcpEvent {
	rp, ok := e.liveReviewProvider(c, node)
	if !ok {
		return nil
	}
	req := e.nodeReq(c, node)
	req.LiveEdits = len(e.LiveSessions(c.run.ID, node.ID, false)) > 0
	return e.recordFinishTurn(c, node, conv, rp.CheckGit(context.Background(), req), "save git check turn")
}

// stepHandoff (③): a node_complete older than the node's last change is
// asked for again (with handoff), then the mark is drained into the result
// still to be judged. When the visit already held a passed result, a missing
// mark keeps the previous handoff instead of failing.
func (e *Engine) stepHandoff(c *execCtx, node *models.Node, oc nodeOutcome, held bool, conv *models.ReactConversation) (nodeOutcome, []models.AcpEvent) {
	var events []models.AcpEvent
	if rp, ok := e.liveReviewProvider(c, node); ok {
		req := e.nodeReq(c, node)
		events = e.recordFinishTurn(c, node, conv, rp.EnsureHandoff(context.Background(), req), "save handoff check turn")
	}
	return e.takeMark(c, node, oc, held), events
}

// takeMark drains node_complete into oc.result (the execution result still to
// judge). Outcomes without a pending result pass through unchanged.
func (e *Engine) takeMark(c *execCtx, node *models.Node, oc nodeOutcome, held bool) nodeOutcome {
	if oc.result == nil {
		return oc
	}
	res := *oc.result
	// Ownership before TakeOutcome: a Cancel/ResumeFrom zombie that returns
	// from RunAgent after a newer driver claimed the slot must not drain the
	// fresh visit's node_complete mark (host outcomes are keyed only by run+node).
	if c.execGen != 0 && !e.isExecOwner(c.run.ID, c.execGen) {
		return nodeOutcome{
			status: "cancelled", err: "lost exec ownership",
			outputMd: "dropped late outcome: lost exec ownership",
			events:   res.Events, usage: res.Usage, usageByModel: res.UsageByModel,
		}
	}
	if held {
		// Only a mark from this revision counts: a stale node_complete.json
		// may belong to another node of the run.
		if o, ok := e.host.TakeOutcome(c.run.ID, node.ID); ok {
			if fail, ok := applyNodeOutcome(&res, o); !ok {
				return fail
			}
		}
	} else if fail, ok := e.consumeNodeOutcome(c, node, &res); !ok {
		return fail
	}
	oc.result = &res
	return oc
}

// stepJudge (④) derives the outcome from the node's products and checks:
// required products, verdict products, business validation, and — when the
// visit revises a result that already passed — settling its preview issues.
func (e *Engine) stepJudge(c *execCtx, node *models.Node, oc nodeOutcome, held bool) (nodeOutcome, judgeKind) {
	if oc.result != nil {
		res := *oc.result
		oc = e.afterDefaultChecks(c, node, e.finalizeAgent(c, node, res))
		if oc.usage == nil {
			oc.usage = res.Usage
		}
		if oc.usageByModel == nil {
			oc.usageByModel = res.UsageByModel
		}
	}
	// Accepting a revised result settles the issues raised on its preview:
	// they must not leak into downstream prompts.
	if oc.status == "completed" && held && node.Caps.CanPreview() {
		e.forceClearPreviewIssueVars(c, c.run.ID)
		if err := e.markPreviewIssuesResolvedByNode(c.run.ID, node.ID); err != nil {
			oc.status = "failed"
			oc.err = err.Error()
			oc.outputMd = err.Error()
		}
	}
	e.retainHandoff(c, node, &oc)
	switch {
	case oc.status == "completed":
		return oc, judgePass
	case oc.verdict || oc.handle == handleFail:
		return oc, judgeVerdictFail
	default:
		return oc, judgeCheckFail
	}
}

// stepReopen keeps the visit's previous result and opens the dialogue again
// so the person can have the agent fix what the check rejected. Nothing is
// saved onto the visit's row.
func (e *Engine) stepReopen(c *execCtx, node *models.Node, oc nodeOutcome, conv *models.ReactConversation) error {
	runID := c.run.ID
	if conv == nil {
		var latest models.ReactConversation
		if err := e.db.Where("run_id = ? AND node_id = ?", runID, node.ID).
			Order("iteration desc, id desc").First(&latest).Error; err == nil {
			conv = &latest
		}
	}
	if conv != nil {
		conv.Done = false
		logDB(e.db.Save(conv), runID, "reopen dialogue after confirm check failure")
	}
	e.clearPendingAuto(c)
	e.host.SetActiveReview(runID, true)
	e.publishReact(runID, node.ID)
	msg := strings.TrimSpace(oc.err)
	if msg == "" {
		msg = "复审确认校验失败"
	}
	log.Warn().Str("run_id", runID).Str("node_id", node.ID).Str("err", msg).
		Msg("confirm failed its checks; dialogue reopened")
	return errors.New(msg)
}

// stepRoute (⑤) persists the outcome and leaves through the matching edge:
// pass → success edges; fail → rollback (within MaxAttempts) / failure edges /
// run failure. Only an unattended (auto) end auto-retries and notifies.
func (e *Engine) stepRoute(c *execCtx, node *models.Node, oc nodeOutcome, opts finishOpts) finishResult {
	runID := c.run.ID
	if opts.resumeDetail != "" {
		e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "resume", Detail: opts.resumeDetail})
	}
	e.saveState(c, node, oc)
	if oc.status == "failed" {
		log.Info().Str("run_id", runID).Str("node_id", node.ID).Str("err", oc.err).Msg("node failed")
		if oc.err != "" {
			c.setVar("last_error", oc.err)
			e.persistVar(runID, "last_error", oc.err)
		}
		next := e.routeFailure(c, node, oc)
		if next == "" {
			if opts.trigger == triggerAuto && e.tryAutoRetry(c, node, oc) {
				e.refreshProgress(c)
				return finishResult{next: node.ID}
			}
			e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit", Detail: "no failure transition; run failed"})
			if opts.trigger == triggerConfirm {
				e.clearPendingAuto(c)
			}
			if e.finish(runID, "failed") && opts.trigger == triggerAuto {
				e.fireRunNotify(c, node, models.NotifyKindFailed)
			}
			return finishResult{ended: true}
		}
		return e.continueTo(c, next, opts.trigger)
	}
	c.nodeOutputs[node.ID] = oc.outputs
	e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit"})
	next := e.routeSuccess(c, node, oc)
	if next == "" && opts.trigger == triggerConfirm {
		e.clearPendingAuto(c)
		e.refreshProgress(c)
		e.finish(runID, "completed")
		return finishResult{ended: true}
	}
	return e.continueTo(c, next, opts.trigger)
}

// continueTo hands the run on to next. A confirm's pending-auto mark (the
// successor the list already shows as starting) survives only when next is
// that successor.
func (e *Engine) continueTo(c *execCtx, next string, trigger finishTrigger) finishResult {
	if trigger == triggerConfirm {
		e.keepPendingAutoOnly(c, next)
	}
	e.rememberHandoffTarget(c, next)
	e.refreshProgress(c)
	return finishResult{next: next}
}

// recordFinishTurn attributes a wrap-up turn's tool calls and tokens to the
// visit and, when the agent said something, appends it to the dialogue. The
// turn's events are returned for the visit's timeline.
func (e *Engine) recordFinishTurn(c *execCtx, node *models.Node, conv *models.ReactConversation, t runtime.ReactTurn, why string) []models.AcpEvent {
	if strings.TrimSpace(t.Msg) == "" && len(t.Events) == 0 && t.Usage == nil && t.UsageByModel == nil {
		return nil
	}
	e.flushMcpCalls(c.run.ID, node.ID)
	e.flushTokenUsage(c.run.ID, node.ID, t.Usage, t.UsageByModel)
	if conv == nil || strings.TrimSpace(t.Msg) == "" {
		return t.Events
	}
	message := models.ReactMessage{Role: "agent", Text: t.Msg, At: time.Now().Format(time.RFC3339), OpID: t.OpID,
		Tools: models.ToolsFromEvents(t.Events), Parts: models.PartsForReply(t.Events, t.Msg)}
	applyReplyMetadata(&message, t)
	conv.Messages = append(conv.Messages, message)
	logDB(e.db.Save(conv), c.run.ID, why)
	return t.Events
}

// writeBackHandoff stores a refreshed node_complete handoff onto the node's
// latest execution, for a node whose result was already routed (an approval
// gate's upstream producer).
func (e *Engine) writeBackHandoff(c *execCtx, node *models.Node, outputs map[string]any) {
	text := strings.TrimSpace(str(outputs[models.OutputHandoff]))
	if text == "" {
		return
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, node.ID).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		return
	}
	if sr.Outputs == nil {
		sr.Outputs = map[string]any{}
	}
	sr.Outputs[models.OutputHandoff] = text
	sr.Outputs[models.OutputHandoffSource] = firstNonEmptyStr(str(outputs[models.OutputHandoffSource]), models.HandoffSourceAgent)
	logDB(e.db.Omit(stateRunTakeoverCols...).Save(&sr), c.run.ID, "write back refreshed handoff")
	c.nodeOutputs[node.ID] = sr.Outputs
}
