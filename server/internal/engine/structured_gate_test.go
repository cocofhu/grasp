package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

func waitNodeStatus(t *testing.T, db *gorm.DB, runID, nodeID, status string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var sr models.StateRun
		if err := db.Where("run_id = ? AND node_id = ?", runID, nodeID).
			Order("iteration desc, id desc").First(&sr).Error; err == nil && sr.Status == status {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("node %s did not reach status %q", nodeID, status)
}

func assertRunVar(t *testing.T, db *gorm.DB, runID, name, want string) {
	t.Helper()
	var v models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", runID, name).First(&v).Error; err != nil {
		t.Fatalf("var %q missing: %v", name, err)
	}
	if v.Value != want {
		t.Fatalf("var %q = %q, want %q", name, v.Value, want)
	}
}

// verdictEdges wires input -> node and the node's pass/fail outlets.
func verdictEdges(node, pass, fail string) []models.Edge {
	return []models.Edge{
		{ID: "e1", Source: "input", Target: node},
		{ID: "pass", Source: node, Target: pass, SourceHandle: handlePass},
		{ID: "fail", Source: node, Target: fail, SourceHandle: handleFail},
	}
}

func assertNodeExecuted(t *testing.T, db *gorm.DB, runID, nodeID string, want bool) {
	t.Helper()
	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", runID, nodeID).Count(&n)
	if want && n == 0 {
		t.Fatalf("expected node %s to execute", nodeID)
	}
	if !want && n > 0 {
		t.Fatalf("expected node %s not to execute, got %d runs", nodeID, n)
	}
}

// TestStructuredGatePassGoto: a passing test verdict leaves through the pass outlet.
func TestStructuredGatePassGoto(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "test", Type: "agent", Caps: capsTest, Config: map[string]any{
				"agent_profile": "t", "prompt": "测试",
			}},
			{ID: "ok", Type: "output", Config: map[string]any{"result": "pass"}},
			{ID: "bad", Type: "output", Config: map[string]any{"result": "fail"}},
		},
		Edges: verdictEdges("test", "ok", "bad"),
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	assertNodeExecuted(t, db, run.ID, "ok", true)
	assertNodeExecuted(t, db, run.ID, "bad", false)
	assertRunVar(t, db, run.ID, "reason", "验证全部通过")
}

// TestStructuredGateFailGoto: a failing verdict follows the fail outlet while the node stays failed.
func TestStructuredGateFailGoto(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "test", Type: "agent", Caps: capsTest, Config: map[string]any{
				"agent_profile": "t", "prompt": "测试",
			}},
			{ID: "ok", Type: "output", Config: map[string]any{"result": "pass"}},
			{ID: "bad", Type: "output", Config: map[string]any{"result": "fix"}},
		},
		Edges: verdictEdges("test", "ok", "bad"),
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.structuredBodies = map[string]string{
		"test": `{"summary":"bad","failed":1,"cases":[{"name":"x","status":"failed"}]}`,
	}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	waitNodeStatus(t, db, run.ID, "test", "failed")
	assertNodeExecuted(t, db, run.ID, "bad", true)
	assertNodeExecuted(t, db, run.ID, "ok", false)
	assertRunVar(t, db, run.ID, "reason", "测试未通过:1 个用例失败,需修复后重新测试")
	assertRunVar(t, db, run.ID, "last_error", "测试未通过:1 个用例失败,需修复后重新测试")
}

