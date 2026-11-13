package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func latestStateRun(t *testing.T, eng *Engine, runID, nodeID string) models.StateRun {
	t.Helper()
	var sr models.StateRun
	if err := eng.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		t.Fatalf("load %s state: %v", nodeID, err)
	}
	return sr
}

// A review-enabled node's first run is an unattended execution: a failed
// check fails the run instead of opening the review.
func TestFinishNodeReviewFirstRunCheckFailFails(t *testing.T) {
	eng, db, p := setupReviewEngine(t)
	p.skipOutcome = true

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	if sr := latestStateRun(t, eng, run.ID, "prop"); sr.Status != "failed" {
		t.Fatalf("prop status = %q", sr.Status)
	}
	var n int64
	db.Model(&models.ReactConversation{}).Where("run_id = ? AND node_id = ?", run.ID, "prop").Count(&n)
	if n != 0 {
		t.Fatalf("a failed first run must not open a review dialogue, got %d", n)
	}
}

// Review confirm whose result fails a check reopens the dialogue and leaves
// the visit's row untouched; nothing is routed.
func TestFinishNodeReviewConfirmCheckFailReopens(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")
	before := latestStateRun(t, eng, run.ID, "prop")

	eng.host.SetRPCOutcomeValidator(&countingRPC{accept: false, msg: "产物不完整"})
	if err := eng.ReactReply(run.ID, "prop", "确认", nil, nil, true); err == nil || !strings.Contains(err.Error(), "产物不完整") {
		t.Fatalf("confirm error = %v", err)
	}

	after := latestStateRun(t, eng, run.ID, "prop")
	if after.Status != "waiting_human" || after.ID != before.ID {
		t.Fatalf("visit must stay waiting for the reviewer: %+v", after)
	}
	if str(after.Outputs["outcome_status"]) != str(before.Outputs["outcome_status"]) {
		t.Fatalf("reopen must not rewrite the visit's outputs: %+v", after.Outputs)
	}
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv)
	if conv.Done {
		t.Fatal("dialogue must be reopened")
	}
	var exits int64
	for _, te := range loadRun(t, db, run.ID).Trace {
		if te.NodeID == "prop" && te.Event == "exit" {
			exits++
		}
	}
	if exits != 0 {
		t.Fatalf("a reopened confirm must not route, got %d exit traces", exits)
	}
}

// A preview-issue cleanup failure on review confirm is a failed check of the
// revised result: the dialogue reopens.
func TestFinishNodeReviewConfirmPreviewCleanupFailureReopens(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, liveGraph(capsPreview))
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitReactPause(t, db, run.ID, "preview")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := db.Migrator().DropTable(&models.PreviewIssue{}); err != nil {
		t.Fatalf("drop issues: %v", err)
	}
	if err := eng.ReactReply(run.ID, "preview", "确认", nil, nil, true); err == nil || !strings.Contains(err.Error(), "mark preview issues resolved") {
		t.Fatalf("confirm error = %v", err)
	}
	if sr := latestStateRun(t, eng, run.ID, "preview"); sr.Status != "waiting_human" {
		t.Fatalf("visit must stay waiting: %q", sr.Status)
	}
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&conv)
	if conv.Done {
		t.Fatal("dialogue must be reopened")
	}
}

// Review confirm refreshes the handoff: the node_complete left by the
// finalize turn replaces the production-phase one.
func TestFinishNodeReviewConfirmRefreshesHandoff(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, handoffReviewGraph())
	run, _ := eng.StartRun("wf", nil, "test")
	waitReactPause(t, db, run.ID, "impl")
	waitRunStatus(t, db, run.ID, "waiting_human")

	p.mu.Lock()
	p.handoffText = "复审后:补齐了边界用例"
	p.mu.Unlock()
	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	sr := latestStateRun(t, eng, run.ID, "impl")
	if sr.Outputs[models.OutputHandoff] != "复审后:补齐了边界用例" || sr.Outputs[models.OutputHandoffSource] != models.HandoffSourceAgent {
		t.Fatalf("handoff not refreshed: %+v", sr.Outputs)
	}
	if up := p.seenUpstream["test"]; up == nil || up.Text != "复审后:补齐了边界用例" {
		t.Fatalf("downstream upstream handoff = %+v", up)
	}
}

