package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// Broker exposes the run-update pub/sub for the WS handler.
func (e *Engine) Broker() *Broker { return e.broker }

// LiveNodeEvents reads a running node's event log directly from its live
// sandbox (via the provider), if the node is currently executing here. ok is
// false otherwise, so the caller falls back to the persisted StateRun snapshot.
// A non-nil err means a live sandbox was found but the bridge read failed.
func (e *Engine) LiveNodeEvents(ctx context.Context, runID, nodeID string) ([]models.AcpEvent, bool, error) {
	src, ok := e.provider.(runtime.LiveEventSource)
	if !ok {
		return nil, false, nil
	}
	return src.LiveNodeEvents(ctx, runID, nodeID)
}

// LiveNodeEventsPage reads a page of events from a running node's live sandbox.
// A non-nil err means a live sandbox was found but the bridge read failed —
// callers must not treat that as an empty live page.
func (e *Engine) LiveNodeEventsPage(ctx context.Context, runID, nodeID, cursor string, limit int) ([]models.AcpEvent, string, bool, bool, error) {
	src, ok := e.provider.(runtime.LiveEventPageSource)
	if !ok {
		return nil, "", false, false, nil
	}
	return src.LiveNodeEventsPage(ctx, runID, nodeID, cursor, limit)
}

// InflightPrompts returns the prompt of each turn still streaming for runID,
// keyed by node id (nil when none or the provider does not track them).
func (e *Engine) InflightPrompts(runID string) map[string]runtime.InflightPrompt {
	src, ok := e.provider.(runtime.InflightPromptSource)
	if !ok {
		return nil
	}
	return src.InflightPrompts(runID)
}

// RecentTurns returns the finished, not-yet-necessarily-persisted bracketed
// turns of each node for runID (nil when the provider does not track them).
func (e *Engine) RecentTurns(runID string) map[string][]models.AcpEvent {
	src, ok := e.provider.(runtime.InflightPromptSource)
	if !ok {
		return nil
	}
	return src.RecentTurns(runID)
}

// publishAcp streams a running node's in-progress ACP events to subscribers of
// the run's WebSocket (live agent preview). It is best-effort: marshal failures
// or absent subscribers are silently dropped. The authoritative event log lives
// in the sandbox (queried on demand via LiveNodeEvents) and the final snapshot
// is persisted by saveState — this is purely the live push channel.
func (e *Engine) publishAcp(runID, nodeID string, events []models.AcpEvent, busy bool) {
	msg, err := json.Marshal(map[string]any{
		"type": "acp", "runId": runID, "nodeId": nodeID, "events": events, "busy": busy,
	})
	if err != nil {
		return
	}
	e.broker.Publish(runID, msg)
}

// startNodeRun appends a fresh in-flight StateRun (status=running) for the
// node's current execution index the moment the FSM enters it — for every node
// type, not just streaming ones. This makes each (re-)visit its own row so a
// loop-back / rollback retry never overwrites the previous execution's output,
// events, or duration, and a long-running node's streamed events (persisted via
// publishAcp) are queryable on refresh even before it completes.
func (e *Engine) startNodeRun(c *execCtx, node *models.Node) {
	now := time.Now()

	before := map[string]any{}
	for k, v := range c.vars {
		before[k] = v
	}
	// New visit / new iteration: drop stale Host outcome + audit artifact so a
	// prior attempt's mark cannot satisfy this visit (CAPA A7 / FR4).
	e.host.ClearOutcome(c.run.ID, node.ID)
	logDB(e.db.Create(&models.StateRun{
		RunID: c.run.ID, NodeID: node.ID, NodeType: node.Type,
		Iteration: c.iter[node.ID], Status: "running", StartedAt: &now, Attempt: c.run.Attempt,
		VarsBefore: before,
	}), c.run.ID, "startNodeRun")
}

