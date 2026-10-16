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
//   - a running run with no in-flight node has no known position and fails.
//
// Queued runs and runs paused at a human gate / react dialogue are untouched.
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
	for _, r := range runs {
		if !handled[r.ID] {
			orphans[r.ID] = true
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
		if r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled" {
			continue
		}
		logDB(e.db.Model(&models.Run{}).Where("id = ?", id).UpdateColumn("status", "failed"), id, "reconcile run")
		e.host.UnregisterRun(id)
		log.Warn().Str("run_id", id).Msg("reconciled interrupted run on startup -> failed")
	}
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
