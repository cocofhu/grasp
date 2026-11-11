package engine

import (
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/rs/zerolog/log"
)

// reattachInfo identifies an agent node that was mid-flight when the previous
// process exited and whose sandbox is still alive.
type reattachInfo struct {
	runID     string
	nodeID    string
	iteration int
	sandbox   string
}

// reconcileInterrupted settles work whose driving goroutine died with the
// previous process:
//
//   - an auto Agent node with a live run sandbox is adopted: its slot is
//     reserved here and ResumeInterrupted later reattaches the sandbox and
//     continues the same visit;
//   - any other node caught mid-flight is instantaneous (input / output /
//     set_var / branch / gate entry) or has no sandbox left, so its visit is
//     closed and the run is requeued to re-enter that node;
//   - a run whose latest transition source has finished, and whose target
//     visit has not started, is queued at that target. The trace wins when an
//     older resume checkpoint names an earlier node;
//   - a confirm that only marked the dialogue done, while the visit is still
//     waiting and no target was written, reopens the dialogue into the inbox;
//   - a running run with no in-flight visit and no such transition fails.
//     A run that never started a visit still fails.
//
// Queued runs, unfinished human dialogues, and terminal runs are left as they
// are. Admission still goes through the existing queue.
func (e *Engine) reconcileInterrupted() {
	var running []models.StateRun
	if err := e.db.Where("status = ?", "running").Order("id asc").Find(&running).Error; err != nil {
		log.Error().Err(err).Msg("startup reconciliation: query running node states failed")
	}
	handled := map[string]bool{}
	orphans := map[string]bool{}
	for _, sr := range running {
		var run models.Run
		if e.db.First(&run, "id = ?", sr.RunID).Error != nil || run.Status != "running" || handled[sr.RunID] {
			e.failStateRun(sr.ID, sr.RunID)
			orphans[sr.RunID] = true
			continue
		}
		if sr.IsManual() {
			// Taken over just before the restart: never adopt or re-run in
			// auto; park it as a manual session (cold sandbox).
			e.reconcileManualVisit(&run, sr)
			handled[sr.RunID] = true
			continue
		}
		if name, ok := e.adoptableSandbox(&run, sr); ok {
			e.reattachMu.Lock()
			e.pendingReattach = append(e.pendingReattach, reattachInfo{
				runID: sr.RunID, nodeID: sr.NodeID, iteration: sr.Iteration, sandbox: name,
			})
			e.adoptedSandboxes[name] = true
			e.reattachMu.Unlock()
			e.sem.ForceAcquire()
			handled[sr.RunID] = true
			log.Info().Str("run_id", sr.RunID).Str("node_id", sr.NodeID).Str("sandbox", name).
				Msg("startup reconciliation: agent sandbox adopted")
			continue
		}
		logDB(e.db.Model(&models.StateRun{}).Where("id = ?", sr.ID).Updates(map[string]any{
			"status": "cancelled",
			"error":  "控制面重启，节点重新进入",
		}), sr.RunID, "reconcile state_run")
		if e.enqueueAdmission(sr.RunID, sr.NodeID, true) {
			handled[sr.RunID] = true
			log.Info().Str("run_id", sr.RunID).Str("node_id", sr.NodeID).
				Msg("startup reconciliation: run requeued at interrupted node")
			continue
		}
		orphans[sr.RunID] = true
	}

	var runs []models.Run
	if err := e.db.Where("status = ?", "running").Find(&runs).Error; err != nil {
		log.Error().Err(err).Msg("startup reconciliation: query running runs failed")
	}
	for i := range runs {
		r := &runs[i]
		if handled[r.ID] {
			continue
		}
		if e.settlePendingHandoff(r) {
			handled[r.ID] = true
			delete(orphans, r.ID)
			continue
		}
		orphans[r.ID] = true
	}

	var paused []models.Run
	if err := e.db.Where("status = ?", "waiting_human").Find(&paused).Error; err != nil {
		log.Error().Err(err).Msg("startup reconciliation: query waiting runs failed")
	}
	for i := range paused {
		r := &paused[i]
		if handled[r.ID] {
			continue
		}
		if e.settlePendingHandoff(r) {
			handled[r.ID] = true
			delete(orphans, r.ID)
		}
	}

	for id := range orphans {
		if handled[id] {
			continue
		}
		var r models.Run
		if e.db.First(&r, "id = ?", id).Error != nil {
			continue
		}
		if r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled" || r.Status == "queued" {
			continue
		}
		logDB(e.db.Model(&models.Run{}).Where("id = ?", id).UpdateColumn("status", "failed"), id, "reconcile run")
		e.host.UnregisterRun(id)
		log.Warn().Str("run_id", id).Msg("reconciled interrupted run on startup -> failed")
	}
}

// settlePendingHandoff queues a run at the latest transition whose target
// visit has not started, or reopens a confirm that left the inbox before a
// target was written. It reports whether the run was settled.
func (e *Engine) settlePendingHandoff(run *models.Run) bool {
	if target, ok := e.pendingTransitionTarget(run); ok {
		if run.Graph.FindNode(target) == nil {
			e.failRun(run.ID, "流转目标不在图上: "+target)
			log.Warn().Str("run_id", run.ID).Str("node_id", target).
				Msg("startup reconciliation: transition target missing, run failed")
			return true
		}
		if !e.enqueueAdmission(run.ID, target, run.Status == "running") {
			return false
		}
		e.signalDispatch()
		log.Info().Str("run_id", run.ID).Str("node_id", target).
			Msg("startup reconciliation: queued at recorded transition")
		return true
	}
	if e.reopenAbandonedConfirm(run) {
		log.Info().Str("run_id", run.ID).Msg("startup reconciliation: confirm reopened into inbox")
		return true
	}
	return false
}

