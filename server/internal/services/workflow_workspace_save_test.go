package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkspaceSavesReportSavingAndFailedCheckpoints(t *testing.T) {
	db := newTestDB(t)
	started := time.Now().UTC()
	rows := []models.WorkflowVisitCheckpoint{
		{ID: "cp-saving", RunID: "run-saving", NodeID: "a", FileState: "pending", RouteState: "sealed"},
		{ID: "cp-failed", RunID: "run-failed", NodeID: "b", FileState: "blocked", RouteState: "sealed", Reason: "snapshot_failed", SnapshotID: "snap-1", CaptureStartedAt: &started},
		{ID: "cp-unknown", RunID: "run-unknown", NodeID: "c", FileState: "unknown", RouteState: "sealed", Reason: "snapshot_unknown", SnapshotID: "snap-2", CaptureStartedAt: &started},
		{ID: "cp-binding", RunID: "run-binding", NodeID: "d", FileState: "blocked", RouteState: "sealed", Reason: "binding_unavailable"},
		{ID: "cp-provider", RunID: "run-provider", NodeID: "e", FileState: "unknown", RouteState: "unknown", Reason: "provider_outcome_unconfirmed"},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.WorkspaceSnapshotJob{ID: "job-1", State: "failed", Error: "git_unsupported", Deadline: started}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkspaceSnapshot{ID: "snap-1", JobID: "job-1"}).Error; err != nil {
		t.Fatal(err)
	}
	runs := []models.Run{{ID: "run-plain", Status: "paused", PauseFromStatus: "running"}, {ID: "run-done", Status: "completed", PauseFromStatus: "workspace_checkpoint:cp-saving"}}
	for _, row := range rows {
		runs = append(runs, models.Run{ID: row.RunID, Status: "paused", PauseFromStatus: "workspace_checkpoint:" + row.ID})
	}
	got := WorkspaceSaves(db, runs)
	want := map[string]WorkspaceSave{
		"run-saving":   {CheckpointID: "cp-saving", NodeID: "a", State: "saving", OutcomeSealed: true},
		"run-failed":   {CheckpointID: "cp-failed", NodeID: "b", State: "failed", Reason: "snapshot_failed", Detail: "git_unsupported", Action: "retry", OutcomeSealed: true},
		"run-unknown":  {CheckpointID: "cp-unknown", NodeID: "c", State: "failed", Reason: "snapshot_unknown", Action: "reconcile", OutcomeSealed: true},
		"run-binding":  {CheckpointID: "cp-binding", NodeID: "d", State: "failed", Reason: "binding_unavailable", OutcomeSealed: true},
		"run-provider": {CheckpointID: "cp-provider", NodeID: "e", State: "failed", Reason: "provider_outcome_unconfirmed", OutcomeSealed: false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d saves: %+v", len(got), got)
	}
	for id, save := range want {
		if got[id] == nil || *got[id] != save {
			t.Fatalf("%s: got %+v want %+v", id, got[id], save)
		}
	}
	if len(WorkspaceSaves(nil, runs)) != 0 || len(WorkspaceSaves(db, runs[:2])) != 0 {
		t.Fatal("runs not paused on a checkpoint have no save state")
	}
}
