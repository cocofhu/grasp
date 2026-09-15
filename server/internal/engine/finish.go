package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

func (c *execCtx) setVar(name string, v any) { c.vars[name] = v }

func (e *Engine) persistVar(runID, name string, v any) {
	v = blob.StripDataInValue(v)
	var rv models.RunVariable
	if err := e.db.Where("run_id = ? AND name = ?", runID, name).First(&rv).Error; err == nil {
		rv.Value = v
		logDB(e.db.Save(&rv), runID, "persist variable")
	} else {
		logDB(e.db.Create(&models.RunVariable{RunID: runID, Name: name, Type: inferType(v), Value: v}), runID, "create variable")
	}
}

func (e *Engine) appendTrace(c *execCtx, te models.TraceEntry) {
	te.At = time.Now().Format(time.RFC3339)
	c.run.Trace = append(c.run.Trace, te)

	logDB(e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).
		Select("Trace").Updates(&models.Run{Trace: c.run.Trace}), c.run.ID, "append trace")
	msg, _ := json.Marshal(map[string]any{"type": "trace", "runId": c.run.ID, "entry": te})
	e.broker.Publish(c.run.ID, msg)
}

func (e *Engine) refreshProgress(c *execCtx) {
	var total, done int64
	total = int64(len(c.graph.Nodes))

	e.db.Model(&models.StateRun{}).Where("run_id = ? AND status IN ?", c.run.ID, []string{"completed", "skipped"}).
		Distinct("node_id").Count(&done)
	if total > 0 {
		c.run.Progress = float64(done) / float64(total)
		logDB(e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).Update("progress", c.run.Progress), c.run.ID, "refresh progress")
	}
}

// finalizeActiveStateRuns marks every still-active StateRun for a run with the
// same terminal status as the run itself. "Active" covers both running rows
// (panic/loadCtx/node-not-found paths that finish without updating the in-flight
// StateRun) and waiting_human rows (a node paused at a human gate / react turn):
// cancelling while paused must move that node off waiting_human, otherwise the
// paused visit lingers as "awaiting input" and the auto-resume picker can't tell
// it was terminated. Normal failure paths already call saveState first, so this
// is a no-op there.
func (e *Engine) finalizeActiveStateRuns(runID, status string) {
	if status != "failed" && status != "cancelled" {
		return
	}
	errMsg := "服务重启中断,执行协程丢失,节点未收尾"
	if status == "cancelled" {
		errMsg = "run 已取消,节点未收尾"
	}
	logDB(e.db.Model(&models.StateRun{}).
		Where("run_id = ? AND status IN ?", runID, []string{"running", "waiting_human"}).
		Updates(map[string]any{"status": status, "error": errMsg}), runID, "finalize active state_runs")
}

// supersedePendingGates resolves any still-open gate for a terminated run. When a
// run is cancelled (or fails) while paused at a human gate, the pending Gate row
// is otherwise left untouched; resuming from that point re-enters the gate at a
// new iteration and opens a *second* gate — leaving two approvals (and a phantom
// pending gate surfaced on the completed run). Marking the old gate resolved
// supersedes it so only the fresh (post-resume) gate remains actionable.
func (e *Engine) supersedePendingGates(runID, status string) {
	if status != "failed" && status != "cancelled" {
		return
	}
	logDB(e.db.Model(&models.Gate{}).
		Where("run_id = ? AND resolved = ?", runID, false).
		Update("resolved", true), runID, "supersede pending gates")
}

// runStatus returns the persisted run status, or "" when the row is missing.
func (e *Engine) runStatus(runID string) string {
	var run models.Run
	if err := e.db.Select("status").First(&run, "id = ?", runID).Error; err != nil {
		return ""
	}
	return run.Status
}

// pauseStillPending reports whether the node's current pause is still awaiting
// human input. ResumeGate / ReactReply mark the gate resolved or conversation
// done before scheduling the next node; if that landed during this driver's
// pause unwind, the waiting_human transition must not apply.
func (e *Engine) pauseStillPending(runID string, node *models.Node) bool {
	switch node.Type {
	case "human_gate", "proposal_select":
		var gate models.Gate
		if err := e.db.Where("run_id = ? AND node_id = ?", runID, node.ID).
			Order("iteration desc, id desc").First(&gate).Error; err != nil {
			return true
		}
		return !gate.Resolved
	case "react", "approve", "preflight":
		var conv models.ReactConversation
		if err := e.db.Where("run_id = ? AND node_id = ?", runID, node.ID).
			Order("iteration desc, id desc").First(&conv).Error; err != nil {
			return true
		}
		return !conv.Done
	default:

		if isReviewNode(node.Type) {
			var conv models.ReactConversation
			if err := e.db.Where("run_id = ? AND node_id = ?", runID, node.ID).
				Order("iteration desc, id desc").First(&conv).Error; err != nil {
				return true
			}
			return !conv.Done
		}
		return true
	}
}

// failRun records a human-readable reason (vars.last_error) then finishes the
// run as failed. Used by early-exit paths (loadCtx / node-not-found / panic /
// empty graph) that never wrote StateRun.error, so AggregateRunFailure still
// has a non-empty source before the default fallback.
func (e *Engine) failRun(runID, reason string) {
	if strings.TrimSpace(reason) != "" {
		e.persistVar(runID, "last_error", reason)
	}
	e.finish(runID, "failed")
}

