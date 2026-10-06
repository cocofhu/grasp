package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

// setupEngineGraphP is setupEngineGraph but also returns the fake provider so a
// test can script forced failures.
func setupEngineGraphP(t *testing.T, g models.Graph) (*Engine, *gorm.DB, *fakeProvider) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.OpenSQLiteTest(dbPath)
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
	p := &fakeProvider{host: host}
	eng := New(db, p, host, arts, 5)
	eng.SetBlobStore(blob.NewMemory())
	cleanupEngineDB(t, eng, db)
	return eng, db, p
}

func TestBroker(t *testing.T) {
	b := NewBroker()
	ch, unsub := b.Subscribe("r")
	b.Publish("r", []byte("hi"))
	select {
	case msg := <-ch:
		if string(msg) != "hi" {
			t.Errorf("got %q", msg)
		}
	default:
		t.Fatal("expected a delivered message")
	}
	b.Publish("other", []byte("x")) // no subscribers, must not panic
	unsub()
}

func TestJSONMsgAndCancel(t *testing.T) {
	msg := jsonMsg("state", "r1", "n1")
	if string(msg) != `{"type":"state","runId":"r1","nodeId":"n1"}` {
		t.Fatalf("jsonMsg = %q", msg)
	}
	// Fast graphs finish instantly under the fake provider, racing Cancel
	// into "already finished". Block on an agent node so Cancel is deterministic.
	eng, db, _ := setupBlockingEngine(t, slowGraph(), 5)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitRunStatus(t, db, run.ID, "running")
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	// AbortRun unblocks the provider; wait for the zombie driver to exit before
	// t.Cleanup closes the DB (otherwise appendTrace races on a closed sqlite).
	waitAdmissionUntil(t, func() bool { return !eng.isExecuting(run.ID) }, 3*time.Second)
	var r models.Run
	db.First(&r, "id = ?", run.ID)
	if r.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", r.Status)
	}
	if err := eng.Cancel("missing-run"); err == nil {
		t.Error("expected error cancelling missing run")
	}
	// Second Cancel on an already-cancelled run is an idempotent heal (clears
	// sticky running StateRuns / zombie exec slots), not an error.
	if err := eng.Cancel(run.ID); err != nil {
		t.Errorf("re-cancel heal: %v", err)
	}
}

// TestRollbackOnFailureCarriesLastError: a node fails once; a rollback edge back
// to the checkpoint retries it, carrying last_error into the retried node's
// vars. The second attempt succeeds and the run completes.
func TestRollbackOnFailureCarriesLastError(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "risky", Type: "agent", Caps: capsPlain, Checkpoint: true, Config: map[string]any{"prompt": "干活:{{vars.last_error}}", "produces": "out.md"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "risky"},
			{ID: "e2", Source: "risky", Target: "output", Kind: models.EdgeSuccess},
			{ID: "erb", Source: "risky", Target: "risky", Kind: models.EdgeRollback, Carry: []string{"last_error"}, MaxAttempts: 3},
		},
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.failLeft = map[string]int{"risky": 1}
	p.reason = "boom-reason"

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// last_error must have been injected before the retry.
	if le := p.varsFor("risky")["last_error"]; le == "" || le == nil {
		t.Errorf("last_error not carried into retried node, got %v", le)
	}
	// Two execution rows: the failed attempt + the successful retry.
	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "risky").Count(&n)
	if n != 2 {
		t.Errorf("risky execution rows = %d, want 2", n)
	}
}

// TestFailureEdgeRoutes: a permanent failure with no rollback but a failure edge
// routes to the failure-handler node.
func TestFailureEdgeRoutes(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "risky", Type: "agent", Caps: capsPlain, Config: map[string]any{"prompt": "x", "produces": "out.md"}},
			{ID: "onfail", Type: "output", Config: map[string]any{"result": "handled"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "risky"},
			{ID: "e2", Source: "risky", Target: "output", Kind: models.EdgeSuccess},
			{ID: "ef", Source: "risky", Target: "onfail", Kind: models.EdgeFailure},
		},
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.failLeft = map[string]int{"risky": 99} // always fail
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "onfail").First(&sr).Error; err != nil {
		t.Fatalf("failure edge did not route to onfail: %v", err)
	}
}

// TestFailureNoTransition: a failure with no rollback/failure edge fails the run.
func TestFailureNoTransition(t *testing.T) {
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
}