// pendingTransitionTarget returns the latest transition whose source visit has
// ended and whose target visit has not started. An older resume checkpoint is
// ignored: the trace is the position a restart continues from.
func (e *Engine) pendingTransitionTarget(run *models.Run) (string, bool) {
	if run == nil || len(run.Trace) == 0 {
		return "", false
	}
	idx := -1
	for i := len(run.Trace) - 1; i >= 0; i-- {
		if run.Trace[i].Event == "transition" && run.Trace[i].To != "" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	te := run.Trace[idx]
	var inflight int64
	if err := e.db.Model(&models.StateRun{}).
		Where("run_id = ? AND status = ?", run.ID, "running").Count(&inflight).Error; err != nil || inflight > 0 {
		return "", false
	}
	var src models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", run.ID, te.NodeID).
		Order("iteration desc, id desc").First(&src).Error; err != nil {
		return "", false
	}
	switch src.Status {
	case "completed", "failed", "cancelled", "skipped":
	default:
		return "", false
	}
	for _, later := range run.Trace[idx+1:] {
		if later.NodeID == te.To && (later.Event == "enter" || later.Event == "reattach") {
			return "", false
		}
	}
	var tgt models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", run.ID, te.To).
		Order("iteration desc, id desc").First(&tgt).Error; err == nil {
		if tgt.Status == "running" || tgt.Status == "waiting_human" {
			return "", false
		}
	}
	return te.To, true
}

// reopenAbandonedConfirm puts a half-finished confirm back in the inbox.
// The dialogue was marked done so it would leave the inbox, but the visit is
// still waiting and no next node was written. The model wrap-up is not replayed.
func (e *Engine) reopenAbandonedConfirm(run *models.Run) bool {
	if run == nil || (run.Status != "waiting_human" && run.Status != "running") {
		return false
	}
	var convs []models.ReactConversation
	if err := e.db.Where("run_id = ? AND done = ?", run.ID, true).
		Order("iteration desc, id desc").Find(&convs).Error; err != nil || len(convs) == 0 {
		return false
	}
	reopened := false
	for _, conv := range convs {
		var sr models.StateRun
		if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, conv.NodeID, conv.Iteration).
			Order("id desc").First(&sr).Error; err != nil || sr.Status != "waiting_human" {
			continue
		}
		res := e.db.Model(&models.ReactConversation{}).Where("id = ? AND done = ?", conv.ID, true).Update("done", false)
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		reopened = true
	}
	if !reopened {
		return false
	}
	if run.Status == "running" {
		logDB(e.db.Model(&models.Run{}).Where("id = ? AND status = ?", run.ID, "running").
			Update("status", "waiting_human"), run.ID, "reopen confirm")
	}
	return true
}

func (e *Engine) failStateRun(id uint, runID string) {
	logDB(e.db.Model(&models.StateRun{}).Where("id = ?", id).Updates(map[string]any{
		"status": "failed",
		"error":  "服务重启中断,执行协程丢失,节点未收尾",
	}), runID, "reconcile state_run")
}

// adoptableSandbox returns the live run sandbox of an interrupted auto Agent
// visit. Clarify Agents pause into a human dialogue and are re-entered instead.
func (e *Engine) adoptableSandbox(run *models.Run, sr models.StateRun) (string, bool) {
	node := run.Graph.FindNode(sr.NodeID)
	if node == nil || node.Caps == nil || node.Caps.Clarify() {
		return "", false
	}
	if spec, ok := nodereg.Get(node.Type); !ok || spec.Exec != nodereg.ExecAgent {
		return "", false
	}
	var sb models.Sandbox
	if e.db.Where("run_id = ? AND node_id = ? AND purpose = ? AND status = ?", sr.RunID, sr.NodeID, "run", "running").
		Order("updated_at desc").First(&sb).Error != nil || sb.Name == "" {
		return "", false
	}
	return sb.Name, true
}

// AdoptedSandboxes returns the run sandboxes reserved for reattachment, so
// startup sandbox reconciliation keeps them alive without a TTL.
func (e *Engine) AdoptedSandboxes() map[string]bool {
	e.reattachMu.Lock()
	defer e.reattachMu.Unlock()
	out := make(map[string]bool, len(e.adoptedSandboxes))
	for k, v := range e.adoptedSandboxes {
		out[k] = v
	}
	return out
}

// ResumeInterrupted starts a driver for every adopted agent node. Call it once
// after the sandbox registry and other provider hooks are wired: the driver
// retires or discards sandboxes through them.
func (e *Engine) ResumeInterrupted() {
	e.reattachMu.Lock()
	pending := e.pendingReattach
	e.pendingReattach = nil
	e.reattachMu.Unlock()
	for _, r := range pending {
		go e.runReattached(r)
	}
}

// runReattached drives an adopted run from its interrupted agent node. The
// slot was reserved by reconcileInterrupted.
func (e *Engine) runReattached(r reattachInfo) {
	defer func() {
		e.sem.Release()
		e.signalDispatch()
	}()
	e.executeFrom(r.runID, r.nodeID, &r)
}
