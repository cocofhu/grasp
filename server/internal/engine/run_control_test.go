package engine

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestPauseOneRunAtBoundaryLeavesOtherRunActive(t *testing.T) {
	e, db, provider := setupBlockingEngine(t, slowGraph(), 2)
	defer provider.ReleaseAll()
	first, err := e.StartRun("wf", nil, "manual")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.StartRun("wf", nil, "manual")
	if err != nil {
		t.Fatal(err)
	}
	waitAdmissionUntil(t, func() bool {
		var count int64
		db.Model(&models.StateRun{}).Where("run_id IN ? AND node_id = ? AND status = ?", []string{first.ID, second.ID}, "work", "running").Count(&count)
		return count == 2
	}, 5*time.Second)
	if err := e.PauseRun(first.ID, 0); err != nil {
		t.Fatal(err)
	}
	var row models.Run
	if err := db.First(&row, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "running" || !row.PauseRequested || e.IsHalted() {
		t.Fatal("pause acknowledged before the safe boundary, or globally halted")
	}
	provider.releaseRun(first.ID)
	waitAdmissionUntil(t, func() bool { return e.runStatus(first.ID) == "paused" }, 5*time.Second)
	if e.runStatus(second.ID) != "running" {
		t.Fatal("another run was paused")
	}
	if err := e.ResumePausedRun(first.ID, 0); err == nil {
		t.Fatal("stale control version accepted")
	}
	// Disabling new workflows does not disable management of an existing Run.
	if err := services.NewSettingsService(db, nil, nil).SetWorkflowFeature(false); err != nil {
		t.Fatal(err)
	}
	if err := e.ResumePausedRun(first.ID, 1); err != nil {
		t.Fatal(err)
	}
	waitAdmissionUntil(t, func() bool { return e.runStatus(first.ID) == "completed" }, 5*time.Second)
	var workExecutions int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", first.ID, "work").Count(&workExecutions)
	if workExecutions != 1 {
		t.Fatalf("resumed run repeated its completed work: %d", workExecutions)
	}
	if e.runStatus(second.ID) != "running" {
		t.Fatal("resuming affected another run")
	}
}

func TestPausedGateRequiresExplicitResume(t *testing.T) {
	e, db, _ := setupBlockingEngine(t, gateThenWorkGraph(), 1)
	run, err := e.StartRun("wf", nil, "manual")
	if err != nil {
		t.Fatal(err)
	}
	waitAdmissionUntil(t, func() bool { return e.runStatus(run.ID) == "waiting_human" }, 5*time.Second)
	if err := e.PauseRun(run.ID, 0); err != nil {
		t.Fatal(err)
	}
	if e.runStatus(run.ID) != "paused" {
		t.Fatal("idle human gate did not acknowledge pause")
	}
	if err := e.ResumeGate(run.ID, "gate", "ok", nil); err == nil {
		t.Fatal("gate bypassed the run pause")
	}
	var gate models.Gate
	if err := db.Where("run_id = ?", run.ID).First(&gate).Error; err != nil {
		t.Fatal(err)
	}
	if gate.Resolved {
		t.Fatal("paused gate was resolved")
	}
	if err := e.ResumePausedRun(run.ID, 1); err != nil {
		t.Fatal(err)
	}
	if e.runStatus(run.ID) != "waiting_human" {
		t.Fatal("resume reran the gate instead of preserving its existing iteration")
	}
}
