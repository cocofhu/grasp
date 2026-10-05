package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
)

type countingRPC struct {
	calls  int
	accept bool
	msg    string
}

func (c *countingRPC) Validate(ctx context.Context, in mcp.OutcomeValidateIn) (mcp.OutcomeValidateOut, error) {
	c.calls++
	return mcp.OutcomeValidateOut{Accept: c.accept, Message: c.msg}, nil
}

func TestAfterDefaultChecksSkipsRPCOnFailure(t *testing.T) {
	eng, _ := setupEngine(t)
	rpc := &countingRPC{accept: true}
	eng.host.SetRPCOutcomeValidator(rpc)

	oc := eng.afterDefaultChecks(&execCtx{run: &models.Run{ID: "r"}}, &models.Node{ID: "n", Type: "agent", Caps: capsResearch},
		nodeOutcome{status: "failed", err: "missing artifact"})
	if oc.status != "failed" {
		t.Fatalf("status = %s", oc.status)
	}
	if rpc.calls != 0 {
		t.Fatalf("RPC called on default failure: %d", rpc.calls)
	}
}

func TestAfterDefaultChecksRunsRPCOnSuccess(t *testing.T) {
	eng, _ := setupEngine(t)
	rpc := &countingRPC{accept: false, msg: "biz no"}
	eng.host.SetRPCOutcomeValidator(rpc)

	oc := eng.afterDefaultChecks(&execCtx{run: &models.Run{ID: "r"}}, &models.Node{ID: "n", Type: "agent", Caps: capsResearch},
		nodeOutcome{status: "completed", outputs: map[string]any{"outcome_status": "success"}})
	if oc.status != "failed" || oc.err != "biz no" {
		t.Fatalf("want rpc reject, got status=%s err=%q", oc.status, oc.err)
	}
	if rpc.calls != 1 {
		t.Fatalf("RPC calls = %d", rpc.calls)
	}
}

// TestConsumeNodeOutcomeEmptyMCPSurface (CAPA A7): true-zero MCP evidence +
// missing outcome + no artifact ⇒ distinct "工具面为空/不可达" failure.
func TestConsumeNodeOutcomeEmptyMCPSurface(t *testing.T) {
	eng, _ := setupEngine(t)
	runID := "r-empty-mcp"
	nodeID := "research"
	eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsResearch)

	c := &execCtx{run: &models.Run{ID: runID}}
	node := &models.Node{ID: nodeID, Type: "agent", Caps: capsResearch}
	res := &runtime.NodeResult{}
	fail, ok := eng.consumeNodeOutcome(c, node, res)
	if ok {
		t.Fatal("want missing outcome failure")
	}
	if fail.err != errMCPSurfaceEmpty {
		t.Fatalf("err=%q want %q", fail.err, errMCPSurfaceEmpty)
	}
	if !fail.retryable {
		t.Fatal("empty MCP surface must be retryable for NodeAutoRetryMax")
	}
	if !strings.Contains(fail.outputMd, errMCPSurfaceEmpty) {
		t.Fatalf("outputMd=%q", fail.outputMd)
	}
}

