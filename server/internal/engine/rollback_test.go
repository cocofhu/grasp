package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// TestRollbackRestoresCheckpoint verifies that a rollback transition restores
// the target checkpoint's variable snapshot, injects carried context, counts
// attempts, and refuses once maxAttempts is exceeded.
func TestRollbackRestoresCheckpoint(t *testing.T) {
	eng, db := setupEngine(t)

	run := models.Run{
		ID: "rb1", WorkflowID: "x", WorkflowName: "x", Status: "running",
		Checkpoints: map[string]map[string]any{},
		Graph:       models.Graph{Nodes: []models.Node{{ID: "cp", Type: "agent", Caps: capsPlain, Checkpoint: true}}},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	db.Create(&models.RunVariable{RunID: "rb1", Name: "attempt", Type: "int", Value: float64(0)})

	c, err := eng.loadCtx("rb1")
	if err != nil {
		t.Fatalf("load ctx: %v", err)
	}

	// Snapshot at the checkpoint with attempt=0, then mutate.
	eng.snapshotCheckpoint(c, "cp")
	c.setVar("attempt", float64(5))
	eng.persistVar("rb1", "attempt", float64(5))

	ed := models.Edge{ID: "e", Source: "bump", Target: "cp", Kind: models.EdgeRollback,
		Carry: []string{"last_error"}, MaxAttempts: 3}

	target, ok := eng.doRollback(c, ed)
	if !ok || target != "cp" {
		t.Fatalf("rollback returned (%q,%v), want (cp,true)", target, ok)
	}
	if f, _ := c.vars["attempt"].(float64); f != 0 {
		t.Errorf("attempt not restored: got %v want 0", c.vars["attempt"])
	}
	if _, has := c.vars["last_error"]; !has {
		t.Errorf("carry var last_error not injected")
	}
	if c.run.Attempt != 1 {
		t.Errorf("attempt counter = %d, want 1", c.run.Attempt)
	}

	// Exhaust the cap.
	c.run.Attempt = 3
	if _, ok := eng.doRollback(c, ed); ok {
		t.Errorf("rollback should be refused past maxAttempts")
	}
	if c.run.Attempt != 3 {
		t.Errorf("attempt counter should stay 3 on refusal, got %d", c.run.Attempt)
	}
}
