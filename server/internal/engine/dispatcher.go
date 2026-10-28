package engine

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// dispatchPoll is a safety net: even without an explicit wake (e.g. a missed
// signal or a transient DB error on a prior drain) the dispatcher re-checks the
// queued backlog periodically.
const dispatchPoll = 5 * time.Second

// admissionFromCheckpoint stores the node id a resumed run should continue
// from when it is queued waiting for a concurrency slot. Persisted on Run so a
// restart can still admit the run at the correct FSM position.
const admissionFromCheckpoint = "__admission_from__"

// dispatch is the single scheduler goroutine. It admits queued runs up to the
// concurrency limit (Priority DESC → remaining human_gate → FIFO), blocking on
// the wake channel between drains. Being the sole admitter keeps admission
// strictly ordered.
func (e *Engine) dispatch() {
	for {
		e.drainQueue()
		select {
		case <-e.stop:
			return
		case <-e.wake:
		case <-time.After(dispatchPoll):
		}
	}
}

// drainQueue admits as many queued runs as free concurrency slots allow, in
// Priority DESC → hasRemainingHumanGate DESC → created_at ASC → id ASC order,
// then returns (either at capacity or with the queue empty).
func (e *Engine) drainQueue() {
	for {
		// Hold the halt read lock from the halt check through the claim and
		// the driver spawn. Once Halt returns, no queued run can be promoted
		// any more, and every admitted driver is already visible to
		// maintenance. Releasing the lock before the claim (as before) let a
		// tick that passed the check claim a run queued after Halt returned.
		e.haltMu.RLock()
		if e.halted || !e.sem.TryAcquire() {
			e.haltMu.RUnlock()
			return // at capacity; a Release + wake will resume draining
		}
		runID, from, ok, noStart := e.claimNextQueued()
		if ok {
			go e.runAdmitted(runID, from)
		}
		e.haltMu.RUnlock()
		if noStart != "" {
			// failRun → finish does provider / store work; keep it outside the lock.
			e.failRun(noStart, "工作流没有可执行节点")
		}
		if !ok {
			e.sem.Release()
			return // nothing admittable right now
		}
	}
}

// claimNextQueued atomically promotes the next queued run to running and
// returns its continue node. Selection order (do NOT rewrite Run.Priority to
// implement same-band tilt — Priority is the stored/badge band):
//
//	Priority DESC → hasRemainingHumanGate DESC → created_at ASC → id ASC
//
// Implementation: load all queued runs in the current highest Priority band,
// score remaining human_gate reachability in memory (from __admission_from__
// or StartNode), pick the head, then CAS UPDATE queued→running. Covering first
// enqueue and re-queue after waiting_human / execute early-exit. The claim is a
// guarded UPDATE so a run cancelled (or otherwise moved off "queued") between
// the read and the write is skipped (RowsAffected == 0) rather than double-run.
// Returns ok=false when there is nothing to admit; the caller then releases the
// tentatively-taken slot.
// noStartRunID is set (with ok=false) when the claimed run has no start node;
// the caller fails it outside the halt lock.
func (e *Engine) claimNextQueued() (runID, fromNodeID string, ok bool, noStartRunID string) {
	var head models.Run
	if err := e.db.Where("status = ?", "queued").
		Order("priority desc, created_at asc, id asc").First(&head).Error; err != nil {
		// ErrRecordNotFound simply means the queue is empty (expected, silent).
		// Any other error is a transient DB fault: the run stays "queued" and
		// the 5s poll retries, but surface it so a persistent fault (which would
		// stall all admission) is diagnosable instead of silently starving.
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Error().Err(err).Msg("dispatcher: claim next queued run failed (will retry)")
		}
		return "", "", false, ""
	}

	var candidates []models.Run
	if err := e.db.Where("status = ? AND priority = ?", "queued", head.Priority).
		Order("created_at asc, id asc").Find(&candidates).Error; err != nil {
		log.Error().Err(err).Msg("dispatcher: load same-priority queued runs failed (will retry)")
		return "", "", false, ""
	}
	if len(candidates) == 0 {
		return "", "", false, ""
	}

	run := pickQueuedByRemainingHumanGate(candidates)
	updates := map[string]any{"status": "running"}
	if run.StartedAt.IsZero() {
		updates["started_at"] = time.Now()
	}
	res := e.db.Model(&models.Run{}).Where("id = ? AND status = ?", run.ID, "queued").
		Updates(updates)
	if res.Error != nil {
		log.Error().Str("run_id", run.ID).Err(res.Error).Msg("dispatcher: claim update failed (will retry)")
		return "", "", false, ""
	}
	if res.RowsAffected == 0 {
		return "", "", false, "" // lost the row (cancelled / claimed elsewhere)
	}
	msg, _ := json.Marshal(map[string]any{"type": "status", "runId": run.ID, "status": "running"})
	e.broker.Publish(run.ID, msg)

	from, hasFrom := e.admissionFromNode(run)
	if !hasFrom {
		start := run.Graph.StartNode()
		if start == nil {
			return "", "", false, run.ID
		}
		fromNodeID = start.ID
	} else {
		e.clearAdmissionFrom(run.ID)
		fromNodeID = from
	}
	log.Info().Str("run_id", run.ID).Str("node_id", fromNodeID).Str("transition", "admit").Msg("run admitted from queue")
	return run.ID, fromNodeID, true, ""
}

