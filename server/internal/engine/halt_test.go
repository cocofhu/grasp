package engine

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestHaltAndCancelQueuedRuns(t *testing.T) {
	eng, db := setupEngine(t)
	// Halt before inserting queued runs so the background dispatcher cannot
	// admit one between Create and CancelQueuedRuns (race that made this flaky).
	eng.Halt()
	now := time.Now()
	db.Create(&models.Run{ID: "q1", Status: "queued", StartedAt: now, Graph: testClarifyGraph()})
	db.Create(&models.Run{ID: "q2", Status: "queued", StartedAt: now, Graph: testClarifyGraph()})
	if !eng.IsHalted() {
		t.Fatal("expected halted")
	}
	if n := eng.CancelQueuedRuns(); n != 2 {
		t.Fatalf("cancelled queued: %d", n)
	}
	var q1 models.Run
	db.First(&q1, "id = ?", "q1")
	if q1.Status != "cancelled" {
		t.Fatalf("q1 status: %s", q1.Status)
	}
}

func TestWaitAgentReactCompletes(t *testing.T) {
	eng, db := setupEngine(t)
	if timedOut := eng.WaitAgentReact(context.Background(), time.Now().Add(time.Second)); timedOut {
		t.Fatal("expected immediate completion with no active runs")
	}

	now := time.Now()
	db.Create(&models.Run{ID: "r1", Status: "running", StartedAt: now, Graph: testClarifyGraph()})
	db.Create(&models.StateRun{RunID: "r1", NodeID: "clarify", NodeType: "agent", Iteration: 1, Status: "running", StartedAt: &now})

	done := make(chan bool, 1)
	go func() {
		done <- eng.WaitAgentReact(context.Background(), time.Now().Add(5*time.Second))
	}()

	time.Sleep(100 * time.Millisecond)
	db.Model(&models.StateRun{}).Where("run_id = ?", "r1").Update("status", "completed")
	select {
	case timedOut := <-done:
		if timedOut {
			t.Fatal("expected completion without timeout")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait timed out")
	}
}

func TestWaitAgentReactForceCancel(t *testing.T) {
	eng, db := setupEngine(t)
	now := time.Now()
	db.Create(&models.Run{ID: "r2", Status: "running", StartedAt: now, Graph: testClarifyGraph()})
	db.Create(&models.StateRun{RunID: "r2", NodeID: "clarify", NodeType: "agent", Iteration: 1, Status: "running", StartedAt: &now})

	if timedOut := eng.WaitAgentReact(context.Background(), time.Now().Add(-time.Second)); !timedOut {
		t.Fatal("expected timeout force cancel")
	}
	var r2 models.Run
	db.First(&r2, "id = ?", "r2")
	if r2.Status != "cancelled" {
		t.Fatalf("run status: %s", r2.Status)
	}
}
