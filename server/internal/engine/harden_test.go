package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// TestDoRollbackPreservesCarriedLastError verifies the failure path's contract:
// the engine sets vars.last_error to the failure reason before routing, and a
// rollback edge that carries it must NOT clobber that value with an empty
// string. This is what lets the retried upstream node see why it failed.
func TestDoRollbackPreservesCarriedLastError(t *testing.T) {
	eng, db := setupEngine(t)

	run := models.Run{
		ID: "le1", WorkflowID: "x", WorkflowName: "x", Status: "running",
		Checkpoints: map[string]map[string]any{},
		Graph:       models.Graph{Nodes: []models.Node{{ID: "cp", Type: "agent", Caps: capsPlain, Checkpoint: true}}},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	c, err := eng.loadCtx("le1")
	if err != nil {
		t.Fatalf("load ctx: %v", err)
	}
	eng.snapshotCheckpoint(c, "cp")

	// Simulate the failed-node path having recorded the reason.
	c.setVar("last_error", "boom: contract miss")
	eng.persistVar("le1", "last_error", "boom: contract miss")

	ed := models.Edge{ID: "e", Source: "impl", Target: "cp", Kind: models.EdgeRollback,
		Carry: []string{"last_error"}, MaxAttempts: 3}
	if _, ok := eng.doRollback(c, ed); !ok {
		t.Fatalf("rollback refused unexpectedly")
	}
	if got, _ := c.vars["last_error"].(string); got != "boom: contract miss" {
		t.Errorf("carried last_error = %q, want the failure reason preserved", got)
	}
}

// TestLoadCtxRestoresMcpTokenAfterRestart simulates a server restart: the
// in-memory token map is empty, but the run persisted its MCP token. loadCtx
// must re-bind it (engine map + host) so resumed sandbox MCP calls authorize.
func TestLoadCtxRestoresMcpTokenAfterRestart(t *testing.T) {
	eng, db := setupEngine(t)

	const tok = "restored-token-abc"
	run := models.Run{
		ID: "tok1", WorkflowID: "x", WorkflowName: "x", Status: "waiting_human",
		McpToken: tok, Graph: models.Graph{Nodes: []models.Node{{ID: "n", Type: "agent", Caps: capsPlain}}},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Before restore: the host must reject the token (nothing registered).
	if _, err := eng.host.ListArtifacts("tok1", tok); err == nil {
		t.Fatalf("expected unauthorized before restore")
	}

	c, err := eng.loadCtx("tok1")
	if err != nil {
		t.Fatalf("load ctx: %v", err)
	}
	if c.token != tok {
		t.Errorf("ctx token = %q, want %q", c.token, tok)
	}
	eng.mu.Lock()
	inMem := eng.tokens["tok1"]
	eng.mu.Unlock()
	if inMem != tok {
		t.Errorf("engine token map = %q, want %q", inMem, tok)
	}
	// After restore: the same token authorizes against the host again.
	if _, err := eng.host.ListArtifacts("tok1", tok); err != nil {
		t.Errorf("token should authorize after restore, got %v", err)
	}
}