// pickQueuedByRemainingHumanGate chooses among same-priority queued candidates
// (already ordered created_at ASC, id ASC): prefer the first with remaining
// human_gate reachability; otherwise the FIFO head. Does not mutate Priority.
func pickQueuedByRemainingHumanGate(candidates []models.Run) models.Run {
	var fallback models.Run
	haveFallback := false
	for i := range candidates {
		from, ok := continueFromNodeID(candidates[i])
		hasGate := ok && hasRemainingHumanGate(&candidates[i].Graph, from)
		if hasGate {
			return candidates[i]
		}
		if !haveFallback {
			fallback = candidates[i]
			haveFallback = true
		}
	}
	if haveFallback {
		return fallback
	}
	return candidates[0]
}

// scheduleRunAdmission is the unified entry for resuming or re-driving a run
// from a specific node. It TryAcquires a slot synchronously (admitRunDirect —
// empty-slot passthrough; does not wait for a future human_gate run). When at
// capacity the run is persisted as queued (with fromNodeID) and the dispatcher
// promotes it later by Priority → remaining human_gate → FIFO — never writing
// "running" before a slot is held. Already-running runs are never preempted.
// scheduleRunAdmission reports whether the run is queued or running before it
// returns. A free slot writes running; a full scheduler writes queued and
// keeps fromNodeID for the dispatcher. Callers that answer a resume must not
// return success while the run is still failed or cancelled.
func (e *Engine) scheduleRunAdmission(runID, fromNodeID string) bool {
	e.haltMu.RLock()
	if e.halted || !e.sem.TryAcquire() {
		e.haltMu.RUnlock()
		return e.queueForAdmission(runID, fromNodeID)
	}
	e.haltMu.RUnlock()
	if e.admitRunDirect(runID, fromNodeID) {
		return true
	}
	e.sem.Release()
	return e.queueForAdmission(runID, fromNodeID)
}

func (e *Engine) queueForAdmission(runID, fromNodeID string) bool {
	ok := e.enqueueAdmission(runID, fromNodeID, false)
	e.signalDispatch()
	return ok
}

// requeueRun rolls a run back from running to queued when execute bails before
// driving the FSM (e.g. acquireExecuteSlot timeout). Without this, runAdmitted's
// defer releases the sem slot while DB still shows running, drifting sem below
// the actual running count and allowing subsequent over-admission.
func (e *Engine) requeueRun(runID, fromNodeID string) {
	if !e.enqueueAdmission(runID, fromNodeID, true) {
		return
	}
	msg, _ := json.Marshal(map[string]any{"type": "status", "runId": runID, "status": "queued"})
	e.broker.Publish(runID, msg)
	log.Info().Str("run_id", runID).Str("node_id", fromNodeID).Msg("run requeued after execute early exit")
}

