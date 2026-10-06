package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

// setupRecEngine builds an engine over graph g with a fake provider that emits
// its built-in MCP calls through the JSON-RPC endpoint, so per-execution
// StateRun.McpCalls can be asserted end-to-end. The history provider is wired so
// RunHistory can be exercised too.
func setupRecEngine(t *testing.T, g models.Graph, fp *fakeProvider) (*Engine, *gorm.DB, *mcp.Host) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	wf := models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf", Name: "wf", Version: 1, PublishedVersion: 1, Graph: g}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: g}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	host.SetHistoryProvider(services.NewRunService(db))
	fp.host = host
	fp.recordCalls = true
	eng := New(db, fp, host, arts, 5)
	cleanupEngineDB(t, eng, db)
	return eng, db, host
}

// reviseLoopGraph: input → design(agent) → gate(human_gate); gate revises back
// to design (action=='revise') or approves to output (action=='approve').
func reviseLoopGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "design", Type: "agent", Caps: capsPlain, Label: "视觉设计", Config: map[string]any{"prompt": "设计", "produces": "design.md"}},
			{ID: "gate", Type: "human_gate", Label: "设计门禁", Config: map[string]any{"title": "确认设计",
				"actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
					map[string]any{"id": "revise", "label": "退回"},
				}}},
			{ID: "output", Type: "output", Label: "输出"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "design"},
			{ID: "e2", Source: "design", Target: "gate"},
			{ID: "e3", Source: "gate", Target: "output", SourceHandle: "approve", Kind: models.EdgeSuccess},
			{ID: "e4", Source: "gate", Target: "design", SourceHandle: "revise", Kind: models.EdgeSuccess},
		},
	}
}

// TestReviseLoopTracePerIteration drives a gate revise loop and asserts each
// re-execution keeps its own independent MCP-call trace and each gate visit its
// own human feedback — nothing merges across iterations.
func TestReviseLoopTracePerIteration(t *testing.T) {
	fp := &fakeProvider{}
	eng, db, host := setupRecEngine(t, reviseLoopGraph(), fp)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// First gate visit → revise with feedback "用直角" (loops back to design).
	waitGatePending(t, db, run.ID, "gate")
	if err := eng.ResumeGate(run.ID, "gate", "revise", map[string]any{"comment": "用直角"}); err != nil {
		t.Fatalf("resume revise: %v", err)
	}

	// design re-runs (iteration 2) and the gate re-opens → approve to finish.
	// Wait on the iteration-2 gate specifically: waiting on any pending gate can
	// race with the iteration-1 gate still being torn down and approve the wrong
	// visit.
	waitGateIteration(t, db, run.ID, "gate", 2)
	if err := eng.ResumeGate(run.ID, "gate", "approve", map[string]any{"comment": "ok"}); err != nil {
		t.Fatalf("resume approve: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// design executed twice; each iteration has its OWN single recorded call.
	var designRuns []models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "design").Order("iteration asc").Find(&designRuns)
	if len(designRuns) != 2 {
		t.Fatalf("expected 2 design executions, got %d", len(designRuns))
	}
	for i, sr := range designRuns {
		if sr.Iteration != i+1 {
			t.Fatalf("design row %d has iteration %d", i, sr.Iteration)
		}
		if len(sr.McpCalls) != 2 {
			t.Fatalf("design iter %d should have 2 MCP calls (list_artifacts + node_complete), got %+v", sr.Iteration, sr.McpCalls)
		}
		if sr.McpCalls[0].Tool != "list_artifacts" || sr.McpCalls[1].Tool != "node_complete" {
			t.Fatalf("design iter %d unexpected tools: %+v", sr.Iteration, sr.McpCalls)
		}
	}

	// Each gate visit carries its own resolution / feedback.
	var gateRuns []models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "gate").Order("iteration asc").Find(&gateRuns)
	if len(gateRuns) != 2 {
		t.Fatalf("expected 2 gate executions, got %d", len(gateRuns))
	}
	if fmt.Sprint(gateRuns[0].Outputs["action"]) != "revise" {
		t.Fatalf("gate iter1 action = %v", gateRuns[0].Outputs["action"])
	}
	form1, _ := gateRuns[0].Outputs["form"].(map[string]any)
	if fmt.Sprint(form1["comment"]) != "用直角" {
		t.Fatalf("gate iter1 feedback lost, got %v", form1["comment"])
	}
	if fmt.Sprint(gateRuns[1].Outputs["action"]) != "approve" {
		t.Fatalf("gate iter2 action = %v", gateRuns[1].Outputs["action"])
	}

	// History scoped to the design stage surfaces the gate's "用直角" feedback.
	out, err := host.RunHistory(run.ID, "design", false, false)
	if err != nil {
		t.Fatalf("RunHistory: %v", err)
	}
	if !strings.Contains(out, "用直角") {
		t.Fatalf("design history should recall the gate feedback, got:\n%s", out)
	}
}

// reactAccumGraph: input → clarify(react) → output.
func reactAccumGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "clarify", Type: "agent", Caps: capsClarify, Label: "澄清", Config: map[string]any{"prompt": "澄清"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "clarify"},
			{ID: "e2", Source: "clarify", Target: "output"},
		},
	}
}

// TestReactMultiTurnTraceAccumulates verifies a react node accumulates every
// turn's MCP calls on its single StateRun row (each reply flush), rather than
// only the first turn or the last one.
func TestReactMultiTurnTraceAccumulates(t *testing.T) {
	fp := &fakeProvider{reactPending: 3} // every reply asks one more question
	eng, db, _ := setupRecEngine(t, reactAccumGraph(), fp)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// The node parks without chatting; three replies each ask a question,
	// then the confirm concludes it.
	waitReactPause(t, db, run.ID, "clarify")
	for _, msg := range []string{"答复1", "答复2", "答复3"} {
		if err := eng.ReactReply(run.ID, "clarify", msg, nil, nil, false); err != nil {
			t.Fatalf("reply %q: %v", msg, err)
		}
		if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
			t.Fatalf("wait after %q: %v", msg, err)
		}
	}
	if err := eng.ReactReply(run.ID, "clarify", "确认", nil, nil, true); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var cr []models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").Find(&cr)
	if len(cr) != 1 {
		t.Fatalf("expected a single clarify execution, got %d", len(cr))
	}
	// 3 ask_question rounds + final node_complete.
	if len(cr[0].McpCalls) != 4 {
		t.Fatalf("react should accumulate 4 MCP calls across turns, got %d (%+v)", len(cr[0].McpCalls), cr[0].McpCalls)
	}
	for i, c := range cr[0].McpCalls {
		want := "ask_question"
		if i == 3 {
			want = "node_complete"
		}
		if c.Tool != want {
			t.Fatalf("call %d: unexpected tool %q, want %q", i, c.Tool, want)
		}
	}
}