// execCtx is the in-memory execution context, rebuildable from the DB so a
// run can resume after a human gate / react pause.
type execCtx struct {
	run         *models.Run
	graph       models.Graph
	vars        map[string]any
	nodeOutputs map[string]map[string]any
	// iter tracks the current per-node execution index (1-based). Seeded from
	// the DB in loadCtx so it survives a pause/resume, then bumped on each
	// enter. Every node write (StateRun / Gate / conversation) is stamped with
	// it, so a loop-back onto a node produces a fresh, separately-traceable
	// execution instead of overwriting the previous one.
	iter map[string]int
	// autoRetries tracks how many times each node has been auto-retried from
	// its failure position within THIS driver's lifetime (see tryAutoRetry).
	// In-memory only: a manual ResumeFrom starts a fresh driver and thus a
	// fresh budget, which is the intended "manual retry is separate" behavior.
	autoRetries map[string]int
	token       string
	// execGen is this driver's ownership token from beginExecute. After Cancel
	// force-clears the slot and ResumeFrom admits a newer driver, the zombie
	// must not TakeOutcome (node_complete is keyed only by run+node) or the
	// fresh visit loses its mark and fails with "未调用 node_complete".
	execGen uint64
}

// lockResume serializes resume operations for a given key (runID:nodeID),
// returning the unlock func. Concurrent callers (e.g. a re-submitted confirm
// after a page refresh) block until the in-flight one finishes, after which
// they observe the persisted Done/Resolved state and are rejected.
func (e *Engine) lockResume(key string) func() {
	e.resumeMu.Lock()
	m, ok := e.resumeLocks[key]
	if !ok {
		m = &sync.Mutex{}
		e.resumeLocks[key] = m
	}
	e.resumeMu.Unlock()
	m.Lock()
	return m.Unlock
}

func (e *Engine) loadCtx(runID string) (*execCtx, error) {
	var run models.Run
	if err := e.db.First(&run, "id = ?", runID).Error; err != nil {
		return nil, err
	}
	c := &execCtx{run: &run, graph: run.Graph,
		vars: map[string]any{}, nodeOutputs: map[string]map[string]any{},
		iter: map[string]int{}, autoRetries: map[string]int{}}
	var vars []models.RunVariable
	if err := e.db.Where("run_id = ?", runID).Find(&vars).Error; err != nil {
		return nil, fmt.Errorf("load run variables: %w", err)
	}
	for _, v := range vars {
		c.vars[v.Name] = v.Value
	}
	// Load the per-node execution history. nodeOutputs must reflect the LATEST
	// execution of each node, and c.iter must resume from the highest recorded
	// index so the next enter bumps it correctly (rather than re-numbering from
	// 1 after a pause/resume).
	var states []models.StateRun
	if err := e.db.Where("run_id = ?", runID).Order("iteration asc, id asc").Find(&states).Error; err != nil {
		return nil, fmt.Errorf("load run states: %w", err)
	}
	for _, s := range states {
		if s.Iteration > c.iter[s.NodeID] {
			c.iter[s.NodeID] = s.Iteration
		}
		if s.Outputs != nil {
			c.nodeOutputs[s.NodeID] = s.Outputs
		}
	}
	e.mu.Lock()
	c.token = e.tokens[runID]
	e.mu.Unlock()

	if c.token == "" && run.McpToken != "" {
		c.token = run.McpToken
		e.mu.Lock()
		e.tokens[runID] = run.McpToken
		e.mu.Unlock()
		e.host.RestoreRun(runID, run.McpToken)
	}
	return c, nil
}

func (e *Engine) evalContext(c *execCtx, extra map[string]any) EvalContext {
	return EvalContext{
		Vars: c.vars, Nodes: c.nodeOutputs, Extra: extra,
		Artifact: func(name string) (string, bool) { return e.store.Get(c.run.ID, name) },
	}
}

