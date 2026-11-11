package services

import (
	"context"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

type recordingReleaser struct {
	runs []string
}

func (r *recordingReleaser) ReleaseHeldRunSandboxes(_ context.Context, runID string) error {
	r.runs = append(r.runs, runID)
	return nil
}

func saveFailedRun(id, checkpointID string, failed bool) (models.Run, models.WorkflowVisitCheckpoint) {
	file, reason := "pending", ""
	if failed {
		file, reason = "blocked", "snapshot_failed"
	}
	run := models.Run{ID: id, WorkflowID: "wf-keep", Status: "paused", PauseFromStatus: "workspace_checkpoint:" + checkpointID}
	row := models.WorkflowVisitCheckpoint{ID: checkpointID, RunID: id, NodeID: "agent", FileState: file, RouteState: "sealed", Reason: reason}
	return run, row
}

func TestDeleteSaveFailedRunMatrix(t *testing.T) {
	db := newTestDB(t)
	wf := NewWorkflowService(db)
	if err := wf.Save(&models.WorkflowDef{ID: "wf-keep", Name: "KeepMe", Graph: validGraph()}); err != nil {
		t.Fatal(err)
	}
	s := NewRunService(db)
	rel := &recordingReleaser{}
	s.SetSandboxReleaser(rel)

	for _, status := range []string{"queued", "running", "waiting_human"} {
		id := "active-" + status
		if err := db.Create(&models.Run{ID: id, WorkflowID: "wf-keep", Status: status}).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(id); !errors.Is(err, ErrRunNotDeletable) {
			t.Fatalf("status %s: %v", status, err)
		}
		if _, ok := s.Get(id); !ok {
			t.Fatalf("status %s was deleted", status)
		}
	}

	savingRun, savingCP := saveFailedRun("saving", "cp-saving", false)
	if err := db.Create(&savingRun).Error; err != nil || db.Create(&savingCP).Error != nil {
		t.Fatal("seed saving")
	}
	if err := s.Delete(savingRun.ID); !errors.Is(err, ErrRunNotDeletable) {
		t.Fatalf("saving: %v", err)
	}
	if _, ok := s.Get(savingRun.ID); !ok {
		t.Fatal("saving run deleted")
	}

	plain := models.Run{ID: "plain-pause", WorkflowID: "wf-keep", Status: "paused", PauseFromStatus: "queued"}
	if err := db.Create(&plain).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(plain.ID); !errors.Is(err, ErrRunNotDeletable) {
		t.Fatalf("ordinary pause: %v", err)
	}

	failedRun, failedCP := saveFailedRun("save-failed", "cp-failed", true)
	if err := db.Create(&failedRun).Error; err != nil || db.Create(&failedCP).Error != nil {
		t.Fatal("seed save-failed")
	}
	if err := db.Create(&models.Artifact{ID: "art-save-failed", RunID: failedRun.ID, Name: "doc"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Sandbox{Name: "sb-save-failed", Purpose: "run", RunID: failedRun.ID, Status: "running"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(failedRun.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(failedRun.ID); ok {
		t.Fatal("save-failed run still present")
	}
	if len(rel.runs) != 1 || rel.runs[0] != failedRun.ID {
		t.Fatalf("sandbox release = %#v", rel.runs)
	}
	var n int64
	db.Model(&models.Artifact{}).Where("run_id = ?", failedRun.ID).Count(&n)
	if n != 0 {
		t.Fatalf("artifacts left: %d", n)
	}
	db.Model(&models.WorkflowVisitCheckpoint{}).Where("run_id = ?", failedRun.ID).Count(&n)
	if n != 0 {
		t.Fatalf("checkpoints left: %d", n)
	}
	db.Model(&models.Sandbox{}).Where("run_id = ?", failedRun.ID).Count(&n)
	if n != 0 {
		t.Fatalf("sandbox rows left: %d", n)
	}
	if _, ok := wf.Get("wf-keep"); !ok {
		t.Fatal("workflow definition was deleted")
	}

	noRelease := NewRunService(db)
	bareRun, bareCP := saveFailedRun("save-failed-bare", "cp-bare", true)
	if err := db.Create(&bareRun).Error; err != nil || db.Create(&bareCP).Error != nil {
		t.Fatal("seed bare")
	}
	if err := noRelease.Delete(bareRun.ID); err == nil {
		t.Fatal("save-failed delete without a releaser succeeded")
	}
	if _, ok := noRelease.Get(bareRun.ID); !ok {
		t.Fatal("run deleted without releasing its sandbox")
	}
}

func TestDeleteSaveFailedRejectsStatusChangeInsideTransaction(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	rel := &recordingReleaser{}
	s.SetSandboxReleaser(rel)
	run, cp := saveFailedRun("race", "cp-race", true)
	if err := db.Create(&run).Error; err != nil || db.Create(&cp).Error != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Artifact{ID: "art-race", RunID: run.ID, Name: "doc"}).Error; err != nil {
		t.Fatal(err)
	}
	s.beforeDeleteRecheck = func(tx *gorm.DB, id string) {
		if err := tx.Model(&models.Run{}).Where("id = ?", id).Update("status", "running").Error; err != nil {
			t.Errorf("flip status: %v", err)
		}
	}
	if err := s.Delete(run.ID); !errors.Is(err, ErrRunNotDeletable) {
		t.Fatalf("race delete: %v", err)
	}
	if _, ok := s.Get(run.ID); !ok {
		t.Fatal("run was deleted after it became running")
	}
	var n int64
	db.Model(&models.Artifact{}).Where("run_id = ?", run.ID).Count(&n)
	if n != 1 {
		t.Fatalf("artifact count = %d", n)
	}
	if len(rel.runs) != 0 {
		t.Fatalf("sandbox released despite rejected delete: %#v", rel.runs)
	}
	var checkpoint models.WorkflowVisitCheckpoint
	if err := db.First(&checkpoint, "id = ?", cp.ID).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.RouteState != "sealed" {
		t.Fatalf("checkpoint route = %s, save should still be open", checkpoint.RouteState)
	}
}
