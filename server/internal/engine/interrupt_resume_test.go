package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// TestFinalizeRunningStateRunsOnFinish: finish(failed/cancelled) syncs any
// still-running StateRun to the run's terminal status with a clear error.
func TestFinalizeRunningStateRunsOnFinish(t *testing.T) {
	eng, db := setupEngine(t)

	t.Run("failed", func(t *testing.T) {
		run := models.Run{ID: "f1", WorkflowID: "x", WorkflowName: "x", Status: "running",
			Graph: models.Graph{Nodes: []models.Node{{ID: "n", Type: "agent", Caps: capsPlain}}}}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run: %v", err)
		}
		sr := models.StateRun{RunID: "f1", NodeID: "n", NodeType: "agent", Iteration: 1, Status: "running"}
		if err := db.Create(&sr).Error; err != nil {
			t.Fatalf("create state_run: %v", err)
		}

		eng.finish("f1", "failed")

		var got models.Run
		db.First(&got, "id = ?", "f1")
		if got.Status != "failed" {
			t.Errorf("run status = %q, want failed", got.Status)
		}
		var st models.StateRun
		db.First(&st, "id = ?", sr.ID)
		if st.Status != "failed" {
			t.Errorf("state_run status = %q, want failed", st.Status)
		}
		if !strings.Contains(st.Error, "节点未收尾") {
			t.Errorf("state_run error = %q, want interrupt message", st.Error)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		run := models.Run{ID: "c1", WorkflowID: "x", WorkflowName: "x", Status: "running",
			Graph: models.Graph{Nodes: []models.Node{{ID: "n", Type: "agent", Caps: capsPlain}}}}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run: %v", err)
		}
		sr := models.StateRun{RunID: "c1", NodeID: "n", NodeType: "agent", Iteration: 1, Status: "running"}
		if err := db.Create(&sr).Error; err != nil {
			t.Fatalf("create state_run: %v", err)
		}

		eng.finish("c1", "cancelled")

		var st models.StateRun
		db.First(&st, "id = ?", sr.ID)
		if st.Status != "cancelled" {
			t.Errorf("state_run status = %q, want cancelled", st.Status)
		}
		if !strings.Contains(st.Error, "已取消") {
			t.Errorf("state_run error = %q, want cancel message", st.Error)
		}
	})

	t.Run("already_failed_noop", func(t *testing.T) {
		run := models.Run{ID: "f2", WorkflowID: "x", WorkflowName: "x", Status: "running",
			Graph: models.Graph{Nodes: []models.Node{{ID: "n", Type: "agent", Caps: capsPlain}}}}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run: %v", err)
		}
		sr := models.StateRun{RunID: "f2", NodeID: "n", NodeType: "agent", Iteration: 1,
			Status: "failed", Error: "node error"}
		if err := db.Create(&sr).Error; err != nil {
			t.Fatalf("create state_run: %v", err)
		}

		eng.finish("f2", "failed")

		var st models.StateRun
		db.First(&st, "id = ?", sr.ID)
		if st.Error != "node error" {
			t.Errorf("state_run error overwritten = %q, want node error", st.Error)
		}
	})
}

// TestPickAutoResumeNodeLevels exercises the three-level auto-pick order.
func TestPickAutoResumeNodeLevels(t *testing.T) {
	eng, db := setupEngine(t)

	t.Run("level1_cancelled", func(t *testing.T) {
		runID := "l1"
		db.Create(&models.Run{ID: runID, WorkflowID: "x", WorkflowName: "x", Status: "cancelled",
			Graph: models.Graph{Nodes: []models.Node{{ID: "a", Type: "agent", Caps: capsPlain}, {ID: "b", Type: "agent", Caps: capsPlain}}}})
		db.Create(&models.StateRun{RunID: runID, NodeID: "a", Iteration: 1, Status: "completed"})
		db.Create(&models.StateRun{RunID: runID, NodeID: "b", Iteration: 1, Status: "cancelled"})

		got := eng.pickAutoResumeNode(runID)
		if got != "b" {
			t.Errorf("pick = %q, want b (cancelled level-1)", got)
		}
	})

	t.Run("level2_running", func(t *testing.T) {
		runID := "l2"
		db.Create(&models.Run{ID: runID, WorkflowID: "x", WorkflowName: "x", Status: "failed",
			Graph: models.Graph{Nodes: []models.Node{{ID: "a", Type: "agent", Caps: capsPlain}, {ID: "b", Type: "agent", Caps: capsPlain}}}})
		db.Create(&models.StateRun{RunID: runID, NodeID: "a", Iteration: 1, Status: "completed"})
		db.Create(&models.StateRun{RunID: runID, NodeID: "b", Iteration: 1, Status: "running"})

		got := eng.pickAutoResumeNode(runID)
		if got != "b" {
			t.Errorf("pick = %q, want b (running level-2)", got)
		}
	})

	t.Run("level3_last_any", func(t *testing.T) {
		runID := "l3"
		db.Create(&models.Run{ID: runID, WorkflowID: "x", WorkflowName: "x", Status: "failed",
			Graph: models.Graph{Nodes: []models.Node{{ID: "a", Type: "agent", Caps: capsPlain}, {ID: "b", Type: "agent", Caps: capsPlain}}}})
		db.Create(&models.StateRun{RunID: runID, NodeID: "a", Iteration: 1, Status: "completed"})
		db.Create(&models.StateRun{RunID: runID, NodeID: "b", Iteration: 1, Status: "completed"})

		got := eng.pickAutoResumeNode(runID)
		if got != "b" {
			t.Errorf("pick = %q, want b (last completed level-3)", got)
		}
	})

	t.Run("empty_no_state_runs", func(t *testing.T) {
		runID := "l4"
		db.Create(&models.Run{ID: runID, WorkflowID: "x", WorkflowName: "x", Status: "failed",
			Graph: models.Graph{Nodes: []models.Node{{ID: "a", Type: "agent", Caps: capsPlain}}}})

		got := eng.pickAutoResumeNode(runID)
		if got != "" {
			t.Errorf("pick = %q, want empty", got)
		}
	})
}

