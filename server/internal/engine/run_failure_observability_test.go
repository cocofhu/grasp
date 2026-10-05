package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/services"
)

// extractMCPToolText pulls the first text content blob from a tools/call RPC response.
func extractMCPToolText(t *testing.T, resp []byte) string {
	t.Helper()
	var envelope struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &envelope); err != nil {
		t.Fatalf("unmarshal mcp resp: %v\n%s", err, resp)
	}
	if envelope.Result.IsError || len(envelope.Result.Content) == 0 {
		t.Fatalf("mcp tool error: %s", resp)
	}
	return envelope.Result.Content[0].Text
}

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

// TestResearchEarlyFailureThreeChannelsNonEmpty locks the clarified acceptance:
// first-node (research) Agent/sandbox-style failure must expose a non-empty
// reason via run_error.json, AggregateRunFailure (DTO/API), and PM MCP.
func TestResearchEarlyFailureThreeChannelsNonEmpty(t *testing.T) {
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

	// (c) ArtifactSummary + pm_list_runs
	ps := services.NewProjectService(db)
	proj, err := ps.Create("ObsFail", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").Update("project_id", proj.ID).Error; err != nil {
		t.Fatal(err)
	}
	pm := services.NewPmService(db, nil)
	en := true
	agent := "agent-a"
	if _, err := pm.UpdateBinding(proj.ID, &en, &agent, []string{pmmcp.MCPProgress, pmmcp.MCPWorkflowRead}, nil, nil); err != nil {
		t.Fatal(err)
	}
	progress := services.NewPmProgress(pm, rs, arts)
	summary := progress.ArtifactSummary(proj.ID, run.ID, 10)
	sumErr, _ := summary["error"].(string)
	if sumErr == "" {
		t.Fatalf("ArtifactSummary missing error: %+v", summary)
	}
	// After finish, run_error.json should appear in the product list.
	if summary["empty"] == true {
		t.Fatalf("expected run_error.json in artifacts: %+v", summary)
	}

	host := pmmcp.NewHost(pm, progress, services.NewWorkflowService(db), rs, arts, eng)
	tok := platformmcp.NewToken()
	host.Restore(proj.ID, "thr-obs", "tester", "agent-a", tok)
	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "pm_list_runs",
			"arguments": map[string]any{"workflowId": "wf", "limit": 5},
		},
	})
	st, resp := host.ServeRPC(proj.ID, pmmcp.MCPWorkflowRead, tok, req)
	if st != 200 {
		t.Fatalf("pm_list_runs status=%d body=%s", st, resp)
	}
	text := extractMCPToolText(t, resp)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("payload: %v\n%s", err, text)
	}
	found := false
	switch rows := payload["runs"].(type) {
	case []any:
		for _, row := range rows {
			m, _ := row.(map[string]any)
			if m["id"] == run.ID {
				found = true
				if errStr, _ := m["error"].(string); errStr == "" {
					t.Fatalf("pm_list_runs row missing error: %+v", m)
				}
			}
		}
	case []map[string]any:
		for _, m := range rows {
			if m["id"] == run.ID {
				found = true
				if errStr, _ := m["error"].(string); errStr == "" {
					t.Fatalf("pm_list_runs row missing error: %+v", m)
				}
			}
		}
	}
	if !found {
		t.Fatalf("run not in pm_list_runs: %s", text)
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