// enqueueAdmission flips a run to "queued" and records the node it should resume
// from as a SINGLE atomic update. Doing both in one write closes a race: the
// dispatcher (drainQueue → claimNextQueued) runs on its own goroutine and can be
// woken by any run's activity, so if the "queued" status were committed before
// the admission-from checkpoint, the dispatcher could claim the run in that gap,
// find no checkpoint, and fall back to the graph's start node — restarting the
// whole run from the top (e.g. a gate loop-back or resume re-driven from the
// input node) instead of continuing at fromNodeID.
//
// When requireRunning is set the flip is guarded on the run still being
// "running" (the requeue-after-early-exit path); the returned bool reports
// whether the run was actually queued so callers can skip follow-up work when
// they lost the race.
func (e *Engine) enqueueAdmission(runID, fromNodeID string, requireRunning bool) bool {
	queued := false
	err := e.db.Transaction(func(tx *gorm.DB) error {
		var run models.Run
		if err := tx.First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		if requireRunning && run.Status != "running" {
			return nil // lost the race; the run already moved off "running"
		}
		cp := run.Checkpoints
		if cp == nil {
			cp = map[string]map[string]any{}
		}
		cp[admissionFromCheckpoint] = map[string]any{"node": fromNodeID}
		q := tx.Model(&models.Run{}).Where("id = ?", runID)
		if requireRunning {
			q = q.Where("status = ?", "running")
		}
		res := q.Select("Status", "Checkpoints").Updates(&models.Run{Status: "queued", Checkpoints: cp})
		if res.Error != nil {
			return res.Error
		}
		queued = res.RowsAffected > 0
		return nil
	})
	if err != nil {
		log.Error().Str("run_id", runID).Str("node_id", fromNodeID).Err(err).Msg("enqueue admission failed")
		return false
	}
	return queued
}

func (e *Engine) admitRunDirect(runID, fromNodeID string) bool {
	var run models.Run
	if err := e.db.First(&run, "id = ?", runID).Error; err != nil {
		return false
	}
	updates := map[string]any{"status": "running"}
	if run.StartedAt.IsZero() {
		updates["started_at"] = time.Now()
	}
	res := e.db.Model(&models.Run{}).Where("id = ? AND status <> ?", runID, "running").Updates(updates)
	if res.Error != nil || res.RowsAffected == 0 {
		return false
	}
	e.clearAdmissionFrom(runID)
	msg, _ := json.Marshal(map[string]any{"type": "status", "runId": runID, "status": "running"})
	e.broker.Publish(runID, msg)
	log.Info().Str("run_id", runID).Str("node_id", fromNodeID).Str("transition", "admit").Msg("run admitted")
	go e.runAdmitted(runID, fromNodeID)
	return true
}

func (e *Engine) admissionFromNode(run models.Run) (string, bool) {
	if run.Checkpoints == nil {
		return "", false
	}
	cp, ok := run.Checkpoints[admissionFromCheckpoint]
	if !ok {
		return "", false
	}
	node, _ := cp["node"].(string)
	if node == "" {
		return "", false
	}
	return node, true
}

func (e *Engine) clearAdmissionFrom(runID string) {
	var run models.Run
	if err := e.db.First(&run, "id = ?", runID).Error; err != nil {
		return
	}
	if run.Checkpoints == nil {
		return
	}
	if _, ok := run.Checkpoints[admissionFromCheckpoint]; !ok {
		return
	}
	delete(run.Checkpoints, admissionFromCheckpoint)
	logDB(e.db.Model(&models.Run{}).Where("id = ?", runID).
		Select("Checkpoints").Updates(&models.Run{Checkpoints: run.Checkpoints}), runID, "clear admission from")
}
