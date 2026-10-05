package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

// waitPollTimeout bounds the polling helpers below. The engine advances runs on
// background workers, so completion lags the triggering call; under loaded CI
// runners this tail can be long. Keep it generous to avoid flaky timeouts —
// tests that genuinely stall still fail, just later.
const waitPollTimeout = 45 * time.Second

func setupEngine(t *testing.T) (*Engine, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.OpenSQLiteTest(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Self-contained: insert the test workflow directly (no seed dependency)
	// and drive it with a deterministic test-double provider (no Docker).
	wf := models.WorkflowDef{
		ID: "clarify-to-design", Name: "clarify-to-design", Status: "published", Version: 1,
		Graph: testClarifyGraph(),
	}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: wf.Graph}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	provider := &fakeProvider{host: host}
	eng := New(db, provider, host, arts, 5)
	eng.SetBlobStore(blob.NewMemory())
	cleanupEngineDB(t, eng, db)
	return eng, db
}

func cleanupEngineDB(t *testing.T, eng *Engine, db *gorm.DB) {
	t.Helper()
	t.Cleanup(func() {
		eng.Close()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

// testClarifyGraph is the FSM exercised by the engine unit test: react
// clarify → agent design (produces) → react review (produces) → human gate →
// agent plan (produces).
func testClarifyGraph() models.Graph {
	return models.Graph{
		Variables: []models.Variable{
			{Name: "idea", Type: "paragraph", Ask: true, Required: true, Editable: true},
			{Name: "design_round", Type: "number", Value: 0},
		},
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "clarify", Type: "agent", Caps: capsClarify, Label: "需求澄清", Config: map[string]any{"agent_profile": "pm-agent", "max_rounds": 3, "prompt": "澄清:{{vars.idea}}"}},
			{ID: "design", Type: "agent", Caps: capsPlain, Label: "设计", Checkpoint: true, Config: map[string]any{"agent_profile": "design-agent", "prompt": "设计", "produces": "design.md"}},
			{ID: "review_design", Type: "agent", Caps: capsCodeReview, Label: "设计复核", Config: map[string]any{"agent_profile": "analyst-agent", "prompt": "复核"}},
			{ID: "approve", Type: "human_gate", Label: "设计评审", Config: map[string]any{"title": "设计评审", "actions": []any{map[string]any{"id": "approve", "label": "批准"}, map[string]any{"id": "revise", "label": "退回"}}}},
			{ID: "plan", Type: "agent", Caps: capsPlain, Label: "方案", Config: map[string]any{"agent_profile": "plan-agent", "prompt": "方案", "produces": "plan.md"}},
			{ID: "output", Type: "output", Label: "输出"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "clarify"},
			{ID: "e2", Source: "clarify", Target: "design"},
			{ID: "e3", Source: "design", Target: "review_design"},
			{ID: "e4", Source: "review_design", Target: "approve", SourceHandle: "pass", Kind: models.EdgeSuccess},
			{ID: "e6", Source: "approve", Target: "plan", SourceHandle: "approve", Kind: models.EdgeSuccess},
			{ID: "e7", Source: "plan", Target: "output"},
		},
	}
}

func waitRunStatus(t *testing.T, db *gorm.DB, runID, status string) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		var r models.Run
		db.First(&r, "id = ?", runID)
		if r.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	var r models.Run
	db.First(&r, "id = ?", runID)
	t.Fatalf("run %s did not reach %q (got %q)", runID, status, r.Status)
}

// waitRunErrorArtifact polls until finish() has written run_error.json.
// Run.status can flip to failed a few statements before the artifact lands.
func waitRunErrorArtifact(t *testing.T, db *gorm.DB, runID string) string {
	t.Helper()
	arts := services.NewArtifactService(db)
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		if body, ok := arts.Get(runID, services.RunErrorArtifactName); ok && body != "" {
			return body
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected run_error.json artifact")
	return ""
}

func waitReactPause(t *testing.T, db *gorm.DB, runID, nodeID string) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		var conv models.ReactConversation
		if err := db.Where("run_id = ? AND node_id = ? AND done = ?", runID, nodeID, false).First(&conv).Error; err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("react node %s did not open a conversation", nodeID)
}