// beginExecute claims the sole execution slot for a run. It returns ok=false
// when another goroutine is already driving the run, so the caller must bail
// rather than run a second concurrent FSM loop over the same run (which would
// race on its persisted state and double-advance transitions). The returned gen
// must be passed to endExecute so a force-cleared zombie driver cannot drop a
// newer owner's claim.
func (e *Engine) beginExecute(runID string) (gen uint64, ok bool) {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.execRuns[runID] {
		return 0, false
	}
	e.execGen[runID]++
	e.execRuns[runID] = true
	return e.execGen[runID], true
}

func (e *Engine) endExecute(runID string, gen uint64) {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.execGen[runID] == gen {
		delete(e.execRuns, runID)
	}
}

// forceEndExecute drops a zombie exec slot (and bumps generation) so ResumeFrom
// can admit a fresh driver after Cancel left an agent call still blocked.
func (e *Engine) forceEndExecute(runID string) {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.execRuns[runID] {
		e.execGen[runID]++
		delete(e.execRuns, runID)
	}
}

// isExecOwner reports whether gen still owns runID's execute slot. After
// forceEndExecute + a newer beginExecute, the late zombie driver must abandon
// without routing — even if ResumeFrom has already flipped DB status back to
// running.
func (e *Engine) isExecOwner(runID string, gen uint64) bool {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	return e.execRuns[runID] && e.execGen[runID] == gen
}