// TestResumeFromOrphanRunning: Run=failed with only a running StateRun (no
// failed row) auto-resumes via level-2 pick after finish syncs the orphan.
func TestResumeFromOrphanRunning(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "risky", Type: "agent", Caps: capsPlain, Config: map[string]any{"prompt": "x", "produces": "out.md"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "risky"},
			{ID: "e2", Source: "risky", Target: "output", Kind: models.EdgeSuccess},
		},
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.failLeft = map[string]int{"risky": 1}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	// Simulate interrupt: revert the failed StateRun back to running (orphan).
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "risky").Order("iteration desc").First(&sr)
	db.Model(&sr).Updates(map[string]any{"status": "running", "error": ""})

	if err := eng.ResumeFrom(run.ID, ""); err != nil {
		t.Fatalf("ResumeFrom auto: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

// TestResumeFromCancelledRun auto-picks a cancelled StateRun at level 1.
func TestResumeFromCancelledRun(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain, Config: map[string]any{"prompt": "x", "produces": "out.md"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "work"},
			{ID: "e2", Source: "work", Target: "output", Kind: models.EdgeSuccess},
		},
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.failLeft = map[string]int{"work": 1}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	// Simulate shutdown-style cancellation on the failed node.
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "work").Order("iteration desc").First(&sr)
	db.Model(&sr).Updates(map[string]any{"status": "cancelled", "error": "shutdown grace 超时"})
	db.Model(&models.Run{}).Where("id = ?", run.ID).Update("status", "cancelled")

	if err := eng.ResumeFrom(run.ID, ""); err != nil {
		t.Fatalf("ResumeFrom cancelled: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

// TestResumeFromPendingNode: explicit resume of a node with no StateRun skips
// VarsBefore restore and runs from the current persisted variable state.
func TestResumeFromPendingNode(t *testing.T) {
	g := models.Graph{
		Variables: []models.Variable{{Name: "x", Type: "number", Value: float64(0)}},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "seed", Type: "set_var", Config: map[string]any{"assignments": []any{
				map[string]any{"var": "x", "expr": "1"},
			}}},
			{ID: "pending", Type: "set_var", Config: map[string]any{"assignments": []any{
				map[string]any{"var": "x", "expr": "x + 10"},
			}}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "seed"},
			{ID: "e2", Source: "seed", Target: "pending"},
			{ID: "e3", Source: "pending", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	runID := "pending-resume"
	db.Create(&models.Run{ID: runID, WorkflowID: "wf", WorkflowName: "wf", Status: "failed", Graph: g})
	db.Create(&models.StateRun{RunID: runID, NodeID: "input", NodeType: "input", Iteration: 1, Status: "completed"})
	db.Create(&models.StateRun{RunID: runID, NodeID: "seed", NodeType: "set_var", Iteration: 1, Status: "completed",
		VarsBefore: map[string]any{"x": float64(0)}})
	db.Create(&models.RunVariable{RunID: runID, Name: "x", Type: "int", Value: float64(1)})

	var cnt int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", runID, "pending").Count(&cnt)
	if cnt != 0 {
		t.Fatalf("pending node should have no StateRun, got %d", cnt)
	}

	if err := eng.ResumeFrom(runID, "pending"); err != nil {
		t.Fatalf("ResumeFrom(pending): %v", err)
	}
	waitRunStatus(t, db, runID, "completed")

	// No VarsBefore for pending: uses current x=1 from seed, not rewound to 0.
	var xv models.RunVariable
	db.Where("run_id = ? AND name = ?", runID, "x").First(&xv)
	if xv.Value != float64(11) {
		t.Errorf("x = %v, want 11 (current vars, no VarsBefore restore)", xv.Value)
	}
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", runID, "pending").Count(&cnt)
	if cnt != 1 {
		t.Errorf("pending StateRun count = %d, want 1", cnt)
	}
}

// TestResumeFromNoStateRunsError distinguishes empty history from generic failure.
func TestResumeFromNoStateRunsError(t *testing.T) {
	eng, db := setupEngine(t)
	run := models.Run{
		ID: "empty", WorkflowID: "x", WorkflowName: "x", Status: "failed",
		Graph: models.Graph{Nodes: []models.Node{{ID: "n", Type: "agent", Caps: capsPlain}}},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	err := eng.ResumeFrom("empty", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "无任何节点执行记录") {
		t.Errorf("error = %q, want no-state-runs message", err.Error())
	}
}