func waitGatePending(t *testing.T, db *gorm.DB, runID, nodeID string) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		var g models.Gate
		if err := db.Where("run_id = ? AND node_id = ? AND resolved = ?", runID, nodeID, false).First(&g).Error; err == nil {
			// Gate row can appear before run status flips to waiting_human;
			// loadPendingGate / ListGatePrimaryProducts require the latter.
			var r models.Run
			if err := db.First(&r, "id = ?", runID).Error; err == nil && r.Status == "waiting_human" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("gate %s not pending", nodeID)
}

// TestClarifyToDesignRun drives a full FSM run through react clarification, an
// autonomous agent, a react design review, a human gate, and completion,
// asserting the produces contract artifacts were captured via the MCP.
func TestClarifyToDesignRun(t *testing.T) {
	eng, db := setupEngine(t)

	run, err := eng.StartRun("clarify-to-design", map[string]any{"idea": "新增邮箱验证码登录"}, "test")
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	// react clarify pauses for human input.
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.ReactReply(run.ID, "clarify", "邮箱验证码,5 分钟有效", nil, nil, false); err != nil {
		t.Fatalf("clarify reply: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait clarify turn: %v", err)
	}
	if err := eng.ReactReply(run.ID, "clarify", "就这样", nil, nil, true); err != nil {
		t.Fatalf("clarify confirm: %v", err)
	}

	// design (agent) and review_design (review) run automatically, then the
	// human gate pauses.
	waitGatePending(t, db, run.ID, "approve")
	if err := eng.ResumeGate(run.ID, "approve", "approve", map[string]any{"comment": "ok"}); err != nil {
		t.Fatalf("resume gate: %v", err)
	}

	waitRunStatus(t, db, run.ID, "completed")

	// Verify produces-contract artifacts landed in the platform store.
	var arts []models.Artifact
	db.Where("run_id = ?", run.ID).Find(&arts)
	got := map[string]bool{}
	for _, a := range arts {
		got[a.Name] = true
	}
	for _, want := range []string{"clarified_requirement.json", "design.md", "review.json", "plan.md"} {
		if !got[want] {
			t.Errorf("missing artifact %q (have %v)", want, keys(got))
		}
	}
}

// setupEngineGraph builds an engine driving a caller-provided graph under
// workflow id "wf", using the deterministic fake provider.
func setupEngineGraph(t *testing.T, g models.Graph) (*Engine, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.OpenSQLiteTest(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	wf := models.WorkflowDef{ID: "wf", Name: "wf", Status: "published", Version: 1, Graph: g}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: g}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	eng := New(db, &fakeProvider{host: host}, host, arts, 5)
	eng.SetBlobStore(blob.NewMemory())
	cleanupEngineDB(t, eng, db)
	return eng, db
}

func proposalGraph() models.Graph {
	return models.Graph{
		Variables: []models.Variable{{Name: "auto_confirm", Type: "bool", Value: true}},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "propose", Type: "agent", Caps: capsProposalAuto, Config: map[string]any{"agent_profile": "arch", "prompt": "方案"}},
			{ID: "select", Type: "proposal_select", Config: map[string]any{"auto_var": "auto_confirm", "output_var": "selected_proposal"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "propose"},
			{ID: "e2", Source: "propose", Target: "select"},
			{ID: "e3", Source: "select", Target: "output"},
		},
	}
}

// TestProposalSelectAuto: auto_confirm=true selects the recommended proposal
// (p2) without pausing, writes proposal.json, sets the output variable, and
// retires the upstream ProposalAgent park session (same as ResumeGate).
func TestProposalSelectAuto(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, proposalGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var arts []models.Artifact
	db.Where("run_id = ? AND name = ?", run.ID, "proposal.json").Find(&arts)
	if len(arts) == 0 {
		t.Fatalf("proposal.json not written")
	}
	var v models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "selected_proposal").First(&v).Error; err != nil {
		t.Fatalf("selected_proposal var missing: %v", err)
	}
	if v.Value != "p2" {
		t.Fatalf("expected recommended p2 selected, got %v", v.Value)
	}
	if !provider.retired[provider.parkKey(run.ID, "propose")] {
		t.Fatalf("expected upstream propose session retired after auto_confirm")
	}
}

