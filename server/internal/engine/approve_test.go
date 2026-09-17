package engine

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

func approveOnlyGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "predev", Type: "approve", Config: map[string]any{"agent_profile": "pm"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "predev"},
			{ID: "e2", Source: "predev", Target: "output"},
		},
	}
}

func startApproveAndReply(t *testing.T, eng *Engine, db *gorm.DB) *models.Run {
	t.Helper()
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	if err := eng.ReactReply(run.ID, "predev", "确认", nil, nil, false); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitApproveReady(t, eng, db, run.ID, "predev")
	if err := eng.ReactReply(run.ID, "predev", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("force: %v", err)
	}
	return run
}

// waitApproveReady waits until the async !force turn has finished and the
// session is idle. Force-finish rejects with "澄清进行中或待发送队列非空"
// if we only wait for the human bubble to land.
func waitApproveReady(t *testing.T, eng *Engine, db *gorm.DB, runID, nodeID string) {
	t.Helper()
	waitApproveHumanTurn(t, db, runID, nodeID)
	waitRunStatus(t, db, runID, "waiting_human")
	if err := eng.waitReviewReadyForTest(runID, nodeID, waitPollTimeout); err != nil {
		t.Fatalf("approve session not idle: %v", err)
	}
}

func waitApproveHumanTurn(t *testing.T, db *gorm.DB, runID, nodeID string) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		var conv models.ReactConversation
		if err := db.Where("run_id = ? AND node_id = ?", runID, nodeID).First(&conv).Error; err == nil {
			for _, m := range conv.Messages {
				if m.Role == "human" {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("approve human turn not persisted")
}

func TestApproveCompletesWithClarifiedAndPlan(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run := startApproveAndReply(t, eng, db)
	waitRunStatus(t, db, run.ID, "completed")
	for _, name := range []string{mcp.ClarifiedRequirementArtifactName, mcp.PlanArtifactName} {
		var c int64
		db.Model(&models.Artifact{}).Where("run_id = ? AND name = ?", run.ID, name).Count(&c)
		if c == 0 {
			t.Errorf("expected %s", name)
		}
	}
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&sr).Error; err != nil {
		t.Fatalf("state: %v", err)
	}
	if sr.Status != "completed" {
		t.Fatalf("node status=%s", sr.Status)
	}
	if _, ok := sr.Outputs["clarified_requirement"]; !ok {
		t.Error("missing outputs.clarified_requirement")
	}
	if _, ok := sr.Outputs["plan"]; !ok {
		t.Error("missing outputs.plan")
	}
	// force path must have consumed node_complete (audit json may be cleared by later nodes).
	if got, _ := sr.Outputs["outcome_status"].(string); got != "success" {
		t.Errorf("outcome_status=%q want success (force requires node_complete)", got)
	}
	if _, ok := sr.Outputs["research"]; ok {
		t.Error("optional research should be absent when not written")
	}
}

// TestApproveClearsPrematureNodeComplete locks g1.1 via fake provider:
// !force clears a pre-seeded node_complete mark and keeps waiting_human.
func TestApproveClearsPrematureNodeComplete(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	eng.host.SetActiveNode(run.ID, "predev", "approve")
	// Temporarily open the tool surface so the seed can plant a mark; Phase1
	// then closes it again before !force ClearOutcome (defense-in-depth).
	eng.host.SetOutcomeAllowed(run.ID, true)
	st, resp := eng.host.ServeRPC(run.ID, run.McpToken, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success","summary":"premature"}}}`))
	if st != 200 {
		t.Fatalf("seed outcome: status=%d resp=%s", st, resp)
	}
	eng.host.SetOutcomeAllowed(run.ID, false)
	if !eng.host.HasOutcome(run.ID, "predev") {
		t.Fatal("expected premature mark before !force reply")
	}
	if err := eng.ReactReply(run.ID, "predev", "做登录", nil, nil, false); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitApproveReady(t, eng, db, run.ID, "predev")
	if eng.host.HasOutcome(run.ID, "predev") {
		t.Fatal("!force approve must ClearOutcome")
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	if conv.Done {
		t.Fatal("premature node_complete must not finish approve")
	}
}

func TestApproveFailsWithoutNodeComplete(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, approveOnlyGraph())
	p.skipOutcome = true
	run := startApproveAndReply(t, eng, db)
	waitRunStatus(t, db, run.ID, "failed")
}

func TestApproveFailsWithoutPlan(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, approveOnlyGraph())
	p.approveSkipPlan = true
	run := startApproveAndReply(t, eng, db)
	waitRunStatus(t, db, run.ID, "failed")
}

func TestApproveFailsWithoutClarified(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, approveOnlyGraph())
	p.reactSkipProduces = true
	run := startApproveAndReply(t, eng, db)
	waitRunStatus(t, db, run.ID, "failed")
}

