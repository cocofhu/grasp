package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// TestResumeFromFailedNode: a node fails permanently (no rollback/failure edge)
// so the run ends "failed". After the transient cause is cleared, ResumeFrom
// re-drives the run from the failed node, which now succeeds, and the run
// completes — reusing everything the original run already produced.
func TestResumeFromFailedNode(t *testing.T) {
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
	p.failLeft = map[string]int{"risky": 1} // fail once, then succeed on resume
	p.reason = "sandbox hiccup"

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	// Resume with an empty node id: it should auto-pick the failed node.
	if err := eng.ResumeFrom(run.ID, ""); err != nil {
		t.Fatalf("ResumeFrom: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// The failed attempt + the successful retry are both recorded (append-only).
	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "risky").Count(&n)
	if n != 2 {
		t.Errorf("risky execution rows = %d, want 2 (failed + resumed)", n)
	}
	// The downstream node ran after the resume.
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&models.StateRun{}).Error; err != nil {
		t.Fatalf("output node did not run after resume: %v", err)
	}
}

// TestResumeFromExplicitNode resumes from a caller-chosen node id rather than
// the auto-detected failed one.
func TestResumeFromExplicitNode(t *testing.T) {
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

	if err := eng.ResumeFrom(run.ID, "risky"); err != nil {
		t.Fatalf("ResumeFrom(risky): %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

// TestResumeFromRejectsNonResumable: a still-running / completed run can't be
// manually resumed, and an unknown node id is rejected.
func TestResumeFromRejectsNonResumable(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain, Config: map[string]any{"prompt": "x", "produces": "out.md"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "work"},
			{ID: "e2", Source: "work", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")

	// A completed run has nothing to retry.
	if err := eng.ResumeFrom(run.ID, ""); err == nil {
		t.Error("expected rejection resuming a completed run")
	}
	if err := eng.ResumeFrom("missing-run", ""); err == nil {
		t.Error("expected error for a missing run")
	}
}

// TestResumeRestoresVarsAtThatTime: restoreVars rewinds the run's variables to
// the target node's entry snapshot — rewinding a value a later node mutated and
// dropping a variable a later node created — so a re-run sees "当时的状态".
func TestResumeRestoresVarsAtThatTime(t *testing.T) {
	eng, db := setupEngine(t)
	run := models.Run{
		ID: "rv", WorkflowID: "x", WorkflowName: "x", Status: "failed",
		Graph: models.Graph{Nodes: []models.Node{{ID: "n", Type: "agent", Caps: capsPlain}}},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	// Live state as of failure: x was mutated to 99 and a later node created `extra`.
	db.Create(&models.RunVariable{RunID: "rv", Name: "x", Type: "int", Value: float64(99)})
	db.Create(&models.RunVariable{RunID: "rv", Name: "extra", Type: "int", Value: float64(7)})
	// Node n started its (failed) execution with x=1 and no `extra`.
	db.Create(&models.StateRun{RunID: "rv", NodeID: "n", Iteration: 1, Status: "failed",
		VarsBefore: map[string]any{"x": float64(1)}})

	c, err := eng.loadCtx("rv")
	if err != nil {
		t.Fatalf("load ctx: %v", err)
	}
	snap, ok := eng.nodeStartVars("rv", "n")
	if !ok {
		t.Fatal("expected an entry snapshot for node n")
	}
	eng.restoreVars(c, snap)

	if c.vars["x"] != float64(1) {
		t.Errorf("in-memory x = %v, want 1", c.vars["x"])
	}
	if _, has := c.vars["extra"]; has {
		t.Error("in-memory `extra` should be gone after rewind")
	}
	var xv models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", "rv", "x").First(&xv).Error; err != nil {
		t.Fatalf("x row missing: %v", err)
	}
	if xv.Value != float64(1) {
		t.Errorf("persisted x = %v, want 1", xv.Value)
	}
	var cnt int64
	db.Model(&models.RunVariable{}).Where("run_id = ? AND name = ?", "rv", "extra").Count(&cnt)
	if cnt != 0 {
		t.Errorf("`extra` (created by a later node) should be dropped on restore, found %d", cnt)
	}
}

// TestResumeRewindsDownstreamMutationE2E: resume from an upstream node after a
// downstream failure; the value a downstream set_var mutated is rewound to the
// upstream node's entry state before the FSM replays forward.
func TestResumeRewindsDownstreamMutationE2E(t *testing.T) {
	g := models.Graph{
		Variables: []models.Variable{{Name: "x", Type: "number", Value: float64(0)}},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "seed", Type: "set_var", Config: map[string]any{"assignments": []any{
				map[string]any{"var": "x", "expr": "1"},
			}}},
			{ID: "bump", Type: "set_var", Config: map[string]any{"assignments": []any{
				map[string]any{"var": "x", "expr": "x + 1"},
			}}},
			{ID: "risky", Type: "agent", Caps: capsPlain, Config: map[string]any{"prompt": "p", "produces": "out.md"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "seed"},
			{ID: "e2", Source: "seed", Target: "bump"},
			{ID: "e3", Source: "bump", Target: "risky"},
			{ID: "e4", Source: "risky", Target: "output"},
		},
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.failLeft = map[string]int{"risky": 1} // fail once so the run stops after bump ran
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	// After seed(1)→bump(2), x is 2 at failure.
	var xv models.RunVariable
	db.Where("run_id = ? AND name = ?", run.ID, "x").First(&xv)
	if xv.Value != float64(2) {
		t.Fatalf("pre-resume x = %v, want 2", xv.Value)
	}

	// Resume from `bump`: its entry snapshot had x=1, so bump replays 1→2 and
	// risky (no longer failing) succeeds. If the rewind were skipped, bump would
	// compute 2→3 and x would end at 3.
	if err := eng.ResumeFrom(run.ID, "bump"); err != nil {
		t.Fatalf("ResumeFrom(bump): %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	db.Where("run_id = ? AND name = ?", run.ID, "x").First(&xv)
	if xv.Value != float64(2) {
		t.Errorf("post-resume x = %v, want 2 (rewound to 1 then bumped once)", xv.Value)
	}
}

// TestResumeFromUnknownNode: on a failed run, an out-of-graph node id is rejected.
func TestResumeFromUnknownNode(t *testing.T) {
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
	p.failLeft = map[string]int{"risky": 99}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	if err := eng.ResumeFrom(run.ID, "nope"); err == nil {
		t.Error("expected rejection for an unknown node id")
	}
}