// TestConsumeNodeOutcomeMissingNodeComplete (CAPA A7): MCP traffic present but
// no mark ⇒ classic「未调用 node_complete」wording (not empty surface).
func TestConsumeNodeOutcomeMissingNodeComplete(t *testing.T) {
	eng, _ := setupEngine(t)
	runID := "r-has-mcp"
	nodeID := "research"
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsResearch)

	st, resp := eng.host.ServeRPC(runID, tok, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"note.md","content":"x","kind":"markdown"}}}`))
	if st != 200 {
		t.Fatalf("ServeRPC status=%d resp=%s", st, resp)
	}
	if n := len(eng.host.PeekMcpCalls(runID, nodeID)); n != 1 {
		t.Fatalf("PeekMcpCalls=%d want 1", n)
	}

	c := &execCtx{run: &models.Run{ID: runID}}
	node := &models.Node{ID: nodeID, Type: "agent", Caps: capsResearch}
	res := &runtime.NodeResult{}
	fail, ok := eng.consumeNodeOutcome(c, node, res)
	if ok {
		t.Fatal("want missing outcome failure")
	}
	if fail.err != errMissingNodeComplete {
		t.Fatalf("err=%q want %q", fail.err, errMissingNodeComplete)
	}
	if fail.retryable {
		t.Fatal("missing node_complete with MCP evidence must not be auto-retryable")
	}
	if strings.Contains(fail.err, "工具面为空") {
		t.Fatalf("must not use empty-surface wording when MCP traffic exists: %q", fail.err)
	}
}

// TestConsumeNodeOutcomeAdoptsArtifactAfterFlush: Host Peek empty after flush
// but node_complete.json(success) exists → adopt, not empty-surface (Demo S1).
func TestConsumeNodeOutcomeAdoptsArtifactAfterFlush(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "r-adopt"
	nodeID := "react_rlze"
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsClarify)

	// Business call then node_complete (writes Host mark + artifact).
	st, resp := eng.host.ServeRPC(runID, tok, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"clarified_requirement.json","content":"{\"title\":\"t\"}","kind":"json"}}}`))
	if st != 200 {
		t.Fatalf("write_artifact status=%d resp=%s", st, resp)
	}
	st, resp = eng.host.ServeRPC(runID, tok, []byte(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success","summary":"done"}}}`))
	if st != 200 || strings.Contains(string(resp), `"error"`) {
		t.Fatalf("node_complete status=%d resp=%s", st, resp)
	}

	// Persist calls then clear Host buffer (flushMcpCalls shape) + Clear Host mark
	// (same-visit mis-clear repro before FR4 fix — evidence still in store/StateRun).
	now := time.Now()
	calls := eng.host.TakeMcpCalls(runID, nodeID)
	if len(calls) < 2 {
		t.Fatalf("want buffered calls before flush, got %d", len(calls))
	}
	if err := db.Create(&models.StateRun{
		RunID: runID, NodeID: nodeID, NodeType: "agent", Iteration: 1,
		Status: "running", McpCalls: calls, StartedAt: &now,
	}).Error; err != nil {
		t.Fatalf("state_run: %v", err)
	}
	eng.host.ClearOutcome(runID, nodeID) // clears memory only path for this test:
	// re-write artifact because ClearOutcome deletes audit via ArtifactDeleter
	if _, err := eng.store.Save(runID, nodeID, mcp.NodeOutcomeArtifactName, "json",
		mcp.OutcomeJSON(mcp.NodeOutcome{Status: mcp.OutcomeSuccess, Summary: "done"})); err != nil {
		t.Fatalf("restore artifact: %v", err)
	}
	if n := len(eng.host.PeekMcpCalls(runID, nodeID)); n != 0 {
		t.Fatalf("Peek after flush want 0, got %d", n)
	}
	if eng.host.HasOutcome(runID, nodeID) {
		t.Fatal("Host mark should be cleared")
	}

	c := &execCtx{run: &models.Run{ID: runID}, token: tok}
	node := &models.Node{ID: nodeID, Type: "agent", Caps: capsClarify}
	res := &runtime.NodeResult{Outputs: map[string]any{}}
	fail, ok := eng.consumeNodeOutcome(c, node, res)
	if !ok {
		t.Fatalf("want adopt success, got fail err=%q", fail.err)
	}
	if res.Outputs["outcome_status"] != "success" {
		t.Fatalf("outputs=%v", res.Outputs)
	}
}

// TestConsumeNodeOutcomeStateRunOnlyNotEmptySurface: Peek empty but StateRun
// has MCP calls and no mark → 未调用, not 工具面为空 (Demo S2).
func TestConsumeNodeOutcomeStateRunOnlyNotEmptySurface(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "r-state-only"
	nodeID := "research"
	eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsResearch)

	now := time.Now()
	if err := db.Create(&models.StateRun{
		RunID: runID, NodeID: nodeID, NodeType: "agent", Iteration: 1,
		Status: "running", StartedAt: &now,
		McpCalls: []models.McpCall{{Tool: "write_artifact"}, {Tool: "set_research"}},
	}).Error; err != nil {
		t.Fatal(err)
	}

	c := &execCtx{run: &models.Run{ID: runID}}
	node := &models.Node{ID: nodeID, Type: "agent", Caps: capsResearch}
	fail, ok := eng.consumeNodeOutcome(c, node, &runtime.NodeResult{})
	if ok {
		t.Fatal("want failure")
	}
	if fail.err != errMissingNodeComplete {
		t.Fatalf("err=%q want %q", fail.err, errMissingNodeComplete)
	}
}