func TestApproveOptionalResearchLifted(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, approveOnlyGraph())
	p.approveWriteOptional = true
	run := startApproveAndReply(t, eng, db)
	waitRunStatus(t, db, run.ID, "completed")
	var c int64
	db.Model(&models.Artifact{}).Where("run_id = ? AND name = ?", run.ID, mcp.ResearchArtifactName).Count(&c)
	if c == 0 {
		t.Error("expected research.json")
	}
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&sr).Error; err != nil {
		t.Fatalf("state: %v", err)
	}
	if _, ok := sr.Outputs["research"]; !ok {
		t.Error("optional research should be lifted into outputs")
	}
}

func TestApproveOpenParksEmptyTranscript(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	waitRunStatus(t, db, run.ID, "waiting_human")
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	if conv.Messages == nil {
		t.Fatal("approve open must persist empty slice, not nil")
	}
	if len(conv.Messages) != 0 {
		t.Fatalf("approve open must persist empty transcript, got %d", len(conv.Messages))
	}
	found := false
	for _, it := range services.NewRunService(db).AllPendingInboxItems() {
		if c, ok := it.(services.ClarifyInboxItem); ok && c.RunID == run.ID && c.NodeID == "predev" {
			found = true
		}
	}
	if !found {
		t.Fatal("empty approve park must still appear in inbox")
	}
}

func TestApproveReplyStaysWaitingUntilForce(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	if err := eng.ReactReply(run.ID, "predev", "做登录", nil, nil, false); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitApproveHumanTurn(t, db, run.ID, "predev")
	waitRunStatus(t, db, run.ID, "waiting_human")
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	if conv.Done {
		t.Fatal("ordinary approve reply must not finish the conversation")
	}
}

func TestApproveIgnoresLeftoverAutoVar(t *testing.T) {
	g := approveOnlyGraph()
	g.Variables = []models.Variable{{Name: "auto_clarify", Type: "bool", Value: true}}
	g.Nodes[1].Config["auto_var"] = "auto_clarify"
	eng, db, _ := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	waitRunStatus(t, db, run.ID, "waiting_human")
}

// Upstream plan.json must not satisfy Approve's required set_plan delivery.
func TestApproveRejectsUpstreamPlanArtifact(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, approveOnlyGraph())
	p.approveSkipPlan = true
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	_, planBody := fakeStructured("plan")
	if _, err := eng.store.Save(run.ID, "upstream_plan", mcp.PlanArtifactName, "json", planBody); err != nil {
		t.Fatalf("seed upstream plan: %v", err)
	}
	if err := eng.ReactReply(run.ID, "predev", "确认", nil, nil, false); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitApproveReady(t, eng, db, run.ID, "predev")
	if err := eng.ReactReply(run.ID, "predev", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("force: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")
}

func markApproveDoneAndReenter(t *testing.T, eng *Engine, db *gorm.DB, runID string) nodeOutcome {
	t.Helper()
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", runID, "predev").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	conv.Done = true
	if err := db.Save(&conv).Error; err != nil {
		t.Fatalf("mark done: %v", err)
	}
	c, err := eng.loadCtx(runID)
	if err != nil {
		t.Fatalf("loadCtx: %v", err)
	}
	node := c.graph.FindNode("predev")
	if node == nil {
		t.Fatal("missing predev")
	}
	c.iter["predev"] = conv.Iteration
	return eng.execReactEnter(c, node)
}

