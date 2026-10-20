package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// Takeover error codes (HTTP 409 bodies; 404 for the not-found pair).
const (
	TakeoverNotAutoNode    = "not_auto_node"
	TakeoverNodeNotCurrent = "node_not_current"
	TakeoverNodeFinishing  = "node_finishing"
	TakeoverRunNotRunning  = "run_not_running"
	TakeoverServerHalting  = "server_halting"
	TakeoverRunNotFound    = "run_not_found"
	TakeoverNodeNotFound   = "node_not_found"
)

// Takeover result statuses / node states.
const (
	TakeoverStatusTakenOver     = "taken_over"
	TakeoverStatusAlreadyManual = "already_manual"
	NodeStateRunning            = "running"
	NodeStateStopping           = "stopping"
	NodeStateWaitingHuman       = "waiting_human"
	NodeStateBusy               = "busy"
)

// TakeoverCurrent is the server's view returned alongside a 409.
type TakeoverCurrent struct {
	RunStatus string `json:"runStatus"`
	NodeID    string `json:"nodeId,omitempty"`
	Iteration int    `json:"iteration,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

// TakeoverError is a typed refusal of a takeover request.
type TakeoverError struct {
	Code    string
	Message string
	Current TakeoverCurrent
}

func (e *TakeoverError) Error() string { return e.Code + ": " + e.Message }

// NotFound reports whether the error maps to HTTP 404.
func (e *TakeoverError) NotFound() bool {
	return e.Code == TakeoverRunNotFound || e.Code == TakeoverNodeNotFound
}

// TakeoverResult is the 200 body of a takeover request.
type TakeoverResult struct {
	Status    string `json:"status"`
	RunID     string `json:"runId"`
	NodeID    string `json:"nodeId"`
	Iteration int    `json:"iteration"`
	Mode      string `json:"mode"`
	State     string `json:"state"`
}

// autoRun tracks one in-flight auto Agent call so a takeover can cancel it.
// state transitions happen under Engine.autoMu: running → taken (takeover won)
// or running → done (RunAgent returned first). Exactly one side wins.
type autoRun struct {
	iter       int
	stateRunID uint
	cancel     context.CancelCauseFunc
	state      int
}

const (
	autoRunning = iota
	autoTaken
	autoDone
)

func autoKey(runID, nodeID string) string { return runID + "|" + nodeID }

func (e *Engine) autoRunsMap() map[string]*autoRun {
	if e.autoRuns == nil {
		e.autoRuns = map[string]*autoRun{}
	}
	return e.autoRuns
}

// beginAutoAgent registers the visit's cancellable RunAgent ctx.
func (e *Engine) beginAutoAgent(c *execCtx, node *models.Node) (context.Context, *autoRun) {
	ctx, cancel := context.WithCancelCause(context.Background())
	iter := c.iter[node.ID]
	var sr models.StateRun
	_ = e.db.Select("id").Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).
		Order("id desc").First(&sr).Error
	ar := &autoRun{iter: iter, stateRunID: sr.ID, cancel: cancel}
	e.autoMu.Lock()
	e.autoRunsMap()[autoKey(c.run.ID, node.ID)] = ar
	e.autoMu.Unlock()
	return ctx, ar
}

// settleAutoAgent resolves the race with a takeover once RunAgent returned.
// It reports true when the takeover won (the visit must become manual).
func (e *Engine) settleAutoAgent(runID, nodeID string, ar *autoRun) bool {
	e.autoMu.Lock()
	defer e.autoMu.Unlock()
	m := e.autoRunsMap()
	if m[autoKey(runID, nodeID)] == ar {
		delete(m, autoKey(runID, nodeID))
	}
	taken := ar.state == autoTaken
	if !taken {
		ar.state = autoDone
	}
	ar.cancel(nil)
	return taken
}

// TakeoverNode interrupts the auto Agent currently running nodeID and turns
// this visit into a manual session (see docs: node takeover). iteration 0
// means "the current visit". The call returns once the takeover is decided
// and the cancel is issued; the conversion to waiting_human is asynchronous
// and announced by a second node_takeover WS event.
func (e *Engine) TakeoverNode(runID, nodeID string, iteration int) (TakeoverResult, error) {
	res := TakeoverResult{RunID: runID, NodeID: nodeID, Mode: models.NodeModeManual}
	if e.IsHalted() {
		return res, &TakeoverError{Code: TakeoverServerHalting, Message: "服务正在停机"}
	}
	var run models.Run
	if err := e.db.First(&run, "id = ?", runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return res, &TakeoverError{Code: TakeoverRunNotFound, Message: "run not found"}
		}
		return res, err
	}
	node := run.Graph.FindNode(nodeID)
	if node == nil {
		return res, &TakeoverError{Code: TakeoverNodeNotFound, Message: "node not found"}
	}
	var sr models.StateRun
	hasSR := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error == nil
	cur := TakeoverCurrent{RunStatus: run.Status}
	if hasSR {
		cur.NodeID, cur.Iteration, cur.Mode = sr.NodeID, sr.Iteration, visitMode(sr)
	}
	refuse := func(code, msg string) (TakeoverResult, error) {
		return res, &TakeoverError{Code: code, Message: msg, Current: cur}
	}
	if node.Type != "agent" || node.Caps == nil || node.Caps.Clarify() {
		return refuse(TakeoverNotAutoNode, "只有自动 Agent 节点可以接管")
	}
	staleIter := iteration > 0 && hasSR && iteration != sr.Iteration
	// Idempotent: this visit is already manual (taken over, or parked in its
	// post-run review which is already a human session).
	if hasSR && !staleIter {
		res.Iteration = sr.Iteration
		if sr.IsManual() && (sr.Status == "running" || sr.Status == "waiting_human") {
			res.Status = TakeoverStatusAlreadyManual
			res.State = NodeStateStopping
			if sr.Status == "waiting_human" {
				res.State = NodeStateWaitingHuman
			}
			return res, nil
		}
		if sr.Status == "waiting_human" && node.Caps.ReviewEnabled() {
			res.Status, res.State = TakeoverStatusAlreadyManual, NodeStateWaitingHuman
			return res, nil
		}
	}
	switch run.Status {
	case "running":
	case "waiting_human":
		return refuse(TakeoverNodeNotCurrent, "该节点不是当前执行中的节点")
	default:
		return refuse(TakeoverRunNotRunning, "运行未在执行中: "+run.Status)
	}
	if !hasSR || staleIter || sr.Status != "running" {
		return refuse(TakeoverNodeNotCurrent, "该节点不是当前执行中的节点")
	}

	e.autoMu.Lock()
	ar := e.autoRunsMap()[autoKey(runID, nodeID)]
	if ar != nil && ar.iter == sr.Iteration && ar.state == autoTaken {
		// A concurrent request (double click / second tab) already won this
		// visit's takeover between our pre-lock read and the lock: idempotent.
		e.autoMu.Unlock()
		res.Iteration, res.Status, res.State = sr.Iteration, TakeoverStatusAlreadyManual, NodeStateStopping
		return res, nil
	}
	if ar == nil || ar.iter != sr.Iteration || ar.state != autoRunning {
		e.autoMu.Unlock()
		return refuse(TakeoverNodeFinishing, "节点正在结算或切换，请刷新后重试")
	}
	now := time.Now()
	upd := e.db.Model(&models.StateRun{}).
		Where("id = ? AND status = ? AND (mode = ? OR mode = '' OR mode IS NULL)", sr.ID, "running", models.NodeModeAuto).
		Updates(map[string]any{"mode": models.NodeModeManual, "taken_over_at": now})
	if upd.Error != nil || upd.RowsAffected == 0 {
		e.autoMu.Unlock()
		if upd.Error != nil {
			return res, upd.Error
		}
		var again models.StateRun
		if e.db.First(&again, sr.ID).Error == nil && again.IsManual() &&
			(again.Status == "running" || again.Status == "waiting_human") {
			res.Iteration, res.Status, res.State = again.Iteration, TakeoverStatusAlreadyManual, NodeStateStopping
			if again.Status == "waiting_human" {
				res.State = NodeStateWaitingHuman
			}
			return res, nil
		}
		// The CAS lost to a run cancel / failure that settled the visit:
		// report the terminal run, not a retryable "finishing".
		if st := e.runStatus(runID); st != "running" && st != "waiting_human" {
			cur.RunStatus = st
			return refuse(TakeoverRunNotRunning, "运行未在执行中: "+st)
		}
		return refuse(TakeoverNodeFinishing, "节点正在结算或切换，请刷新后重试")
	}
	ar.state = autoTaken
	ar.cancel(runtime.ErrNodeTakenOver)
	e.autoMu.Unlock()

	res.Iteration, res.Status, res.State = sr.Iteration, TakeoverStatusTakenOver, NodeStateStopping
	log.Info().Str("run_id", runID).Str("node_id", nodeID).Int("iteration", sr.Iteration).
		Msg("auto node taken over by user")
	e.touchActivity(runID, true)
	e.publishTakeover(runID, nodeID, sr.Iteration, NodeStateStopping)
	return res, nil
}

func visitMode(sr models.StateRun) string {
	if sr.IsManual() {
		return models.NodeModeManual
	}
	return models.NodeModeAuto
}

// publishTakeover pushes a node_takeover frame on the run's broker topic (the
// same channel GET /api/runs/:id/events streams).
func (e *Engine) publishTakeover(runID, nodeID string, iteration int, state string) {
	b, err := json.Marshal(map[string]any{
		"type": "node_takeover", "runId": runID, "nodeId": nodeID,
		"iteration": iteration, "mode": models.NodeModeManual, "state": state,
	})
	if err != nil {
		return
	}
	e.broker.Publish(runID, b)
}

// publishNodeEnter pushes a node_enter frame on the run's broker topic (the
// WS /api/runs/:id/events channel, like node_takeover) when the FSM opens a
// new visit of a node. kind/mode use the currentNode classification for a
// fresh (auto, not yet parked) visit: gate / clarify / agent.
func (e *Engine) publishNodeEnter(c *execCtx, node *models.Node) {
	kind, mode := services.ClassifyVisit(node, models.StateRun{Mode: models.NodeModeAuto}, false)
	b, err := json.Marshal(map[string]any{
		"type": "node_enter", "runId": c.run.ID, "nodeId": node.ID,
		"iteration": c.iter[node.ID], "name": services.GraphNodeLabel(c.graph, node.ID),
		"kind": kind, "mode": mode,
	})
	if err != nil {
		return
	}
	e.broker.Publish(c.run.ID, b)
}

// enterTakeover converts an interrupted auto visit into a parked manual
// session: a Takeover conversation seeded with what the agent said so far and
// a paused outcome, so executeFrom parks the run at waiting_human. It never
// yields failed / retryable, whatever RunAgent returned.
func (e *Engine) enterTakeover(c *execCtx, node *models.Node, res runtime.NodeResult, runErr error) nodeOutcome {
	out := nodeOutcome{status: "paused", outputMd: "已中断自动执行，转人工接管…", outputs: res.Outputs,
		events: res.Events, usage: res.Usage, usageByModel: res.UsageByModel, takeover: true}
	// The run was cancelled / failed (or this driver lost exec ownership)
	// while the taken-over agent was unwinding: executeFrom drops this
	// outcome, so open no manual session and leave the MCP host alone —
	// finish() already unregistered the run.
	if st := e.runStatus(c.run.ID); (st != "running" && st != "waiting_human") || !e.isExecOwner(c.run.ID, c.execGen) {
		log.Info().Str("run_id", c.run.ID).Str("node_id", node.ID).Str("run_status", st).
			Msg("takeover settled after run ended / ownership moved; no manual session")
		return out
	}
	e.seedTakeoverConversation(c.run.ID, node, c.iter[node.ID], res.OutputMd, res.Events, time.Now())
	e.applyTakeoverCaps(c, node.ID)
	e.host.SetActiveNode(c.run.ID, node.ID, c.graph.FindNode(node.ID).Caps)
	e.host.SetActiveReview(c.run.ID, true)
	if runErr != nil && !errors.Is(runErr, runtime.ErrNodeTakenOver) {
		log.Info().Str("run_id", c.run.ID).Str("node_id", node.ID).Err(runErr).
			Msg("takeover won over a RunAgent error; visit stays manual")
	}
	return out
}

// rollbackTakeover undoes enterTakeover's side effects when executeFrom drops
// the takeover outcome (run cancelled / exec ownership lost in the window
// after enterTakeover's own check): clear the MCP review phase and close the
// conversation so no open manual session dangles on a dead visit.
func (e *Engine) rollbackTakeover(runID, nodeID string, iter int) {
	e.host.SetActiveReview(runID, false)
	e.closeTakeoverConversation(runID, nodeID, iter)
}

// closeTakeoverConversation marks the visit's open Takeover conversation done.
func (e *Engine) closeTakeoverConversation(runID, nodeID string, iter int) {
	if iter < 1 {
		iter = 1
	}
	logDB(e.db.Model(&models.ReactConversation{}).
		Where("run_id = ? AND node_id = ? AND iteration = ? AND takeover = ? AND done = ?", runID, nodeID, iter, true, false).
		Update("done", true), runID, "close dropped takeover conversation")
}

const takeoverOpening = "已中断自动执行，本节点转为人工接管（不会再自动执行）。可以继续对话让我修改；确认无误后点「确认并流转」进入下一节点。"

// seedTakeoverConversation creates (or reopens) the visit's Takeover
// conversation. Idempotent per (run, node, iteration). The opening turn
// carries the auto phase: its aggregated ACP events and its time span
// (startedAt = the visit's start, endedAt = autoEnd). When events are present
// the chat renders the auto timeline itself, so the narration is not
// repeated in the text; without events the last auto narration is prepended.
func (e *Engine) seedTakeoverConversation(runID string, node *models.Node, iter int, narration string, events []models.AcpEvent, autoEnd time.Time) {
	if iter < 1 {
		iter = 1
	}
	var conv models.ReactConversation
	err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, node.ID, iter).
		Order("id desc").First(&conv).Error
	if err == nil {
		if !conv.Takeover || conv.Done {
			conv.Takeover, conv.Done = true, false
			logDB(e.db.Save(&conv), runID, "reopen takeover conversation")
		}
		return
	}
	auto := autoPhaseEvents(events)
	text := takeoverOpening
	if n := strings.TrimSpace(narration); n != "" && len(auto) == 0 {
		text = n + "\n\n---\n" + takeoverOpening
	}
	opening := models.ReactMessage{Role: "agent", Text: text, At: time.Now().Format(time.RFC3339), Events: auto}
	var sr models.StateRun
	if e.db.Select("id", "started_at").Where("run_id = ? AND node_id = ? AND iteration = ?", runID, node.ID, iter).
		Order("id desc").First(&sr).Error == nil && sr.StartedAt != nil {
		start := *sr.StartedAt
		opening.StartedAt = &start
	}
	end := autoEnd
	opening.EndedAt = &end
	conv = models.ReactConversation{RunID: runID, NodeID: node.ID, Iteration: iter, Takeover: true,
		Messages: []models.ReactMessage{opening}}
	logDB(e.db.Create(&conv), runID, "seed takeover conversation")
}

// applyTakeoverCaps makes a taken-over visit behave as a review node for the
// rest of the engine/provider (FIFO turns, confirm-and-advance, parked session,
// MCP review tools) without persisting anything to the run graph. The original
// caps are restored when the FSM opens a new visit of the node.
func (e *Engine) applyTakeoverCaps(c *execCtx, nodeID string) {
	node := c.graph.FindNode(nodeID)
	if node == nil || node.Caps == nil || node.Caps.Clarify() || node.Caps.Review {
		return
	}
	if c.takeoverCaps == nil {
		c.takeoverCaps = map[string]*models.AgentCapabilities{}
	}
	if _, done := c.takeoverCaps[nodeID]; done {
		return
	}
	c.takeoverCaps[nodeID] = node.Caps
	node.Caps = models.ManualReviewCaps(node.Caps)
}

// restoreTakeoverCaps undoes applyTakeoverCaps when a new visit starts: the
// next visit (loop-back / rerun) runs in auto again.
func (e *Engine) restoreTakeoverCaps(c *execCtx, node *models.Node) {
	if orig, ok := c.takeoverCaps[node.ID]; ok {
		node.Caps = orig
		delete(c.takeoverCaps, node.ID)
	}
}

// applyManualVisits re-applies takeover caps for every node whose latest
// visit is a live manual one (loadCtx).
func (e *Engine) applyManualVisits(c *execCtx, states []models.StateRun) {
	for _, s := range states {
		if models.ManualVisitLive(s, c.iter[s.NodeID]) {
			e.applyTakeoverCaps(c, s.NodeID)
		}
	}
}

// IsTakeoverVisit reports whether the node's latest visit is a manual takeover.
func (e *Engine) IsTakeoverVisit(runID, nodeID string) bool {
	var sr models.StateRun
	if e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error != nil {
		return false
	}
	return sr.IsManual()
}

// reconcileManualVisit finishes a takeover the process died in the middle of:
// the visit row is manual but still running. It is never re-adopted or re-run
// in auto; it becomes a paused manual session in a cold sandbox (the first
// manual turn rehydrates one).
func (e *Engine) reconcileManualVisit(run *models.Run, sr models.StateRun) {
	node := run.Graph.FindNode(sr.NodeID)
	if node != nil {
		// Cold restart: continue from the visit's persisted timeline.
		ended := time.Now()
		if sr.TakenOverAt != nil {
			ended = *sr.TakenOverAt
		}
		e.seedTakeoverConversation(run.ID, node, sr.Iteration, "", sr.Events, ended)
	}
	logDB(e.db.Model(&models.StateRun{}).Where("id = ?", sr.ID).
		Updates(map[string]any{"status": "waiting_human", "output_md": "已转人工接管（控制面重启，沙箱需冷启动）"}), run.ID, "reconcile manual visit")
	logDB(e.db.Model(&models.Run{}).Where("id = ? AND status = ?", run.ID, "running").
		Updates(map[string]any{"status": "waiting_human", "last_activity_at": time.Now()}), run.ID, "reconcile manual run")
	log.Info().Str("run_id", run.ID).Str("node_id", sr.NodeID).
		Msg("startup reconciliation: taken-over visit parked as manual (cold sandbox)")
}

// ManualSessionBusy reports whether a manual (review / takeover) session for
// the node has a turn running: an active platform FIFO turn or a turn the
// sandbox bridge itself reports as running.
func (e *Engine) ManualSessionBusy(runID, nodeID string) bool {
	if _, thinking := e.ReviewSessionState(runID, nodeID); thinking {
		return true
	}
	if bi, ok := e.provider.(runtime.SessionBridgeInspector); ok {
		if st, live := bi.SessionBridgeState(runID, nodeID); live && st.Busy {
			return true
		}
	}
	return false
}

// activityDebounce bounds how often ACP traffic bumps Run.last_activity_at.
var activityDebounce = 5 * time.Second

type activityTracker struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func (e *Engine) activity() *activityTracker {
	e.activityOnce.Do(func() {
		if e.act == nil {
			e.act = &activityTracker{last: map[string]time.Time{}, now: time.Now}
		}
	})
	return e.act
}

// touchActivity stamps Run.last_activity_at. force=false is the debounced
// path (ACP events, conversation traffic): at most one UPDATE per run per
// activityDebounce, kept in memory in between. force=true always writes
// (node enter/exit, takeover) and restarts the window.
func (e *Engine) touchActivity(runID string, force bool) {
	if runID == "" {
		return
	}
	t := e.activity()
	t.mu.Lock()
	now := t.now()
	last, ok := t.last[runID]
	if ok && !force && now.Sub(last) < activityDebounce {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	if !ok && terminalRunStatus(e.runStatus(runID)) {
		// A late ACP frame / save after finish() forgot the run: neither
		// stamp a finished run nor re-grow the debounce map for it.
		return
	}
	t.mu.Lock()
	t.last[runID] = now
	t.mu.Unlock()
	logDB(e.db.Model(&models.Run{}).Where("id = ?", runID).UpdateColumn("last_activity_at", now), runID, "touch activity")
}

// forgetActivity drops the debounce slot of a finished run.
func (e *Engine) forgetActivity(runID string) {
	t := e.activity()
	t.mu.Lock()
	delete(t.last, runID)
	t.mu.Unlock()
}

// isActivityEvent filters ACP frames that count as node activity.
func isActivityEvent(events []models.AcpEvent) bool {
	for _, ev := range events {
		if ev.Kind == "message" || ev.Kind == "tool_call" {
			return true
		}
	}
	return false
}

// stateRunTakeoverCols are owned by TakeoverNode / reconcile only. Every other
// load-modify-Save of a StateRun omits them so a stale copy loaded before a
// concurrent takeover can never write mode back to auto.
var stateRunTakeoverCols = []string{"mode", "taken_over_at"}

func terminalRunStatus(st string) bool {
	return st == "completed" || st == "failed" || st == "cancelled"
}

// autoPhaseEvents is the auto phase's timeline as carried on the takeover
// conversation's opening turn: the aggregated ACP events minus the auto
// prompt itself (the platform's full agent prompt, not a chat turn).
func autoPhaseEvents(events []models.AcpEvent) []models.AcpEvent {
	out := make([]models.AcpEvent, 0, len(events))
	for _, ev := range events {
		if ev.Kind == "prompt" {
			continue
		}
		out = append(out, ev)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
