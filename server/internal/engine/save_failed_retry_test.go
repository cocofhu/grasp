package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func pauseOnCheckpoint(t *testing.T, e *Engine, runID, checkpointID string) {
	t.Helper()
	if err := e.db.Model(&models.Run{}).Where("id = ?", runID).Updates(map[string]any{
		"status": "paused", "pause_from_status": "workspace_checkpoint:" + checkpointID, "pause_requested": false,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRetrySaveDoesNotOpenNodeExecution(t *testing.T) {
	e, c, _, state := workspaceCheckpointFixture(t, true)
	defer e.Close()
	now := time.Now().UTC()
	const checkpointID = "cp-retry-save"
	if err := e.db.Create(&models.WorkflowVisitCheckpoint{
		ID: checkpointID, RunID: c.run.ID, StateRunID: state.ID, NodeID: "agent",
		FileState: "blocked", RouteState: "sealed", Reason: "snapshot_failed",
		SnapshotID: "snap-retry", CaptureStartedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	pauseOnCheckpoint(t, e, c.run.ID, checkpointID)
	if err := e.db.Model(&models.StateRun{}).Where("id = ?", state.ID).Updates(map[string]any{"status": "completed", "output_md": "kept"}).Error; err != nil {
		t.Fatal(err)
	}
	var before int64
	e.db.Model(&models.StateRun{}).Where("run_id = ?", c.run.ID).Count(&before)

	if err := e.ResumePausedRun(c.run.ID, 0); err != nil {
		t.Fatal(err)
	}
	var after int64
	e.db.Model(&models.StateRun{}).Where("run_id = ?", c.run.ID).Count(&after)
	if after != before {
		t.Fatalf("retry save opened a node execution: before %d after %d", before, after)
	}
	var run models.Run
	if err := e.db.First(&run, "id = ?", c.run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "paused" {
		t.Fatalf("status = %s, want paused", run.Status)
	}
	var row models.WorkflowVisitCheckpoint
	if err := e.db.First(&row, "id = ?", checkpointID).Error; err != nil {
		t.Fatal(err)
	}
	if row.FileState != "pending" || row.RouteState == "applied" {
		t.Fatalf("capture was not rearmed: file %s route %s", row.FileState, row.RouteState)
	}
}

func TestSaveFailedResumeRerunsBannerNodeAndKeepsHistory(t *testing.T) {
	e, c, _, state := workspaceCheckpointFixture(t, true)
	defer e.Close()
	const checkpointID = "cp-fail-retry"
	if err := e.db.Create(&models.WorkflowVisitCheckpoint{
		ID: checkpointID, RunID: c.run.ID, StateRunID: state.ID, NodeID: "agent",
		FileState: "blocked", RouteState: "sealed", Reason: "snapshot_failed",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Create(&models.StateRun{
		RunID: c.run.ID, NodeID: "start", NodeType: "input", Iteration: 1, Status: "completed", OutputMd: "upstream-kept",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Model(&models.StateRun{}).Where("id = ?", state.ID).Updates(map[string]any{"status": "completed", "output_md": "node-result"}).Error; err != nil {
		t.Fatal(err)
	}
	pauseOnCheckpoint(t, e, c.run.ID, checkpointID)

	if err := e.ResumeFrom(c.run.ID, ""); err != nil {
		t.Fatal(err)
	}
	var run models.Run
	if err := e.db.First(&run, "id = ?", c.run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" && run.Status != "running" {
		t.Fatalf("status after failure retry = %s, want queued or running", run.Status)
	}
	var checkpoint models.WorkflowVisitCheckpoint
	if err := e.db.First(&checkpoint, "id = ?", checkpointID).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.RouteState != "cancelled" || checkpoint.RouteState == "applied" {
		t.Fatalf("save route = %s, want cancelled and not applied", checkpoint.RouteState)
	}
	deadline := time.Now().Add(5 * time.Second)
	var agentRows int64
	for time.Now().Before(deadline) {
		e.db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", c.run.ID, "agent").Count(&agentRows)
		if agentRows >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if agentRows < 2 {
		t.Fatalf("agent executions = %d, want a new visit plus the old one", agentRows)
	}
	var previous models.StateRun
	if err := e.db.First(&previous, state.ID).Error; err != nil {
		t.Fatal(err)
	}
	if previous.Status != "completed" || previous.OutputMd != "node-result" {
		t.Fatalf("old execution changed: %+v", previous)
	}
	var upstream models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, "start").First(&upstream).Error; err != nil {
		t.Fatal(err)
	}
	if upstream.Status != "completed" || upstream.OutputMd != "upstream-kept" {
		t.Fatalf("upstream result changed: %+v", upstream)
	}
}

func TestSaveFailedResumeRejectsSavingAndOrdinaryPause(t *testing.T) {
	e, c, _, state := workspaceCheckpointFixture(t, true)
	defer e.Close()
	const savingID = "cp-still-saving"
	if err := e.db.Create(&models.WorkflowVisitCheckpoint{
		ID: savingID, RunID: c.run.ID, StateRunID: state.ID, NodeID: "agent",
		FileState: "pending", RouteState: "sealed",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Model(&models.StateRun{}).Where("id = ?", state.ID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	pauseOnCheckpoint(t, e, c.run.ID, savingID)
	if err := e.ResumeFrom(c.run.ID, ""); err == nil || !strings.Contains(err.Error(), "不可续跑") {
		t.Fatalf("saving resume err = %v", err)
	}
	var run models.Run
	if err := e.db.First(&run, "id = ?", c.run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "paused" {
		t.Fatalf("saving run status = %s", run.Status)
	}
	var row models.WorkflowVisitCheckpoint
	if err := e.db.First(&row, "id = ?", savingID).Error; err != nil {
		t.Fatal(err)
	}
	if row.RouteState != "sealed" {
		t.Fatalf("saving checkpoint route = %s", row.RouteState)
	}

	if err := e.db.Model(&models.WorkflowVisitCheckpoint{}).Where("id = ?", savingID).Update("route_state", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).Update("pause_from_status", "queued").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.ResumeFrom(c.run.ID, ""); err == nil || !strings.Contains(err.Error(), "不可续跑") {
		t.Fatalf("ordinary pause resume err = %v", err)
	}
	if err := e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).Update("status", "running").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.ResumeFrom(c.run.ID, ""); err == nil {
		t.Fatal("running run accepted failure retry")
	}
	if err := e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.ResumeFrom(c.run.ID, ""); err == nil {
		t.Fatal("completed run accepted failure retry")
	}
	var visits int64
	e.db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", c.run.ID, "agent").Count(&visits)
	if visits != 1 {
		t.Fatalf("rejected resumes opened visits: %d", visits)
	}
}