const doneShortCircuitClarified = `{
		"title":"需求","summary":"Done 再进入",
		"background":"测试背景",
		"goals":["完成需求"],"in_scope":["本功能"],"out_of_scope":["其它"],
		"functional_requirements":[{"id":"f1","title":"需求","detail":"实现所述需求","priority":"must","acceptance_criteria":["可验收"]}],
		"assumptions":["无额外假设(已与用户确认)"],"dependencies":["无额外依赖(已与用户确认)"],"constraints":["无额外约束(已与用户确认)"]
	}`

// Done=true without this node's plan must not complete (g2.1).
func TestApproveDoneShortCircuitFailsWithoutPlan(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	if _, err := eng.store.Save(run.ID, "predev", mcp.ClarifiedRequirementArtifactName, "json", doneShortCircuitClarified); err != nil {
		t.Fatalf("seed clarified: %v", err)
	}
	oc := markApproveDoneAndReenter(t, eng, db, run.ID)
	if oc.status == "completed" {
		t.Fatalf("Done without plan must not complete: %+v", oc)
	}
	if oc.status != "failed" {
		t.Fatalf("status=%s want failed (%s)", oc.status, oc.err)
	}
}

func TestApproveDoneShortCircuitCompletesWithOwnedProducts(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	if _, err := eng.store.Save(run.ID, "predev", mcp.ClarifiedRequirementArtifactName, "json", doneShortCircuitClarified); err != nil {
		t.Fatalf("seed clarified: %v", err)
	}
	_, planBody := fakeStructured("plan")
	if _, err := eng.store.Save(run.ID, "predev", mcp.PlanArtifactName, "json", planBody); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	oc := markApproveDoneAndReenter(t, eng, db, run.ID)
	if oc.status != "completed" {
		t.Fatalf("owned products must complete, got %s %s", oc.status, oc.err)
	}
	if _, ok := oc.outputs["clarified_requirement"]; !ok {
		t.Error("missing outputs.clarified_requirement")
	}
	if _, ok := oc.outputs["plan"]; !ok {
		t.Error("missing outputs.plan")
	}
}

func TestApproveDoneShortCircuitRejectsUpstreamPlan(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	if _, err := eng.store.Save(run.ID, "predev", mcp.ClarifiedRequirementArtifactName, "json", doneShortCircuitClarified); err != nil {
		t.Fatalf("seed clarified: %v", err)
	}
	_, planBody := fakeStructured("plan")
	if _, err := eng.store.Save(run.ID, "upstream_plan", mcp.PlanArtifactName, "json", planBody); err != nil {
		t.Fatalf("seed upstream plan: %v", err)
	}
	oc := markApproveDoneAndReenter(t, eng, db, run.ID)
	if oc.status == "completed" {
		t.Fatalf("upstream plan must not satisfy Done re-entry: %+v", oc)
	}
	if oc.status != "failed" {
		t.Fatalf("status=%s want failed (%s)", oc.status, oc.err)
	}
}

// Optional research written by another node must not be lifted into Approve outputs.
func TestApproveDoesNotLiftUpstreamOptionalResearch(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	_, researchBody := fakeStructured("research")
	if _, err := eng.store.Save(run.ID, "upstream_research", mcp.ResearchArtifactName, "json", researchBody); err != nil {
		t.Fatalf("seed upstream research: %v", err)
	}
	if err := eng.ReactReply(run.ID, "predev", "确认", nil, nil, false); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitApproveReady(t, eng, db, run.ID, "predev")
	if err := eng.ReactReply(run.ID, "predev", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("force: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&sr).Error; err != nil {
		t.Fatalf("state: %v", err)
	}
	if _, ok := sr.Outputs["research"]; ok {
		t.Error("upstream research must not be lifted into approve outputs")
	}
}
