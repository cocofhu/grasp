package engine

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func researchEarlyFailGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "agent", Caps: capsResearch, Label: "代码调研", Config: map[string]any{"prompt": "调研"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "research"},
			{ID: "e2", Source: "research", Target: "output"},
		},
	}
}

// TestResearchEarlyFailureChannelsNonEmpty locks the clarified acceptance:
// first-node (research) Agent/sandbox-style failure must expose a non-empty
// reason via run_error.json and AggregateRunFailure (DTO/API).
func TestResearchEarlyFailureChannelsNonEmpty(t *testing.T) {
	eng, db, fp := setupEngineGraphP(t, researchEarlyFailGraph())
	fp.mu.Lock()
	fp.failLeft = map[string]int{"research": 1}
	fp.reason = "sandbox setup failed: create sandbox: timeout after retries"
	fp.mu.Unlock()

	run, err := eng.StartRun("wf", map[string]any{}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")
	waitNodeStatus(t, db, run.ID, "research", "failed")

	// (a) Aggregated Run-level reason (runDetailDTO / V1GetRun source)
	rs := services.NewRunService(db)
	info := rs.AggregateRunFailure(run.ID)
	if info.DisplayReason() == "" {
		t.Fatal("AggregateRunFailure display reason empty")
	}
	if info.FailedNode != "research" {
		t.Fatalf("failedNode=%q", info.FailedNode)
	}
	if !strings.Contains(info.Reason, "sandbox setup failed") {
		t.Fatalf("reason=%q", info.Reason)
	}
	// No archived sandbox logs in the fake provider path.
	if !info.NoSandboxLog {
		t.Fatal("expected noSandboxLog for fake early failure")
	}
	if !strings.Contains(info.DisplayReason(), services.NoSandboxLogMarker) {
		t.Fatalf("display missing no-log marker: %q", info.DisplayReason())
	}

	// (b) run_error.json product
	arts := services.NewArtifactService(db)
	body := waitRunErrorArtifact(t, db, run.ID)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("parse run_error.json: %v\n%s", err, body)
	}
	reason, _ := parsed["reason"].(string)
	if reason == "" {
		t.Fatalf("run_error.json reason empty: %s", body)
	}
	if parsed["failedNode"] != "research" {
		t.Fatalf("run_error failedNode=%v", parsed["failedNode"])
	}
	// Must not synthesize node_complete.json for infrastructure failure.
	if _, has := arts.Get(run.ID, "node_complete.json"); has {
		t.Fatal("must not synthesize node_complete.json on infra failure")
	}
}

// TestResearchEarlyFailureNoSandboxLogBoundary ensures missing docker logs never
// blank the outward reason and always mark 「无可用沙箱日志」.
func TestResearchEarlyFailureNoSandboxLogBoundary(t *testing.T) {
	eng, db, fp := setupEngineGraphP(t, researchEarlyFailGraph())
	fp.mu.Lock()
	fp.failLeft = map[string]int{"research": 1}
	fp.reason = "Agent 启动失败: exit code 1"
	fp.mu.Unlock()

	run, err := eng.StartRun("wf", map[string]any{}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	info := services.NewRunService(db).AggregateRunFailure(run.ID)
	if info.DisplayReason() == "" {
		t.Fatal("reason empty")
	}
	if !info.NoSandboxLog {
		t.Fatal("expected NoSandboxLog")
	}
	if !strings.Contains(info.DisplayReason(), services.NoSandboxLogMarker) {
		t.Fatalf("display=%q", info.DisplayReason())
	}
	body := waitRunErrorArtifact(t, db, run.ID)
	if !strings.Contains(body, services.NoSandboxLogMarker) && !strings.Contains(body, `"noSandboxLog": true`) {
		t.Fatalf("run_error.json missing no-log signal: %s", body)
	}
}

// TestSuccessfulRunSkipsRunErrorArtifact keeps the happy path free of
// misleading failure products / fields.
func TestSuccessfulRunSkipsRunErrorArtifact(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "agent", Caps: capsResearchAuto, Config: map[string]any{"prompt": "调研"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "research"},
			{ID: "e2", Source: "research", Target: "output"},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", map[string]any{}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	if _, ok := services.NewArtifactService(db).Get(run.ID, services.RunErrorArtifactName); ok {
		t.Fatal("completed run must not write run_error.json")
	}
	info := services.NewRunService(db).AggregateRunFailure(run.ID)
	// Aggregate may still invent a fallback if called wrongly; DTO only lifts
	// on status==failed. Ensure the completed run has no failed StateRun.error.
	var failed int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND status = ?", run.ID, "failed").Count(&failed)
	if failed != 0 {
		t.Fatalf("unexpected failed state runs: %d (info=%+v)", failed, info)
	}
}

// TestFailRunEarlyExitPersistsReason covers loadCtx-style early finish via
// failRun so aggregation is never an empty string.
func TestFailRunEarlyExitPersistsReason(t *testing.T) {
	eng, db := setupEngineGraph(t, researchEarlyFailGraph())
	stubID := "run-early-fail"
	db.Create(&models.Run{ID: stubID, WorkflowID: "wf", Status: "running", Graph: researchEarlyFailGraph()})
	eng.failRun(stubID, "加载运行上下文失败: simulated")
	waitRunStatus(t, db, stubID, "failed")

	info := services.NewRunService(db).AggregateRunFailure(stubID)
	if !strings.Contains(info.DisplayReason(), "加载运行上下文失败") {
		t.Fatalf("display=%q", info.DisplayReason())
	}
	waitRunErrorArtifact(t, db, stubID)
}

type recordingFailureAggregator struct {
	inner RunFailureAggregator
	mu    sync.Mutex
	calls []string
}

func (a *recordingFailureAggregator) AggregateRunFailure(runID string, optDegradeNote ...string) services.RunFailureInfo {
	a.mu.Lock()
	a.calls = append(a.calls, runID)
	a.mu.Unlock()
	info := a.inner.AggregateRunFailure(runID, optDegradeNote...)
	info.Reason = "injected: " + info.Reason
	return info
}

// TestRunErrorArtifactUsesInjectedAggregator locks that run_error.json comes
// from the aggregator wired at the composition root, not one the engine builds.
func TestRunErrorArtifactUsesInjectedAggregator(t *testing.T) {
	eng, db, fp := setupEngineGraphP(t, researchEarlyFailGraph())
	agg := &recordingFailureAggregator{inner: services.NewRunService(db)}
	eng.SetRunFailureAggregator(agg)
	fp.mu.Lock()
	fp.failLeft = map[string]int{"research": 1}
	fp.reason = "sandbox setup failed"
	fp.mu.Unlock()

	run, err := eng.StartRun("wf", map[string]any{}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")
	body := waitRunErrorArtifact(t, db, run.ID)
	if !strings.Contains(body, "injected: ") {
		t.Fatalf("run_error.json not from injected aggregator: %s", body)
	}
	agg.mu.Lock()
	defer agg.mu.Unlock()
	if len(agg.calls) != 1 || agg.calls[0] != run.ID {
		t.Fatalf("aggregator calls=%v", agg.calls)
	}
}
