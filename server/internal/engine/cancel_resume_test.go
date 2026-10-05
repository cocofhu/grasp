package engine

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TestCancelDuringAgentAllowsResume is the regression for run-aaf0f3d4-class
// stuck runs: Cancel while RunAgent is blocked flips the run to cancelled but
// used to leave execRuns claimed and implement StateRun "running", so ResumeFrom
// returned "run 正在执行中" forever and the UI could not continue.
func TestCancelDuringAgentAllowsResume(t *testing.T) {
	eng, db, p := setupBlockingEngine(t, slowGraph(), 5)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitAdmissionUntil(t, func() bool {
		var sr models.StateRun
		return db.Where("run_id = ? AND node_id = ? AND status = ?", run.ID, "work", "running").
			First(&sr).Error == nil
	}, 3*time.Second)

	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "work").
		Order("iteration desc").First(&sr).Error; err != nil {
		t.Fatalf("load work state: %v", err)
	}
	if sr.Status != "cancelled" {
		t.Fatalf("work status = %q, want cancelled after Cancel", sr.Status)
	}
	if eng.isExecuting(run.ID) {
		t.Fatal("exec slot still held after Cancel; ResumeFrom would be blocked")
	}

	// Resume while the original provider call may still be blocked — the gen-guarded
	// forceEndExecute must let a fresh driver admit instead of "正在执行中".
	if err := eng.ResumeFrom(run.ID, "work"); err != nil {
		t.Fatalf("ResumeFrom after cancel-during-agent: %v", err)
	}
	waitAdmissionUntil(t, func() bool {
		var sr2 models.StateRun
		return db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "work", 2).
			First(&sr2).Error == nil
	}, 3*time.Second)

	p.releaseRun(run.ID)
	waitAdmissionUntil(t, func() bool {
		var r models.Run
		if db.First(&r, "id = ?", run.ID).Error != nil {
			return false
		}
		return r.Status == "completed" || r.Status == "failed"
	}, 5*time.Second)

	// Iteration 1 must stay cancelled (late zombie completion must not revive it).
	if err := db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "work", 1).
		First(&sr).Error; err != nil {
		t.Fatalf("reload iter1: %v", err)
	}
	if sr.Status != "cancelled" {
		t.Fatalf("iter1 status = %q, want cancelled (not revived)", sr.Status)
	}
	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "work").Count(&n)
	if n < 2 {
		t.Fatalf("work executions = %d, want >= 2 after resume", n)
	}
}

// TestCancelHealUnblocksZombieExecSlot: when a terminal run still holds execRuns
// (simulated), Cancel heal must clear the slot and finalize sticky StateRuns so
// ResumeFrom is no longer blocked with "正在执行中".
func TestCancelHealUnblocksZombieExecSlot(t *testing.T) {
	eng, db, p := setupBlockingEngine(t, slowGraph(), 5)
	runID := "run-zombie-exec"
	db.Create(&models.Run{
		ID: runID, WorkflowID: "wf", WorkflowName: "z", Status: "cancelled",
		Graph: slowGraph(), McpToken: eng.host.RegisterRun(runID),
	})
	db.Create(&models.StateRun{RunID: runID, NodeID: "work", NodeType: "agent", Iteration: 1, Status: "running"})

	if _, ok := eng.beginExecute(runID); !ok {
		t.Fatal("beginExecute")
	}
	if !eng.isExecuting(runID) {
		t.Fatal("expected zombie claim")
	}

	if err := eng.Cancel(runID); err != nil {
		t.Fatalf("Cancel heal: %v", err)
	}
	if eng.isExecuting(runID) {
		t.Fatal("Cancel heal left exec slot claimed")
	}
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", runID, "work").First(&sr)
	if sr.Status != "cancelled" {
		t.Fatalf("heal StateRun status = %q, want cancelled", sr.Status)
	}

	// Production stuck symptom: ResumeFrom must not return "正在执行中".
	if err := eng.ResumeFrom(runID, "work"); err != nil {
		t.Fatalf("ResumeFrom after heal: %v", err)
	}
	waitAdmissionUntil(t, func() bool {
		var sr2 models.StateRun
		return db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, "work", 2).
			First(&sr2).Error == nil
	}, 3*time.Second)
	p.releaseRun(runID)
	waitRunStatus(t, db, runID, "completed")
	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", runID, "work").Count(&n)
	if n < 2 {
		t.Fatalf("work executions = %d after resume, want >= 2", n)
	}
}

// TestResumeFromForceClearsZombieSlotWithoutCancelHeal: ResumeFrom itself must
// steal a stuck exec slot on a terminal run (operator may hit 续跑 without
// clicking Cancel again).
func TestResumeFromForceClearsZombieSlotWithoutCancelHeal(t *testing.T) {
	eng, db, p := setupBlockingEngine(t, slowGraph(), 5)
	runID := "run-resume-force"
	db.Create(&models.Run{
		ID: runID, WorkflowID: "wf", WorkflowName: "z", Status: "cancelled",
		Graph: slowGraph(), McpToken: eng.host.RegisterRun(runID),
	})
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "work", NodeType: "agent", Iteration: 1,
		Status: "cancelled", Error: "run 已取消,节点未收尾",
	})
	if _, ok := eng.beginExecute(runID); !ok {
		t.Fatal("beginExecute")
	}

	if err := eng.ResumeFrom(runID, "work"); err != nil {
		t.Fatalf("ResumeFrom: %v (want success after force-clear, not 正在执行中)", err)
	}
	waitAdmissionUntil(t, func() bool {
		var sr models.StateRun
		return db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, "work", 2).
			First(&sr).Error == nil
	}, 3*time.Second)
	p.releaseRun(runID)
	waitRunStatus(t, db, runID, "completed")
	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", runID, "work").Count(&n)
	if n < 2 {
		t.Fatalf("work executions = %d after resume, want >= 2", n)
	}
}