// TestConsumeNodeOutcomeCorruptArtifact: artifact exists but unparseable →
// mark lost wording (Demo S4), not empty surface.
func TestConsumeNodeOutcomeCorruptArtifact(t *testing.T) {
	eng, _ := setupEngine(t)
	runID := "r-corrupt"
	nodeID := "test"
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsTest)
	if _, err := eng.store.Save(runID, nodeID, mcp.NodeOutcomeArtifactName, "json", "{not-json"); err != nil {
		t.Fatal(err)
	}
	st, resp := eng.host.ServeRPC(runID, tok, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"x.md","content":"x","kind":"markdown"}}}`))
	if st != 200 {
		t.Fatalf("status=%d resp=%s", st, resp)
	}

	fail, ok := eng.consumeNodeOutcome(&execCtx{run: &models.Run{ID: runID}}, &models.Node{ID: nodeID, Type: "agent", Caps: capsTest}, &runtime.NodeResult{})
	if ok {
		t.Fatal("want failure")
	}
	if fail.err != errOutcomeMarkLost {
		t.Fatalf("err=%q want %q", fail.err, errOutcomeMarkLost)
	}
}

// TestConsumeNodeOutcomeAdoptsFailedArtifact: status=failed mark is normal
// finalize (failed result), not empty-surface / 未调用.
func TestConsumeNodeOutcomeAdoptsFailedArtifact(t *testing.T) {
	eng, _ := setupEngine(t)
	runID := "r-failed-mark"
	nodeID := "implement"
	eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsImplement)
	if _, err := eng.store.Save(runID, nodeID, mcp.NodeOutcomeArtifactName, "json",
		mcp.OutcomeJSON(mcp.NodeOutcome{Status: mcp.OutcomeFailed, Error: "plan incomplete"})); err != nil {
		t.Fatal(err)
	}

	fail, ok := eng.consumeNodeOutcome(&execCtx{run: &models.Run{ID: runID}},
		&models.Node{ID: nodeID, Type: "agent", Caps: capsImplement}, &runtime.NodeResult{})
	if ok {
		t.Fatal("want failed outcome (agent-reported)")
	}
	if fail.err != "plan incomplete" {
		t.Fatalf("err=%q", fail.err)
	}
	if strings.Contains(fail.err, "工具面") || strings.Contains(fail.err, "未调用") {
		t.Fatalf("must not remap failed mark: %q", fail.err)
	}
}

// TestNodeReqDoesNotClearOutcome: same-visit NodeReq rebuild must keep mark.
func TestNodeReqDoesNotClearOutcome(t *testing.T) {
	eng, _ := setupEngine(t)
	runID := "r-keep-mark"
	nodeID := "work"
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsPlain)
	st, resp := eng.host.ServeRPC(runID, tok, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success","summary":"ok"}}}`))
	if st != 200 {
		t.Fatalf("node_complete: %s", resp)
	}
	if !eng.host.HasOutcome(runID, nodeID) {
		t.Fatal("want mark")
	}
	c := &execCtx{run: &models.Run{ID: runID}, token: tok, vars: map[string]any{}}
	_ = eng.nodeReq(c, &models.Node{ID: nodeID, Type: "agent", Caps: capsPlain, Config: map[string]any{}})
	if !eng.host.HasOutcome(runID, nodeID) {
		t.Fatal("nodeReq must not ClearOutcome on same visit")
	}
}

// TestStartNodeRunClearsOutcome: new visit clears Host mark + artifact.
func TestStartNodeRunClearsOutcome(t *testing.T) {
	eng, _ := setupEngine(t)
	runID := "r-clear-visit"
	nodeID := "research"
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, nodeID, capsResearch)
	_, _ = eng.host.ServeRPC(runID, tok, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success"}}}`))
	if !eng.host.HasOutcome(runID, nodeID) {
		t.Fatal("want mark")
	}
	if _, ok := eng.store.Get(runID, mcp.NodeOutcomeArtifactName); !ok {
		t.Fatal("want artifact")
	}
	c := &execCtx{run: &models.Run{ID: runID}, iter: map[string]int{nodeID: 2}, vars: map[string]any{}}
	eng.startNodeRun(c, &models.Node{ID: nodeID, Type: "agent", Caps: capsResearch})
	if eng.host.HasOutcome(runID, nodeID) {
		t.Fatal("startNodeRun must ClearOutcome")
	}
	if _, ok := eng.store.Get(runID, mcp.NodeOutcomeArtifactName); ok {
		t.Fatal("startNodeRun must delete node_complete.json")
	}
}

func TestMissingOutcomeErrBranches(t *testing.T) {
	if got := missingOutcomeErr(nil, nil, mcp.OutcomeArtifactAbsent); got != errMCPSurfaceEmpty {
		t.Fatalf("zero evidence: %q", got)
	}
	if got := missingOutcomeErr([]models.McpCall{{Tool: "write_artifact"}}, nil, mcp.OutcomeArtifactAbsent); got != errMissingNodeComplete {
		t.Fatalf("host calls: %q", got)
	}
	if got := missingOutcomeErr(nil, []models.McpCall{{Tool: "set_research"}}, mcp.OutcomeArtifactAbsent); got != errMissingNodeComplete {
		t.Fatalf("state calls: %q", got)
	}
	if got := missingOutcomeErr(nil, nil, mcp.OutcomeArtifactCorrupt); got != errOutcomeMarkLost {
		t.Fatalf("corrupt: %q", got)
	}
	if got := missingOutcomeErr([]models.McpCall{{Tool: "x"}}, nil, mcp.OutcomeArtifactCorrupt); got != errOutcomeMarkLost {
		t.Fatalf("corrupt with MCP: %q", got)
	}
}

func TestAggregateRunFailureDegradeNote(t *testing.T) {
	_, db := setupEngine(t)
	s := services.NewRunService(db)
	db.Create(&models.Run{ID: "rf-degrade", Status: "failed"})
	db.Create(&models.StateRun{RunID: "rf-degrade", NodeID: "research", Status: "failed", Error: "boom"})
	info := s.AggregateRunFailure("rf-degrade", "容器已销毁，未能拉取 live logs")
	if !info.NoSandboxLog {
		t.Fatal("expected noSandboxLog")
	}
	if info.SandboxLogNote != "容器已销毁，未能拉取 live logs" {
		t.Fatalf("note=%q", info.SandboxLogNote)
	}
	display := info.DisplayReason()
	if !strings.Contains(display, "容器已销毁") {
		t.Fatalf("display=%q", display)
	}
}