// TestReviewStructuredGateVerdicts exercises approve/reject routing and reason_var.
func TestReviewStructuredGateVerdicts(t *testing.T) {
	base := func() models.Graph {
		return models.Graph{
			Nodes: []models.Node{
				{ID: "input", Type: "input"},
				{ID: "review", Type: "agent", Caps: capsCodeReview, Config: map[string]any{
					"agent_profile": "v", "prompt": "评审",
				}},
				{ID: "ok", Type: "output", Config: map[string]any{"result": "ok"}},
				{ID: "bad", Type: "output", Config: map[string]any{"result": "bad"}},
			},
			Edges: verdictEdges("review", "ok", "bad"),
		}
	}

	t.Run("approve", func(t *testing.T) {
		eng, db, p := setupEngineGraphP(t, base())
		p.structuredBodies = map[string]string{
			"review": `{"summary":"ok","verdict":"approve"}`,
		}
		run, _ := eng.StartRun("wf", nil, "test")
		waitRunStatus(t, db, run.ID, "completed")
		assertNodeExecuted(t, db, run.ID, "ok", true)
		assertRunVar(t, db, run.ID, "reason", "验证全部通过")
	})

	t.Run("reject", func(t *testing.T) {
		eng, db, p := setupEngineGraphP(t, base())
		p.structuredBodies = map[string]string{
			"review": `{"summary":"no","verdict":"reject"}`,
		}
		run, _ := eng.StartRun("wf", nil, "test")
		waitRunStatus(t, db, run.ID, "completed")
		waitNodeStatus(t, db, run.ID, "review", "failed")
		assertNodeExecuted(t, db, run.ID, "bad", true)
		assertRunVar(t, db, run.ID, "reason", "评审结论为 reject:方案/实现被否决,需整改后重新评审")
		assertRunVar(t, db, run.ID, "last_error", "评审结论为 reject:方案/实现被否决,需整改后重新评审")
	})
}

// TestStructuredGateMalformed fails closed for bad test_result.json.
func TestStructuredGateMalformed(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "test", Type: "agent", Caps: capsTest, Config: map[string]any{
				"agent_profile": "t", "prompt": "测试",
			}},
			{ID: "ok", Type: "output"},
			{ID: "bad", Type: "output"},
		},
		Edges: verdictEdges("test", "ok", "bad"),
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.structuredBodies = map[string]string{"test": `{bad`}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	assertNodeExecuted(t, db, run.ID, "bad", true)
}

// TestStructuredGateRetrySnapshotsScreenshots: a test node that fails its gate,
// rolls back and re-runs, then passes must snapshot EACH attempt's own
// test_result.json — screenshots included — into that iteration's outputs. The
// run-scoped artifact store keeps only the latest test_result.json (same name,
// overwritten each attempt), so a retry that relied on the store would show the
// last attempt's screenshots for every historical tab. Persisting per-iteration
// (outputs.test_result_json) is what makes every retry independently reviewable,
// exactly like the first attempt.
func TestStructuredGateRetrySnapshotsScreenshots(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "test", Type: "agent", Caps: capsTest, Checkpoint: true, Config: map[string]any{
				"agent_profile": "t", "prompt": "测试",
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "test"},
			{ID: "e2", Source: "test", Target: "output", SourceHandle: handlePass, Kind: models.EdgeSuccess},
			{ID: "erb", Source: "test", Target: "test", SourceHandle: handleFail, Kind: models.EdgeRollback, MaxAttempts: 3},
		},
	}
	eng, db, p := setupEngineGraphP(t, g)
	// Attempt 1 fails the gate (a failed case) and carries screenshot SHOTA;
	// attempt 2 passes and carries a different screenshot SHOTB.
	p.structuredBodySeq = map[string][]string{
		"test": {
			`{"summary":"a1","failed":1,"cases":[{"name":"x","status":"failed"}],"screenshots":[{"artifact":"SHOTA.png","mimeType":"image/png","caption":"first"}]}`,
			`{"summary":"a2","passed":1,"screenshots":[{"artifact":"SHOTB.png","mimeType":"image/png","caption":"second"}]}`,
		},
	}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")

	var rows []models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "test").
		Order("iteration asc").Find(&rows).Error; err != nil {
		t.Fatalf("load test state runs: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("test execution rows = %d, want 2 (failed attempt + retried pass)", len(rows))
	}
	snap := func(sr models.StateRun) string {
		s, _ := sr.Outputs["test_result_json"].(string)
		return s
	}
	first, second := snap(rows[0]), snap(rows[1])
	if !strings.Contains(first, `"artifact":"SHOTA.png"`) {
		t.Errorf("first attempt snapshot missing its own screenshot ref (SHOTA): %q", first)
	}
	if strings.Contains(first, "SHOTB") {
		t.Errorf("first attempt snapshot leaked the retry's screenshot ref (SHOTB): %q", first)
	}
	if !strings.Contains(second, `"artifact":"SHOTB.png"`) {
		t.Errorf("retry snapshot missing its own screenshot ref (SHOTB): %q", second)
	}
}