func (e *Engine) finish(runID, status string) bool {
	updates := map[string]any{"status": status}
	if status == "completed" {
		updates["progress"] = 1.0
	}
	// Stamp the total elapsed time from the run's start so the run list / detail
	// show a real 耗时 instead of 00:00 (Run.DurationSec was never computed).
	var run models.Run
	if e.db.First(&run, "id = ?", runID).Error == nil && !run.StartedAt.IsZero() {
		if d := int(time.Since(run.StartedAt).Seconds()); d >= 0 {
			updates["duration_sec"] = d
		}
	}

	q := e.db.Model(&models.Run{}).Where("id = ?", runID)
	if status == "waiting_human" {
		// If this run already had gate/react pause rows and every one is
		// resolved/done, a concurrent ResumeGate/ReactReply continued the
		// run — skip a late pause unwind so we do not clobber re-admitted
		// "running" (or queued) back to waiting_human.
		var totalGates, totalReact int64
		_ = e.db.Model(&models.Gate{}).Where("run_id = ?", runID).Count(&totalGates).Error
		_ = e.db.Model(&models.ReactConversation{}).Where("run_id = ?", runID).Count(&totalReact).Error
		if totalGates > 0 || totalReact > 0 {
			var pendingGates, pendingReact int64
			_ = e.db.Model(&models.Gate{}).
				Where("run_id = ? AND resolved = ?", runID, false).Count(&pendingGates).Error
			_ = e.db.Model(&models.ReactConversation{}).
				Where("run_id = ? AND done = ?", runID, false).Count(&pendingReact).Error
			if pendingGates == 0 && pendingReact == 0 {
				return false
			}
		}
		q = q.Where("status = ?", "running")
	}
	res := q.Updates(updates)
	logDB(res, runID, "finish run")
	if status == "waiting_human" && (res.Error != nil || res.RowsAffected == 0) {
		return false
	}
	if res.Error != nil {
		return false
	}
	if status == "completed" {

		e.fireCompletedRunNotify(runID)
	}
	if status == "failed" || status == "cancelled" {
		e.finalizeActiveStateRuns(runID, status)
		e.supersedePendingGates(runID, status)
	}
	// Persist run_error.json before AbortRun so (1) DB pollers that see
	// status=failed already have the product, and (2) live sandbox logs can
	// still be archived. AbortRun tears containers down.
	if status == "failed" {
		e.persistRunErrorArtifact(runID)
	}
	if status == "completed" || status == "failed" || status == "cancelled" {
		if e.shareRevoker != nil {
			e.shareRevoker.RevokeUnusedForRun(runID)
		}

		if ab, ok := e.provider.(runtime.RunAborter); ok {
			ab.AbortRun(runID)
		}
		e.host.UnregisterRun(runID)
		e.mu.Lock()
		delete(e.tokens, runID)
		e.mu.Unlock()

		action := models.AuditActionRunCompleted
		switch status {
		case "failed":
			action = models.AuditActionRunFailed
		case "cancelled":
			action = models.AuditActionRunCancelled
		}
		projectID := services.ResolveProjectIDForRun(e.db, runID)
		if projectID != "" {
			e.recordAudit(services.AuditRecord{
				ProjectID:    projectID,
				Actor:        services.SystemActor(),
				CallerKind:   models.CallerKindSystem,
				Action:       action,
				ResourceType: "run",
				ResourceID:   runID,
				RunID:        runID,
				Outcome:      models.AuditOutcomeOK,
				Summary:      "run " + status,
				Payload:      map[string]any{"status": status, "trigger": "engine", "runId": runID},
			})
		}
	}
	msg, _ := json.Marshal(map[string]any{"type": "status", "runId": runID, "status": status})
	e.broker.Publish(runID, msg)
	return true
}

// persistRunErrorArtifact writes run_error.json via the artifact store so empty
// product failures remain queryable. Best-effort: store failures are logged and
// do not change finish control flow. Before aggregating, forces sandbox log
// archive/pull so CAPA mis-fires after retire still get logs or a degrade note.
func (e *Engine) persistRunErrorArtifact(runID string) {
	if e.store == nil {
		return
	}
	degrade := ""
	if a, ok := e.provider.(runtime.RunSandboxLogArchiver); ok {
		if n, note := a.ArchiveRunSandboxLogs(context.Background(), runID); n == 0 && note != "" {
			degrade = note
		}
	}
	info := services.NewRunService(e.db).AggregateRunFailure(runID, degrade)
	body, err := services.MarshalRunErrorJSON(info)
	if err != nil {
		log.Warn().Str("run_id", runID).Err(err).Msg("marshal run_error.json failed")
		return
	}
	nodeID := info.FailedNode
	if _, err := e.store.Save(runID, nodeID, services.RunErrorArtifactName, "json", body); err != nil {
		log.Warn().Str("run_id", runID).Err(err).Msg("save run_error.json failed")
	}
}

// logDB records a GORM write error on an engine best-effort persistence path.
// The FSM keeps driving the run from its in-memory context, but a silent DB
// write failure previously desynced the persisted run / UI from reality with no
// trace at all; this surfaces it in the logs so it can be diagnosed. Control
// flow is intentionally unchanged — these writes are recovery-friendly (a
// resume reloads from the DB) and aborting mid-transition would be worse.
func logDB(res *gorm.DB, runID, op string) {
	if res != nil && res.Error != nil {
		log.Error().Str("run_id", runID).Str("op", op).Err(res.Error).Msg("engine db write failed")
	}
}

func inferType(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case int, int64, float64:
		return "int"
	default:
		return "string"
	}
}