// A confirm whose finalize turn leaves no node_complete keeps the result the
// visit already passed with, including its handoff.
func TestFinishNodeReviewConfirmWithoutMarkKeepsPassedResult(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, handoffReviewGraph())
	run, _ := eng.StartRun("wf", nil, "test")
	waitReactPause(t, db, run.ID, "impl")
	waitRunStatus(t, db, run.ID, "waiting_human")

	p.mu.Lock()
	p.confirmSkipOutcome = true
	p.mu.Unlock()
	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	sr := latestStateRun(t, eng, run.ID, "impl")
	if sr.Status != "completed" || sr.Outputs[models.OutputHandoff] != "fake handoff from impl" {
		t.Fatalf("passed result not kept: %s %+v", sr.Status, sr.Outputs)
	}
	p.mu.Lock()
	checks := p.handoffChecks["impl"]
	p.mu.Unlock()
	if checks != 1 {
		t.Fatalf("the confirm must run the handoff check once, got %d", checks)
	}
}

// node_complete status=failed left by the review confirm is the node's verdict:
// it routes as a fail (here: no failure edge, the run fails) instead of
// reopening the dialogue.
func TestFinishNodeReviewConfirmFailedMarkIsVerdict(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, handoffReviewGraph())
	run, _ := eng.StartRun("wf", nil, "test")
	waitReactPause(t, db, run.ID, "impl")
	waitRunStatus(t, db, run.ID, "waiting_human")

	p.mu.Lock()
	p.outcomeFailed = true
	p.mu.Unlock()
	if err := eng.ReactReply(run.ID, "impl", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	if sr := latestStateRun(t, eng, run.ID, "impl"); sr.Status != "failed" {
		t.Fatalf("impl status = %q", sr.Status)
	}
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "impl").First(&conv)
	if !conv.Done {
		t.Fatal("a failing verdict must not reopen the dialogue")
	}
}

// Clarify confirm is the end of the clarify execution: a failed check fails
// the node (no reopen), and the session is retired.
func TestFinishNodeClarifyConfirmCheckFailFails(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, reactOnlyGraph())
	p.reactSkipProduces = true
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.ReactReply(run.ID, "clarify", "完成", nil, nil, true); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	if sr := latestStateRun(t, eng, run.ID, "clarify"); sr.Status != "failed" {
		t.Fatalf("clarify status = %q", sr.Status)
	}
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv)
	if !conv.Done {
		t.Fatal("a failed clarify confirm must not reopen the dialogue")
	}
	if !p.retired[p.parkKey(run.ID, "clarify")] {
		t.Fatal("clarify confirm must retire the session")
	}
	var rv models.RunVariable
	db.Where("run_id = ? AND name = ?", run.ID, "last_error").First(&rv)
	if v, _ := rv.Value.(string); !strings.Contains(v, "产物") {
		t.Fatalf("last_error = %v", rv.Value)
	}
}

// A confirm-triggered failure never auto-retries, even when the failure itself
// is retryable and auto-retry is on: only unattended ends retry.
func TestFinishNodeConfirmFailureDoesNotAutoRetry(t *testing.T) {
	autoRetryBackoff = 0
	eng, db, p := setupEngineGraphP(t, reactOnlyGraph())
	eng.SetAutoRetryMax(3)
	p.skipOutcome = true
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.ReactReply(run.ID, "clarify", "完成", nil, nil, true); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	var n int64
	db.Model(&models.StateRun{}).Where("run_id = ? AND node_id = ?", run.ID, "clarify").Count(&n)
	if n != 1 {
		t.Fatalf("clarify execution rows = %d, want 1 (no auto-retry on confirm)", n)
	}
	for _, te := range loadRun(t, db, run.ID).Trace {
		if te.Event == "resume" && strings.Contains(te.Detail, "自动从失败位置重试") {
			t.Fatalf("confirm failure must not auto-retry: %+v", te)
		}
	}
}