func TestStructuredGateArtifactNames(t *testing.T) {
	if mcp.TestResultArtifactName == "" || mcp.ReviewArtifactName == "" {
		t.Fatal("artifact names should be set")
	}
}

// TestStructuredGateSkippedOnlyPass: skipped-only results pass the test verdict.
func TestStructuredGateSkippedOnlyPass(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "test", Type: "agent", Caps: capsTest, Config: map[string]any{
				"agent_profile": "t", "prompt": "测试",
			}},
			{ID: "ok", Type: "output", Config: map[string]any{"result": "pass"}},
			{ID: "bad", Type: "output", Config: map[string]any{"result": "fail"}},
		},
		Edges: verdictEdges("test", "ok", "bad"),
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.structuredBodies = map[string]string{
		"test": `{"summary":"skipped only","skipped":2,"cases":[{"name":"a","status":"skipped"},{"name":"b","status":"skipped"}]}`,
	}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	waitNodeStatus(t, db, run.ID, "test", "completed")
	assertNodeExecuted(t, db, run.ID, "ok", true)
	assertNodeExecuted(t, db, run.ID, "bad", false)
	assertRunVar(t, db, run.ID, "reason", "验证全部通过")
}

func planCoverageGateGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "plan", Type: "agent", Caps: capsPlan, Config: map[string]any{"agent_profile": "p", "prompt": "计划"}},
			{ID: "test", Type: "agent", Caps: capsTest, Config: map[string]any{
				"agent_profile": "t", "prompt": "测试",
			}},
			{ID: "ok", Type: "output", Config: map[string]any{"result": "pass"}},
			{ID: "bad", Type: "output", Config: map[string]any{"result": "fix"}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "plan"},
			{ID: "e2", Source: "plan", Target: "test", Kind: models.EdgeSuccess},
			{ID: "pass", Source: "test", Target: "ok", SourceHandle: handlePass},
			{ID: "fail", Source: "test", Target: "bad", SourceHandle: handleFail},
		},
	}
}

func TestStructuredGatePlanCoverageMissingFailGoto(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, planCoverageGateGraph())
	p.structuredBodies = map[string]string{
		"plan": `{"goals":[{"id":"g1","title":"A","subgoals":[{"id":"g1.1","title":"x"},{"id":"g1.2","title":"y"}]}]}`,
		"test": `{"summary":"missing coverage","cases":[{"name":"a","status":"passed"}]}`,
	}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	waitNodeStatus(t, db, run.ID, "test", "failed")
	assertNodeExecuted(t, db, run.ID, "bad", true)
	assertNodeExecuted(t, db, run.ID, "ok", false)
	assertRunVar(t, db, run.ID, "reason", "计划贴合度校验失败:缺少 plan_coverage(有计划叶子时必填)")
}

func TestStructuredGatePlanCoveragePassGoto(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, planCoverageGateGraph())
	p.structuredBodies = map[string]string{
		"plan": `{"goals":[{"id":"g1","title":"A","subgoals":[{"id":"g1.1","title":"x"},{"id":"g1.2","title":"y"}]}]}`,
		"test": `{"summary":"covered","cases":[{"name":"a","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"implemented x"},
			{"plan_id":"g1.2","passed":true,"evidence":"implemented y"}
		]}`,
	}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	waitNodeStatus(t, db, run.ID, "test", "completed")
	assertNodeExecuted(t, db, run.ID, "ok", true)
	assertNodeExecuted(t, db, run.ID, "bad", false)
	assertRunVar(t, db, run.ID, "reason", "验证全部通过")
}

func TestStructuredGatePlanCoverageNoPlanFailOpen(t *testing.T) {
	// No plan node: empty plan.json → coverage not required.
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "test", Type: "agent", Caps: capsTest, Config: map[string]any{
				"agent_profile": "t", "prompt": "测试",
			}},
			{ID: "ok", Type: "output", Config: map[string]any{"result": "pass"}},
			{ID: "bad", Type: "output", Config: map[string]any{"result": "fix"}},
		},
		Edges: verdictEdges("test", "ok", "bad"),
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.structuredBodies = map[string]string{
		"test": `{"summary":"no plan","cases":[{"name":"a","status":"passed"}]}`,
	}
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	assertNodeExecuted(t, db, run.ID, "ok", true)
	assertNodeExecuted(t, db, run.ID, "bad", false)
}