// TestProposalSelectManual: auto_confirm=false pauses on a gate; resuming with
// a chosen proposal id finalizes it.
func TestProposalSelectManual(t *testing.T) {
	g := proposalGraph()
	g.Variables[0].Value = false // auto_confirm=false → manual selection
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "select")
	if err := eng.ResumeGate(run.ID, "select", "p1", nil); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var v models.RunVariable
	db.Where("run_id = ? AND name = ?", run.ID, "selected_proposal").First(&v)
	if v.Value != "p1" {
		t.Fatalf("expected manual choice p1, got %v", v.Value)
	}
}

// TestProposalSelectManualSingleCandidate: auto_confirm=false but only one
// valid candidate — finalize without pausing (no pseudo-choice wait).
func TestProposalSelectManualSingleCandidate(t *testing.T) {
	g := proposalGraph()
	g.Variables[0].Value = false
	eng, db, provider := setupEngineGraphP(t, g)
	provider.structuredBodies = map[string]string{
		"propose": `{"context":"方向已明确","proposals":[{"id":"p1","title":"唯一方案","recommended":true}]}`,
	}
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var gates []models.Gate
	db.Where("run_id = ? AND node_id = ?", run.ID, "select").Find(&gates)
	if len(gates) != 0 {
		t.Fatalf("single-candidate manual mode must not create a pending gate, got %d", len(gates))
	}
	var arts []models.Artifact
	db.Where("run_id = ? AND name = ?", run.ID, "proposal.json").Find(&arts)
	if len(arts) == 0 {
		t.Fatalf("proposal.json not written")
	}
	var v models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "selected_proposal").First(&v).Error; err != nil {
		t.Fatalf("selected_proposal var missing: %v", err)
	}
	if v.Value != "p1" {
		t.Fatalf("expected sole candidate p1, got %v", v.Value)
	}
}

// TestGateActionHandles: a human_gate leaves through the outlet named by the
// chosen action and assigns the action to the global var.
func TestGateActionHandles(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{"title": "选路", "output_var": "action",
				"actions": []any{
					map[string]any{"id": "toB", "label": "去B"},
					map[string]any{"id": "toA", "label": "去A"},
				}}},
			{ID: "a", Type: "output", Config: map[string]any{"result": "A"}},
			{ID: "b", Type: "output", Config: map[string]any{"result": "B"}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "a", SourceHandle: "toA", Kind: models.EdgeSuccess},
			{ID: "e3", Source: "gate", Target: "b", SourceHandle: "toB", Kind: models.EdgeSuccess},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitGatePending(t, db, run.ID, "gate")
	if err := eng.ResumeGate(run.ID, "gate", "toB", nil); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// Node b should have been reached (via its outlet), node a should not.
	var srB models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "b").First(&srB).Error; err != nil {
		t.Fatalf("outlet routing did not reach node b: %v", err)
	}
	var srA models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "a").First(&srA).Error; err == nil {
		t.Fatalf("node a should not have executed when toB was chosen")
	}
	var v models.RunVariable
	db.Where("run_id = ? AND name = ?", run.ID, "action").First(&v)
	if v.Value != "toB" {
		t.Fatalf("action var not assigned, got %v", v.Value)
	}
}

// waitGateIteration waits until an unresolved gate exists for the node at the
// given per-node execution index (the loop-back re-opens the gate at iter+1).
func waitGateIteration(t *testing.T, db *gorm.DB, runID, nodeID string, iteration int) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		var g models.Gate
		if err := db.Where("run_id = ? AND node_id = ? AND iteration = ? AND resolved = ?", runID, nodeID, iteration, false).
			First(&g).Error; err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("gate %s did not re-open at iteration %d", nodeID, iteration)
}

