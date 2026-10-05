package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// autoRetryGraph is a minimal input → risky(agent) → output graph used by the
// auto-retry tests. The risky node is where the fake provider injects failures.
func autoRetryGraph() models.Graph {
	return models.Graph{
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
}

// researchContractGraph exercises finalizeStructured's contract-miss path
// (RunAgent succeeds but the reserved artifact is missing → retryable=false).
func researchContractGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "agent", Caps: capsResearch, Label: "调研",
				Config: map[string]any{"agent_profile": "r", "prompt": "调研"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "research"},
			{ID: "e2", Source: "research", Target: "output", Kind: models.EdgeSuccess},
		},
	}
}

// reviewGateGraph exercises a structured-gate reject with no failure edge
// (deterministic, retryable=false).
func reviewGateGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "review", Type: "agent", Caps: capsCodeReview, Label: "评审",
				Config: map[string]any{"agent_profile": "v", "prompt": "评审"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "review"},
			{ID: "e2", Source: "review", Target: "output", Kind: models.EdgeSuccess},
		},
	}
}

// TestAutoRetryTransientRecovers: a node fails twice with a real ACP-style
// RunAgent error (retryable=true via execAgent), then succeeds. With the
// auto-retry cap set, the engine re-runs it from the failure position without
// any manual resume and the run completes.
func TestAutoRetryTransientRecovers(t *testing.T) {
	autoRetryBackoff = 0 // don't sleep in tests
	eng, db, p := setupEngineGraphP(t, autoRetryGraph())
	eng.SetAutoRetryMax(3)
	p.failLeft = map[string]int{"risky": 2} // fail twice, succeed on the 3rd
	// Real ACP unavailable / RetriableError style — no Chinese markers needed.
	p.reason = "agent chat: acp error: RetriableError: [unavailable] Error"

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "risky").Count(&n)
	if n != 3 {
		t.Errorf("risky execution rows = %d, want 3 (2 failed + 1 recovered)", n)
	}
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&models.StateRun{}).Error; err != nil {
		t.Fatalf("output node did not run after auto-retry: %v", err)
	}
}

// TestAutoRetryExhaustsBudget: a node that keeps failing with a retryable
// RunAgent error is retried exactly autoRetryMax times and then the run is
// failed (1 original attempt + N retries = N+1 execution rows).
func TestAutoRetryExhaustsBudget(t *testing.T) {
	autoRetryBackoff = 0
	eng, db, p := setupEngineGraphP(t, autoRetryGraph())
	eng.SetAutoRetryMax(2)
	p.failLeft = map[string]int{"risky": 99} // never recovers
	p.reason = "agent chat: acp error: RetriableError: [unavailable] Error"

	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "risky").Count(&n)
	if n != 3 {
		t.Errorf("risky execution rows = %d, want 3 (1 original + 2 auto-retries)", n)
	}
}

// TestAutoRetryEmptyMCPSurfaceRecovers: CAPA A7 empty MCP surface (no tool
// traffic + no node_complete) is retryable. After two empty attempts the fake
// emits a mark and the run completes without manual resume.
func TestAutoRetryEmptyMCPSurfaceRecovers(t *testing.T) {
	autoRetryBackoff = 0
	eng, db, p := setupEngineGraphP(t, autoRetryGraph())
	eng.SetAutoRetryMax(3)
	p.skipOutcomeLeft = 2 // two empty-surface failures, then success

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "risky").Count(&n)
	if n != 3 {
		t.Errorf("risky execution rows = %d, want 3 (2 empty-MCP + 1 recovered)", n)
	}
}

// TestAutoRetryContractFinalizeNotRetried: finalizeStructured contract miss
// carries the old marker text but leaves retryable=false, so it is never
// auto-retried even with a cap set.
func TestAutoRetryContractFinalizeNotRetried(t *testing.T) {
	autoRetryBackoff = 0
	eng, db, p := setupEngineGraphP(t, researchContractGraph())
	eng.SetAutoRetryMax(3)
	p.structuredSkipProduces = true

	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "research").Count(&n)
	if n != 1 {
		t.Errorf("research execution rows = %d, want 1 (contract miss, no retry)", n)
	}
}

// TestAutoRetryGateRejectNotRetried: a structured-gate reject (review verdict)
// is deterministic and untagged, so it is never auto-retried.
func TestAutoRetryGateRejectNotRetried(t *testing.T) {
	autoRetryBackoff = 0
	eng, db, p := setupEngineGraphP(t, reviewGateGraph())
	eng.SetAutoRetryMax(3)
	p.structuredBodies = map[string]string{
		"review": `{"summary":"no","verdict":"reject"}`,
	}

	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "review").Count(&n)
	if n != 1 {
		t.Errorf("review execution rows = %d, want 1 (gate reject, no retry)", n)
	}
}

// TestAutoRetryDisabledByDefault: with the cap at 0 (the raw-engine default), a
// retryable RunAgent failure is not retried — preserving fail-fast behavior.
func TestAutoRetryDisabledByDefault(t *testing.T) {
	autoRetryBackoff = 0
	eng, db, p := setupEngineGraphP(t, autoRetryGraph())
	// no SetAutoRetryMax -> disabled
	p.failLeft = map[string]int{"risky": 99}
	p.reason = "agent chat: acp error: RetriableError: [unavailable] Error"

	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "failed")

	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "risky").Count(&n)
	if n != 1 {
		t.Errorf("risky execution rows = %d, want 1 (auto-retry disabled)", n)
	}
}

// TestStaleNodeCompleteNotReusedAfterRunAgentError: a failed attempt that
// already called node_complete must not let the next attempt complete without
// a fresh mark (startNodeRun ClearOutcome drops Host mark + audit artifact
// before each new visit/iteration).
func TestStaleNodeCompleteNotReusedAfterRunAgentError(t *testing.T) {
	autoRetryBackoff = 0
	eng, db, p := setupEngineGraphP(t, autoRetryGraph())
	eng.SetAutoRetryMax(3)
	p.failLeft = map[string]int{"risky": 1}
	p.reason = "agent chat: acp error: RetriableError: [unavailable] Error"
	p.outcomeBeforeFail = true
	p.skipOutcome = true // success attempt deliberately omits node_complete

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ? AND status = ?", run.ID, "risky", "failed").
		Order("id desc").First(&sr).Error; err != nil {
		t.Fatalf("latest failed risky row: %v", err)
	}
	if !strings.Contains(sr.Error, "node_complete") && !strings.Contains(sr.OutputMd, "node_complete") {
		t.Fatalf("want missing node_complete after stale mark cleared, got err=%q outputMd=%q",
			sr.Error, sr.OutputMd)
	}
}

// TestIsAutoRetryableOnlyReadsFlag: table-driven check that the decider ignores
// err/outputMd content and returns only oc.retryable.
func TestIsAutoRetryableOnlyReadsFlag(t *testing.T) {
	cases := []struct {
		name string
		oc   nodeOutcome
		want bool
	}{
		{name: "flag true", oc: nodeOutcome{retryable: true, err: "anything"}, want: true},
		{name: "flag false with old marker in err", oc: nodeOutcome{
			retryable: false, err: "结构化产物契约未满足:research.json",
		}, want: false},
		{name: "flag false with 执行失败 in outputMd", oc: nodeOutcome{
			retryable: false, outputMd: "调研 执行失败:boom",
		}, want: false},
		{name: "zero value", oc: nodeOutcome{}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAutoRetryable(tc.oc); got != tc.want {
				t.Errorf("isAutoRetryable = %v, want %v", got, tc.want)
			}
		})
	}
}