// TestSetVarAndBranch: a set_var node computes a variable; a branch node routes
// on it; the target node runs.
func TestSetVarAndBranch(t *testing.T) {
	g := models.Graph{
		Variables: []models.Variable{{Name: "score", Type: "number", Value: float64(0)}},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "assign", Type: "set_var", Config: map[string]any{"assignments": []any{
				map[string]any{"var": "score", "expr": "7"},
			}}},
			{ID: "branch", Type: "branch", Config: map[string]any{"cases": []any{
				map[string]any{"when": "score > 5", "id": "high"},
				map[string]any{"when": "score <= 5", "id": "low"},
			}}},
			{ID: "high", Type: "output", Config: map[string]any{"result": "HIGH"}},
			{ID: "low", Type: "output", Config: map[string]any{"result": "LOW"}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "assign"},
			{ID: "e2", Source: "assign", Target: "branch"},
			{ID: "e3", Source: "branch", Target: "high", SourceHandle: "high"},
			{ID: "e4", Source: "branch", Target: "low", SourceHandle: "low"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "high").First(&models.StateRun{}).Error; err != nil {
		t.Fatalf("branch did not route to high: %v", err)
	}
	if db.Where("run_id = ? AND node_id = ?", run.ID, "low").First(&models.StateRun{}).Error == nil {
		t.Fatal("low branch should not have executed")
	}
}

// TestBranchNoMatch: a branch node with no matching case completes when no outlet matches.
func TestBranchNoMatch(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "branch", Type: "branch", Config: map[string]any{"cases": []any{
				map[string]any{"when": "false", "id": "never"},
			}}},
			{ID: "never", Type: "output"},
		},
		Edges: []models.Edge{{ID: "e1", Source: "input", Target: "branch"}},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	// No case matched and no outgoing edge => run completes at branch.
	waitRunStatus(t, db, run.ID, "completed")
}

// TestAutoCaptureDeliverable: an agent node with no produces auto-captures its
// primary output as <node>.md.
func TestAutoCaptureDeliverable(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain, Config: map[string]any{"prompt": "干活"}}, // no produces
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
	var a models.Artifact
	if err := db.Where("run_id = ? AND name = ?", run.ID, "work.md").First(&a).Error; err != nil {
		t.Fatalf("auto-captured deliverable work.md missing: %v", err)
	}
}

// TestGateFormValidation: a human_gate with a required form field rejects an
// empty submission and accepts a filled one, persisting the field as a var.
func TestGateFormValidation(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{"title": "评审",
				"actions": []any{map[string]any{"id": "approve", "label": "批准"}},
				"form":    []any{map[string]any{"key": "comment", "label": "意见", "required": true}},
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "output", Kind: models.EdgeSuccess},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitGatePending(t, db, run.ID, "gate")

	if err := eng.ResumeGate(run.ID, "gate", "approve", map[string]any{}); err == nil {
		t.Fatal("expected a validation error for the missing required field")
	}
	if err := eng.ResumeGate(run.ID, "gate", "approve", map[string]any{"comment": "看起来不错"}); err != nil {
		t.Fatalf("resume with filled form: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var v models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "comment").First(&v).Error; err != nil {
		t.Fatalf("form field not persisted as var: %v", err)
	}
}

// TestPlanAndImplementNodes: a plan node enforces the set_plan contract and an
// implement node exports its working branch to the global `branches` var.
// Neither node writes a fixed-name markdown companion.
func TestPlanAndImplementNodes(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "plan", Type: "agent", Caps: capsPlan, Config: map[string]any{"prompt": "计划"}},
			{ID: "impl", Type: "agent", Caps: capsImplement, Config: map[string]any{"prompt": "实现"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "plan"},
			{ID: "e2", Source: "plan", Target: "impl"},
			{ID: "e3", Source: "impl", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")

	if _, ok := services.NewArtifactService(db).Get(run.ID, mcp.PlanArtifactName); !ok {
		t.Error("plan.json not written by plan node")
	}
	if _, ok := services.NewArtifactService(db).Get(run.ID, mcp.ImplementationResultArtifactName); !ok {
		t.Error("implementation_result.json not written by implement node")
	}
	for _, name := range []string{"plan.md", "implementation_result.md"} {
		var c int64
		db.Model(&models.Artifact{}).Where("run_id = ? AND name = ?", run.ID, name).Count(&c)
		if c != 0 {
			t.Errorf("unexpected companion artifact %q", name)
		}
	}
	// f2: plan/implement still expose rendered Markdown + *_json in node outputs.
	for _, tc := range []struct {
		nodeID, outKey string
	}{
		{"plan", "plan"},
		{"impl", "implementation_result"},
	} {
		var sr models.StateRun
		if err := db.Where("run_id = ? AND node_id = ?", run.ID, tc.nodeID).
			Order("iteration desc").First(&sr).Error; err != nil {
			t.Fatalf("load %s state run: %v", tc.nodeID, err)
		}
		md, _ := sr.Outputs[tc.outKey].(string)
		if strings.TrimSpace(md) == "" {
			t.Errorf("%s outputs[%q] rendered markdown empty", tc.nodeID, tc.outKey)
		}
		raw, _ := sr.Outputs[tc.outKey+"_json"].(string)
		if strings.TrimSpace(raw) == "" {
			t.Errorf("%s outputs[%q] empty", tc.nodeID, tc.outKey+"_json")
		}
	}
	var v models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "branches").First(&v).Error; err != nil {
		t.Fatalf("implement node did not export branches var: %v", err)
	}
	if v.Value != `{"app":"feature/impl"}` {
		t.Errorf("branches var = %v, want {\"app\":\"feature/impl\"}", v.Value)
	}
}
