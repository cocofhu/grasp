package engine

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestHaltKeepsQueuedRuns(t *testing.T) {
	eng, db := setupEngine(t)
	eng.Halt()
	now := time.Now()
	db.Create(&models.Run{ID: "q1", Status: "queued", StartedAt: now, Graph: testClarifyGraph()})
	if !eng.IsHalted() {
		t.Fatal("expected halted")
	}
	eng.drainQueue()
	var q1 models.Run
	db.First(&q1, "id = ?", "q1")
	if q1.Status != "queued" {
		t.Fatalf("q1 status: %s", q1.Status)
	}
}

func TestHaltKeepsDirectResumeQueuedWithoutAdmittingDriver(t *testing.T) {
	eng, db := setupEngine(t)
	eng.Halt()
	run := models.Run{ID: "halted-resume", Status: "waiting_human", Graph: testClarifyGraph()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	var admissions atomic.Int64
	if err := db.Callback().Update().Before("gorm:update").Register("test:count-halted-admissions", func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "runs" && values["status"] == "running" {
			admissions.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	// This may be a resume that passed its handler's halt check before Halt.
	eng.scheduleRunAdmission(run.ID, "clarify")
	if err := db.First(&run, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if admissions.Load() != 0 || eng.ActiveDrivers() != 0 || run.Status != "queued" {
		t.Fatalf("halted admission escaped: attempted=%d active=%d status=%s", admissions.Load(), eng.ActiveDrivers(), run.Status)
	}
	if node, ok := eng.admissionFromNode(run); !ok || node != "clarify" {
		t.Fatalf("resume checkpoint lost: %s %v", node, ok)
	}
}