// acquireExecuteSlot polls beginExecute for up to ~2s. The only expected
// contention is the brief teardown window of a just-paused/failed driver (see
// execute's comment); a slot still held past that window means a genuinely
// live driver (e.g. a long node call), and the caller should bail.
func (e *Engine) acquireExecuteSlot(runID string) (gen uint64, ok bool) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if gen, ok = e.beginExecute(runID); ok {
			return gen, true
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// runAdmitted drives an already-admitted run (its concurrency slot is held by
// the caller — the dispatcher via TryAcquire, or a resume via Acquire). It runs
// the FSM loop, then releases the slot and wakes the dispatcher so the next
// queued run can be admitted. This centralizes the "release + wake" so the
// invariant holds: a run in the running state holds exactly one slot.
func (e *Engine) runAdmitted(runID, fromNodeID string) {
	defer func() {
		e.sem.Release()
		e.signalDispatch()
	}()
	e.execute(runID, fromNodeID)
}

// resumeAdmitted continues a paused or terminated run from fromNodeID via the
// same TryAcquire + queued fallback path as the priority-then-FIFO dispatcher.
// DB status is only promoted to running once a slot is actually held.
func (e *Engine) resumeAdmitted(runID, fromNodeID string) {
	e.scheduleRunAdmission(runID, fromNodeID)
}

// execute drives the FSM loop from the given node id. The caller must already
// hold a concurrency slot (see runAdmitted / resumeAdmitted).
func (e *Engine) execute(runID, fromNodeID string) {

	gen, ok := e.acquireExecuteSlot(runID)
	if !ok {
		log.Warn().Str("run_id", runID).Str("from", fromNodeID).Msg("execute skipped: run driver did not release its slot in time")
		e.requeueRun(runID, fromNodeID)
		return
	}
	defer e.endExecute(runID, gen)

	defer func() {
		if r := recover(); r != nil {
			log.Error().Str("run_id", runID).Interface("panic", r).
				Bytes("stack", debug.Stack()).Msg("execute goroutine panicked; marking run failed")
			e.failRun(runID, fmt.Sprintf("执行协程异常: %v", r))
		}
	}()

	c, err := e.loadCtx(runID)
	if err != nil {
		log.Error().Str("run_id", runID).Err(err).Msg("load run context failed")
		e.failRun(runID, "加载运行上下文失败: "+err.Error())
		return
	}
	c.execGen = gen

	if st := e.runStatus(runID); st == "cancelled" || st == "failed" || st == "completed" {
		log.Info().Str("run_id", runID).Str("status", st).Msg("execute aborted: run already terminal")
		return
	}

	cur := fromNodeID
	for cur != "" {

		if !e.isExecOwner(runID, gen) {
			log.Info().Str("run_id", runID).Str("from", cur).
				Msg("execute abandoned: lost exec ownership")
			return
		}
		node := c.graph.FindNode(cur)
		if node == nil {
			e.appendTrace(c, models.TraceEntry{NodeID: cur, Event: "exit", Detail: "node not found"})
			e.failRun(runID, "节点不存在: "+cur)
			return
		}

		if node.Checkpoint {
			e.snapshotCheckpoint(c, node.ID)
		}

		c.iter[node.ID]++
		e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "enter", Iteration: c.iter[node.ID]})
		e.startNodeRun(c, node)

		outcome := e.executeNode(c, node)

		if !e.isExecOwner(runID, gen) {
			e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit", Detail: "lost exec ownership; drop late outcome"})
			log.Info().Str("run_id", runID).Str("node_id", node.ID).
				Msg("execute stopped: lost exec ownership while node was in flight")
			return
		}

		if st := e.runStatus(runID); st == "cancelled" || st == "failed" {
			e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit", Detail: "run " + st + " during node; drop late outcome"})
			log.Info().Str("run_id", runID).Str("node_id", node.ID).Str("status", st).
				Msg("execute stopped: run became terminal while node was in flight")
			return
		}

		switch outcome.status {
		case "paused":

			e.saveState(c, node, outcome)
			e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "pause", Detail: outcome.outputMd})

			if e.pauseStillPending(runID, node) {
				if e.finish(runID, "waiting_human") {

					e.fireGateAutoInvoke(c, node)
					e.fireApproveFirstMessage(c, node)
					e.fireRunNotify(c, node, models.NotifyKindWaitingHuman)
				}
			}
			return
		case "failed":
			log.Info().Str("run_id", runID).Str("node_id", node.ID).Str("err", outcome.err).Msg("node failed")
			e.saveState(c, node, outcome)

			if nodereg.ClarifyInteractive(node.Type) && outcome.sandboxSetup {
				if outcome.err != "" {
					c.setVar("last_error", outcome.err)
					e.persistVar(runID, "last_error", outcome.err)
				}
				e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit", Detail: "sandbox setup failed"})
				e.refreshProgress(c)
				return
			}

			if outcome.err != "" {
				c.setVar("last_error", outcome.err)
				e.persistVar(runID, "last_error", outcome.err)
			}
			next := e.routeFailure(c, node, outcome)
			if next == "" {

				if e.tryAutoRetry(c, node, outcome) {
					cur = node.ID
					break
				}
				e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit", Detail: "no failure transition; run failed"})
				if e.finish(runID, "failed") {
					e.fireRunNotify(c, node, models.NotifyKindFailed)
				}
				return
			}
			cur = next
		default:
			e.saveState(c, node, outcome)
			c.nodeOutputs[node.ID] = outcome.outputs
			e.appendTrace(c, models.TraceEntry{NodeID: node.ID, Event: "exit"})
			next := e.routeSuccess(c, node, outcome)
			cur = next
		}
		e.refreshProgress(c)
	}
	// FSM drained to a terminal point. Normally status is still "running" and we
	// promote to completed. Under a resume/pause race (CI flake class), a late
	// finish("waiting_human") from the departing pause driver can clobber the
	// re-admitted "running" status after ReactReply/ResumeGate has already driven
	// this goroutine through remaining nodes — leaving status as waiting_human or
	// queued while every node has already executed. Coerce those to completed
	// too (cancelled/failed/completed are left alone).
	var run models.Run
	if err := e.db.Select("status").First(&run, "id = ?", runID).Error; err == nil {
		switch run.Status {
		case "running", "waiting_human", "queued":

			e.finish(runID, "completed")
			log.Info().Str("run_id", runID).Msg("run completed")
		}
	}
}
