package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func reactSandboxGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "clarify", Type: "agent", Caps: capsClarify, Label: "需求澄清", Config: map[string]any{"prompt": "澄清"}},
			{ID: "plan", Type: "agent", Caps: capsPlan, Label: "计划", Config: map[string]any{"prompt": "计划"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "clarify"},
			{ID: "e2", Source: "clarify", Target: "plan"},
			{ID: "e3", Source: "plan", Target: "output"},
		},
	}
}

// TestReactSandboxSetupFailed keeps the run running, marks the react node failed,
// leaves downstream pending, skips conversation creation, and allows ResumeFrom.
func TestReactSandboxSetupFailed(t *testing.T) {
	setupErr := fmt.Errorf("%w: create sandbox: docker pull registry.example/sandbox:latest: exit status 1",
		errors.New("sandbox setup failed"))

	eng, db := setupEngineGraph(t, reactSandboxGraph())
	fp := eng.provider.(*fakeProvider)
	fp.reactSetupErr = setupErr

	run, err := eng.StartRun("wf", map[string]any{}, "test")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	waitNodeStatus(t, db, run.ID, "clarify", "failed")
	waitRunStatus(t, db, run.ID, "running")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&sr).Error; err != nil {
		t.Fatalf("state run: %v", err)
	}
	if sr.Status != "failed" {
		t.Fatalf("clarify status = %q, want failed", sr.Status)
	}
	if sr.Error == "" {
		t.Fatal("expected full sandbox error on StateRun")
	}

	var planSR models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "plan").First(&planSR).Error; err == nil {
		t.Fatalf("downstream plan should not have started, got %+v", planSR)
	}

	var convCount int64
	db.Model(&models.ReactConversation{}).Where("run_id = ? AND node_id = ?", run.ID, "clarify").Count(&convCount)
	if convCount != 0 {
		t.Fatalf("expected no react conversation on sandbox failure, got %d", convCount)
	}

	rs := services.NewRunService(db)
	for _, it := range rs.AllPendingInboxItems() {
		if c, ok := it.(services.ClarifyInboxItem); ok && c.RunID == run.ID {
			t.Fatalf("sandbox failure run must not appear in clarify inbox: %+v", c)
		}
	}

	fp.reactSetupErr = nil
	if err := eng.ResumeFrom(run.ID, "clarify"); err != nil {
		t.Fatalf("resume from running+failed: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
}

// TestReactClarifyGateStillWaitingHuman ensures a normal ask_question clarify
// path still enters waiting_human.
func TestReactClarifyGateStillWaitingHuman(t *testing.T) {
	eng, db := setupEngineGraph(t, reactSandboxGraph())
	run, err := eng.StartRun("wf", map[string]any{}, "test")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	waitRunStatus(t, db, run.ID, "waiting_human")
	waitReactPause(t, db, run.ID, "clarify")

	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&sr)
	if sr.Status != "waiting_human" {
		t.Fatalf("clarify status = %q, want waiting_human", sr.Status)
	}

	rs := services.NewRunService(db)
	found := false
	for _, it := range rs.AllPendingInboxItems() {
		if c, ok := it.(services.ClarifyInboxItem); ok && c.RunID == run.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected clarify item in inbox for waiting_human run")
	}
}

// TestPlanSandboxFailureStillFinishesRun confirms non-react nodes still fail the
// whole run (f5 scope boundary).
func TestPlanSandboxFailureStillFinishesRun(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "clarify", Type: "agent", Caps: capsClarify, Config: map[string]any{"prompt": "澄清"}},
			{ID: "plan", Type: "agent", Caps: capsPlan, Config: map[string]any{"prompt": "计划"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "clarify"},
			{ID: "e2", Source: "clarify", Target: "plan"},
			{ID: "e3", Source: "plan", Target: "output"},
		},
	}
	eng, db := setupEngineGraph(t, g)
	fp := eng.provider.(*fakeProvider)

	// Arm the plan failure before the run can reach it. The clarify react node
	// runs via ReactOpen/ReactReply, which never consult failLeft, so injecting
	// it up-front is safe and avoids racing the dispatcher: once ReactReply
	// resumes the run, the plan node may execute before a post-reply mutation
	// lands, letting plan succeed and the run complete (flaky under CI timing).
	fp.mu.Lock()
	fp.failLeft = map[string]int{"plan": 1}
	fp.reason = "plan 执行失败:sandbox setup failed: create sandbox"
	fp.mu.Unlock()

	run, err := eng.StartRun("wf", map[string]any{}, "test")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.ReactReply(run.ID, "clarify", "ok", nil, nil, true); err != nil {
		t.Fatalf("clarify confirm: %v", err)
	}

	waitRunStatus(t, db, run.ID, "failed")
}