// TestGateLoopBackReApproval is the regression guard for the "执行完成后回到门禁
// 无法二次审批" bug: a run that loops back onto a human_gate (via a revise
// action) must re-open the gate for a fresh decision every visit, and each
// visit's execution must be recorded separately (traceable), not overwritten.
func TestGateLoopBackReApproval(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain, Config: map[string]any{"agent_profile": "x", "prompt": "干活", "produces": "work.md"}},
			{ID: "gate", Type: "human_gate", Config: map[string]any{"title": "评审", "output_var": "action",
				"actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
					map[string]any{"id": "revise", "label": "退回修改"},
				}}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "work"},
			{ID: "e2", Source: "work", Target: "gate"},
			{ID: "e3", Source: "gate", Target: "output", SourceHandle: "approve", Kind: models.EdgeSuccess},
			{ID: "e4", Source: "gate", Target: "work", SourceHandle: "revise"},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// First visit: gate pauses at iteration 1; revise loops back to work.
	waitGateIteration(t, db, run.ID, "gate", 1)
	if err := eng.ResumeGate(run.ID, "gate", "revise", nil); err != nil {
		t.Fatalf("first resume (revise): %v", err)
	}

	// Second visit: the bug was that the gate stayed resolved and passed
	// through, so the run finished without re-approval. It must re-open here.
	waitGateIteration(t, db, run.ID, "gate", 2)
	if err := eng.ResumeGate(run.ID, "gate", "approve", nil); err != nil {
		t.Fatalf("second resume (approve) — re-approval broken: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// Both executions of each looped node are preserved (traceability): the
	// work node and the gate node each have two separate StateRun rows.
	assertExecutionCount := func(nodeID string, want int) {
		var n int64
		db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, nodeID).Count(&n)
		if int(n) != want {
			t.Fatalf("node %s: expected %d execution records, got %d", nodeID, want, n)
		}
	}
	assertExecutionCount("work", 2)
	assertExecutionCount("gate", 2)

	// Two distinct gate decisions were recorded (revise then approve).
	var gates int64
	db.Model(&models.Gate{}).Where("run_id = ? AND node_id = ? AND resolved = ?", run.ID, "gate", true).Count(&gates)
	if gates != 2 {
		t.Fatalf("expected 2 resolved gate rows, got %d", gates)
	}
}

// TestCancelAtGateThenResumeNoDuplicateApproval is the regression guard for the
// "审批中取消工作流后从失败处继续,原审批还在,完成后出现两个审批" bug: cancelling
// while paused at a human gate must supersede the pending gate and move the paused
// node off waiting_human, so resuming from that point opens exactly ONE fresh
// approval — not a second one alongside the stale, never-resolved original.
func TestCancelAtGateThenResumeNoDuplicateApproval(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain, Config: map[string]any{"agent_profile": "x", "prompt": "干活", "produces": "work.md"}},
			{ID: "gate", Type: "human_gate", Config: map[string]any{"title": "评审", "output_var": "action",
				"actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
				}}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "work"},
			{ID: "e2", Source: "work", Target: "gate"},
			{ID: "e3", Source: "gate", Target: "output", SourceHandle: "approve", Kind: models.EdgeSuccess},
			{ID: "e4", Source: "gate", Target: "work", SourceHandle: "revise"},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Pause at the gate (iteration 1, waiting_human).
	waitGateIteration(t, db, run.ID, "gate", 1)
	waitRunStatus(t, db, run.ID, "waiting_human")

	// Cancel while paused at the gate.
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")

	// Cancel must supersede the pending gate and take the paused node off
	// waiting_human — otherwise the original approval lingers as actionable.
	var unresolved int64
	db.Model(&models.Gate{}).Where("run_id = ? AND resolved = ?", run.ID, false).Count(&unresolved)
	if unresolved != 0 {
		t.Fatalf("cancel left %d unresolved gate(s); expected 0", unresolved)
	}
	var stuck int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND status = ?", run.ID, "waiting_human").Count(&stuck)
	if stuck != 0 {
		t.Fatalf("cancel left %d waiting_human state_run(s); expected 0", stuck)
	}

	// Resume from the failure position (auto-picks the cancelled gate node).
	if err := eng.ResumeFrom(run.ID, ""); err != nil {
		t.Fatalf("resume: %v", err)
	}

	// The gate re-opens at iteration 2 with exactly ONE pending approval — the
	// superseded iteration-1 gate stays resolved and does not double up.
	waitGateIteration(t, db, run.ID, "gate", 2)
	waitRunStatus(t, db, run.ID, "waiting_human")
	db.Model(&models.Gate{}).Where("run_id = ? AND resolved = ?", run.ID, false).Count(&unresolved)
	if unresolved != 1 {
		t.Fatalf("after resume expected exactly 1 pending gate, got %d", unresolved)
	}

	if err := eng.ResumeGate(run.ID, "gate", "approve", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// No phantom pending gate remains after completion.
	db.Model(&models.Gate{}).Where("run_id = ? AND resolved = ?", run.ID, false).Count(&unresolved)
	if unresolved != 0 {
		t.Fatalf("after completion %d unresolved gate(s) linger; expected 0", unresolved)
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