// An approval gate's upstream producer that was revised while the gate waited
// is asked for node_complete again when the gate resolves, and the refreshed
// handoff is written back onto its execution.
func TestFinishNodeGateUpstreamWritesBackHandoff(t *testing.T) {
	eng, db, p := setupGateReactEngine(t)
	run, err := eng.StartRun("gate-react-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "select")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.GateReactRevise(run.ID, "select", "按意见改", nil, nil); err != nil {
		t.Fatalf("GateReactRevise: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.handoffCheckOutcome = true
	p.handoffText = "审批前已按意见修改"
	p.mu.Unlock()
	if err := eng.ResumeGate(run.ID, "select", "approve", nil); err != nil {
		t.Fatalf("ResumeGate: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	sr := latestStateRun(t, eng, run.ID, "prop")
	if sr.Outputs[models.OutputHandoff] != "审批前已按意见修改" {
		t.Fatalf("gate upstream handoff not written back: %+v", sr.Outputs)
	}
	p.mu.Lock()
	prompts := p.handoffPrompts["prop"]
	p.mu.Unlock()
	if prompts != 1 {
		t.Fatalf("a revised producer must be asked for node_complete once, got %d", prompts)
	}
	if !p.retired[p.parkKey(run.ID, "prop")] {
		t.Fatal("producer session must be retired after the gate resolves")
	}
}

// Resolving an approval gate whose upstream producer was not touched while
// the gate waited costs no model turn: the producer's node_complete is still
// current, and its handoff is kept.
func TestFinishNodeGateUpstreamUnchangedSkipsHandoffPrompt(t *testing.T) {
	eng, db, p := setupGateReactEngine(t)
	run, err := eng.StartRun("gate-react-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "select")
	waitRunStatus(t, db, run.ID, "waiting_human")
	before := latestStateRun(t, eng, run.ID, "prop")

	p.mu.Lock()
	p.handoffCheckOutcome = true
	p.handoffText = "不应出现"
	p.mu.Unlock()
	if err := eng.ResumeGate(run.ID, "select", "approve", nil); err != nil {
		t.Fatalf("ResumeGate: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	p.mu.Lock()
	prompts := p.handoffPrompts["prop"]
	p.mu.Unlock()
	if prompts != 0 {
		t.Fatalf("an unchanged producer must not be re-prompted, got %d", prompts)
	}
	sr := latestStateRun(t, eng, run.ID, "prop")
	if sr.Outputs[models.OutputHandoff] != before.Outputs[models.OutputHandoff] {
		t.Fatalf("handoff changed: %v → %v", before.Outputs[models.OutputHandoff], sr.Outputs[models.OutputHandoff])
	}
}

// An auto run of a review-enabled node that fails its check has nothing to
// hand off: no wrap-up turn is sent to its still-parked session.
func TestFinishNodeDecidedFailureSkipsWrapUpTurns(t *testing.T) {
	eng, db, p := setupReviewEngine(t)
	p.skipOutcome = true
	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "failed")

	p.mu.Lock()
	defer p.mu.Unlock()
	if n := p.handoffChecks["prop"]; n != 0 {
		t.Fatalf("handoff check on a failed run: %d", n)
	}
	if n := p.wrapUpCalls["prop"]; n != 0 {
		t.Fatalf("git check on a failed run: %d", n)
	}
}

func TestUpstreamHandoffPassesThroughGate(t *testing.T) {
	eng, _ := setupEngine(t)
	c := &execCtx{
		run: &models.Run{ID: "run-gate-pass", Trace: []models.TraceEntry{
			{NodeID: "impl", Event: "transition", To: "gate"},
			{NodeID: "gate", Event: "transition", To: "test"},
		}},
		graph: models.Graph{Nodes: []models.Node{
			{ID: "impl", Type: "agent", Label: "实现"},
			{ID: "gate", Type: "human_gate"},
			{ID: "test", Type: "agent"},
		}},
	}
	eng.db.Create(&models.StateRun{RunID: c.run.ID, NodeID: "impl", Iteration: 1, Status: "completed",
		Outputs: map[string]any{models.OutputHandoff: "实现说明", models.OutputHandoffSource: models.HandoffSourceAgent}})
	eng.db.Create(&models.StateRun{RunID: c.run.ID, NodeID: "gate", Iteration: 1, Status: "completed"})

	up := eng.upstreamHandoff(c, c.graph.FindNode("test"))
	if up == nil || up.NodeID != "impl" || up.Text != "实现说明" || up.Label != "实现" {
		t.Fatalf("upstream handoff = %+v", up)
	}
}
