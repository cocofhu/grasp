package engine

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
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