// TestSaveStateDoesNotReviveCancelledVisit: late completed outcomes must not
// overwrite a StateRun that Cancel already finalized.
func TestSaveStateDoesNotReviveCancelledVisit(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-save-guard"
	db.Create(&models.Run{ID: runID, WorkflowID: "x", WorkflowName: "x", Status: "cancelled",
		Graph: models.Graph{Nodes: []models.Node{{ID: "work", Type: "agent", Caps: capsPlain}}}})
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "work", NodeType: "agent", Iteration: 1,
		Status: "cancelled", Error: "run 已取消,节点未收尾",
	})

	c := &execCtx{
		run:  &models.Run{ID: runID, Status: "cancelled"},
		iter: map[string]int{"work": 1},
		vars: map[string]any{},
	}
	node := &models.Node{ID: "work", Type: "agent", Caps: capsPlain}
	eng.saveState(c, node, nodeOutcome{status: "completed", outputMd: "should not land", outputs: map[string]any{"x": 1}})

	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, "work", 1).First(&sr)
	if sr.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", sr.Status)
	}
	if sr.OutputMd == "should not land" {
		t.Fatal("late completed outcome overwrote cancelled StateRun")
	}
	if sr.Error != "run 已取消,节点未收尾" {
		t.Fatalf("error cleared/changed: %q", sr.Error)
	}
}

// TestExecOwnershipGenGuardsZombieEndExecute: forceEndExecute bumps generation
// so a late endExecute from the zombie cannot drop the new driver's claim.
func TestExecOwnershipGenGuardsZombieEndExecute(t *testing.T) {
	eng, _ := setupEngine(t)
	runID := "run-gen-guard"

	gen1, ok := eng.beginExecute(runID)
	if !ok {
		t.Fatal("beginExecute 1")
	}
	eng.forceEndExecute(runID)
	if eng.isExecuting(runID) {
		t.Fatal("still executing after forceEndExecute")
	}
	if eng.isExecOwner(runID, gen1) {
		t.Fatal("zombie gen must not own after forceEndExecute")
	}

	gen2, ok := eng.beginExecute(runID)
	if !ok {
		t.Fatal("beginExecute 2")
	}
	if gen2 == gen1 {
		t.Fatalf("gen did not bump: %d", gen2)
	}
	// Zombie teardown must be a no-op against the new owner.
	eng.endExecute(runID, gen1)
	if !eng.isExecuting(runID) {
		t.Fatal("zombie endExecute cleared the new driver's slot")
	}
	if !eng.isExecOwner(runID, gen2) {
		t.Fatal("new driver lost ownership")
	}
	eng.endExecute(runID, gen2)
	if eng.isExecuting(runID) {
		t.Fatal("owner endExecute did not release")
	}
}

// TestCancelCompletedStillRejected: heal applies to cancelled/failed only;
// completed runs stay rejected.
func TestCancelCompletedStillRejected(t *testing.T) {
	eng, db := setupEngine(t)
	db.Create(&models.Run{ID: "run-done", Status: "completed", Graph: models.Graph{}})
	if err := eng.Cancel("run-done"); err == nil {
		t.Fatal("expected error cancelling completed run")
	}
}

// TestZombieDoesNotAdvanceAfterResumeFrom: Cancel mid-agent, ResumeFrom admits a
// new driver, then release the blocked provider — the zombie must not open an
// extra node visit or flip the run oddly; only the new driver's visit proceeds.
func TestZombieDoesNotAdvanceAfterResumeFrom(t *testing.T) {
	eng, db, p := setupBlockingEngine(t, slowGraph(), 5)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitAdmissionUntil(t, func() bool {
		var sr models.StateRun
		return db.Where("run_id = ? AND node_id = ? AND status = ?", run.ID, "work", "running").
			First(&sr).Error == nil
	}, 3*time.Second)

	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")

	if err := eng.ResumeFrom(run.ID, "work"); err != nil {
		t.Fatalf("ResumeFrom: %v", err)
	}
	waitAdmissionUntil(t, func() bool {
		var sr models.StateRun
		return db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "work", 2).
			First(&sr).Error == nil
	}, 3*time.Second)

	// Count work rows before release (zombie still inside RunAgent).
	var before int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "work").Count(&before)
	if before < 2 {
		t.Fatalf("before release work rows = %d, want >= 2 (iter1 cancelled + iter2 resumed)", before)
	}

	p.releaseRun(run.ID)
	waitAdmissionUntil(t, func() bool {
		var r models.Run
		if db.First(&r, "id = ?", run.ID).Error != nil {
			return false
		}
		// Resume may complete or fail (e.g. transient MCP rebind); either is
		// fine — the regression is zombie advancement, not happy-path success.
		return r.Status == "completed" || r.Status == "failed"
	}, 5*time.Second)

	var after int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "work").Count(&after)
	// Zombie must not open an extra visit beyond what the new driver already started.
	if after != before {
		t.Fatalf("work rows changed after release: before=%d after=%d (zombie advanced?)", before, after)
	}

	time.Sleep(50 * time.Millisecond)
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "work").Count(&after)
	if after != before {
		t.Fatalf("after settle work rows = %d, want %d", after, before)
	}
}
