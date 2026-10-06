package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

// reviewGraph: input → research (review on) → output.
func reviewGraph() models.Graph {
	return models.Graph{
		Variables: []models.Variable{
			{Name: "idea", Type: "paragraph", Ask: true, Required: true, Editable: true},
		},
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "prop", Type: "agent", Caps: capsResearch, Label: "调研", Config: map[string]any{"agent_profile": "pm-agent", "prompt": "做调研"}},
			{ID: "output", Type: "output", Label: "输出"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "prop"},
			{ID: "e2", Source: "prop", Target: "output"},
		},
	}
}

func setupReviewEngine(t *testing.T) (*Engine, *gorm.DB, *fakeProvider) {
	t.Helper()
	db, err := database.OpenSQLiteTest(t.TempDir() + "/review.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	g := reviewGraph()
	wf := models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "review-wf", Name: "review-wf", Version: 1, PublishedVersion: 1, Graph: g}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: g}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	provider := &fakeProvider{host: host}
	eng := New(db, provider, host, arts, 5)
	cleanupEngineDB(t, eng, db)
	return eng, db, provider
}

// TestReviewEnterReviseFinish: review var truthy ⇒ enter interactive review; a
// revise turn edits in place (stays paused), then a forced finish re-validates
// the product contract WITHOUT Agent ReactReply and advances to completion.
func TestReviewEnterReviseFinish(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// The producer runs once, then pauses in the review phase.
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	// The seeded conversation opens with an agent product-summary turn.
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("load review conv: %v", err)
	}
	if len(conv.Messages) == 0 || conv.Messages[0].Role != "agent" {
		t.Fatalf("review conversation should open with an agent summary turn: %+v", conv.Messages)
	}

	// A revise turn with an annotation keeps the node paused (in-place edit).
	anns := []models.ReactAnnotation{{JSONPath: "findings[r1]", Note: "把发现说得更具体"}}
	if err := eng.ReactReply(run.ID, "prop", "按标注改一下", nil, anns, false); err != nil {
		t.Fatalf("revise reply: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatalf("wait revise: %v", err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human") // still paused after a revise
	if provider.reviseCalls["prop"] != 1 {
		t.Fatalf("expected one ReviseInPlace call, got %d", provider.reviseCalls["prop"])
	}
	// The human turn persisted its annotation for re-render.
	db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv)
	var sawAnnotation bool
	for _, m := range conv.Messages {
		if m.Role == "human" && len(m.Annotations) == 1 && m.Annotations[0].JSONPath == "findings[r1]" {
			sawAnnotation = true
		}
	}
	if !sawAnnotation {
		t.Fatalf("annotation not persisted on the human review turn: %+v", conv.Messages)
	}

	// Force finish → no Agent ReactReply → re-validate store snapshot → advance.
	beforeReact := provider.reactReplyCalls["prop"]
	if err := eng.ReactReply(run.ID, "prop", "确认", nil, nil, true); err != nil {
		t.Fatalf("finish reply: %v", err)
	}
	if provider.reactReplyCalls["prop"] != beforeReact {
		t.Fatalf("review force must not call ReactReply; got %d (before %d)",
			provider.reactReplyCalls["prop"], beforeReact)
	}
	if !provider.retired[provider.parkKey(run.ID, "prop")] {
		t.Fatal("expected RetireSession on review force success")
	}
	if provider.wrapUpCalls["prop"] != 1 {
		t.Fatalf("expected OfferCommitOnConfirm once before retire, got %d", provider.wrapUpCalls["prop"])
	}
	if provider.wrapUpAfterRetire {
		t.Fatal("OfferCommitOnConfirm must run before RetireSession")
	}
	waitRunStatus(t, db, run.ID, "completed")

	db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv)
	if !conv.Done {
		t.Fatal("expected review conversation Done after successful force")
	}

	// The reserved product survived the review and remains in the store.
	if _, ok := arts(db, run.ID, mcp.ResearchArtifactName); !ok {
		t.Fatalf("research.json missing after review finish")
	}
}

// TestReviewForcePersistsGitWrapUp: confirm-time OfferCommitOnConfirm narration
// is stored as the agent turn answering the human「确认」.
func TestReviewForcePersistsGitWrapUp(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	provider.wrapUpMsg = "已提交 src/a.go,跳过 tmp.log"

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")

	if err := eng.ReactReply(run.ID, "prop", "确认", nil, nil, true); err != nil {
		t.Fatalf("finish reply: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	if !conv.Done {
		t.Fatal("expected Done")
	}
	var saw bool
	for _, m := range conv.Messages {
		if m.Role == "agent" && m.Text == provider.wrapUpMsg {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("git wrap-up narration not persisted: %+v", conv.Messages)
	}
}

// TestReviewForceValidationFailureKeepsPaused: business re-validation failure
// must not Done/routeFailure; the review stays waiting_human and is retryable.
func TestReviewForceValidationFailureKeepsPaused(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	// Install rejector only after the producer already entered review, so the
	// initial RunAgent finalize is unaffected.
	rpc := &countingRPC{accept: false, msg: "业务校验未通过:产物不完整"}
	eng.host.SetRPCOutcomeValidator(rpc)

	err = eng.ReactReply(run.ID, "prop", "确认", nil, nil, true)
	if err == nil {
		t.Fatal("expected validation failure error from review force")
	}
	if !strings.Contains(err.Error(), "业务校验未通过") {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.reactReplyCalls["prop"] != 0 {
		t.Fatalf("review force must not call ReactReply on failure; got %d", provider.reactReplyCalls["prop"])
	}

	waitRunStatus(t, db, run.ID, "waiting_human")
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	if conv.Done {
		t.Fatal("conversation must stay open after validation failure")
	}

	// Retry after validator accepts: should complete without Agent wrap-up.
	rpc.accept = true
	if err := eng.ReactReply(run.ID, "prop", "再次确认", nil, nil, true); err != nil {
		t.Fatalf("retry finish: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

// TestReviewForceRequiresReadyAndRejectsLateRevise: force confirm retires the
// parked session when ready; a subsequent revise is rejected (conv Done).
// Busy-state reject is covered by TestReviewForceRejectedWhileQueued.
func TestReviewForceRequiresReadyAndRejectsLateRevise(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if !provider.HasLiveSession(run.ID, "prop") {
		t.Fatal("expected live parked session before force")
	}
	if err := eng.ReactReply(run.ID, "prop", "确认", nil, nil, true); err != nil {
		t.Fatalf("force: %v", err)
	}
	if provider.HasLiveSession(run.ID, "prop") {
		t.Fatal("session must be retired after review force")
	}
	waitRunStatus(t, db, run.ID, "completed")
	// Late revise after Done must be rejected.
	if err := eng.ReactReply(run.ID, "prop", "迟到修订", nil, nil, false); err == nil {
		t.Fatal("expected react already done after force")
	}
}

// TestReviewForceRejectedWhileQueued: force confirm is refused while the
// platform FIFO has pending/active work (FR4 ready gate).
func TestReviewForceRejectedWhileQueued(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	hold := make(chan struct{})
	provider.reviseHold = hold

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.ReactReply(run.ID, "prop", "改一下", nil, nil, false); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Wait until the pump has started the held turn.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, thinking := eng.ReviewSessionState(run.ID, "prop"); thinking {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	err = eng.ReactReply(run.ID, "prop", "确认", nil, nil, true)
	if err == nil || !strings.Contains(err.Error(), "Cancel") {
		t.Fatalf("expected ready-gate error, got %v", err)
	}
	close(hold)
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := eng.ReactReply(run.ID, "prop", "确认", nil, nil, true); err != nil {
		t.Fatalf("force after ready: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

// TestClarifyForceStillUsesAgentWrapUp: classic clarify「结束交互」must keep
// calling provider.ReactReply(force) — review force isolation must not regress it.
func TestClarifyForceStillUsesAgentWrapUp(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, reactOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	before := p.reactReplyCalls["clarify"]
	if err := eng.ReactReply(run.ID, "clarify", "完成", nil, nil, true); err != nil {
		t.Fatalf("clarify force: %v", err)
	}
	if p.reactReplyCalls["clarify"] != before+1 {
		t.Fatalf("clarify force must call ReactReply; got %d (before %d)",
			p.reactReplyCalls["clarify"], before)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

// arts is a tiny helper: does the run have an artifact by name?
func arts(db *gorm.DB, runID, name string) (models.Artifact, bool) {
	var a models.Artifact
	if err := db.Where("run_id = ? AND name = ?", runID, name).First(&a).Error; err != nil {
		return models.Artifact{}, false
	}
	return a, true
}

// gateReactGraph: input → research → human_gate → output.
// No review control variable: the producer's session is kept alive solely via
// hasDownstreamReactGate so the gate can issue a ReAct reject.
func gateReactGraph() models.Graph {
	return models.Graph{
		Variables: []models.Variable{
			{Name: "idea", Type: "paragraph", Ask: true, Required: true, Editable: true},
		},
		Nodes: []models.Node{
			{ID: "input", Type: "input", Label: "输入"},
			{ID: "prop", Type: "agent", Caps: capsResearchAuto, Label: "调研", Config: map[string]any{"agent_profile": "pm-agent", "prompt": "做调研"}},
			{ID: "select", Type: "human_gate", Label: "确认", Config: map[string]any{
				"title":         "确认调研",
				"body_template": "{{nodes.prop.outputs.research}}",
				"actions":       []any{map[string]any{"id": "approve", "label": "通过"}},
			}},
			{ID: "output", Type: "output", Label: "输出"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "prop"},
			{ID: "e2", Source: "prop", Target: "select"},
			{ID: "e3", Source: "select", SourceHandle: "approve", Target: "output"},
		},
	}
}

func setupGateReactEngine(t *testing.T) (*Engine, *gorm.DB, *fakeProvider) {
	t.Helper()
	db, err := database.OpenSQLiteTest(t.TempDir() + "/gate-react.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	g := gateReactGraph()
	wf := models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "gate-react-wf", Name: "gate-react-wf", Version: 1, PublishedVersion: 1, Graph: g}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: g}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	artsSvc := services.NewArtifactService(db)
	host := mcp.NewHost(artsSvc)
	provider := &fakeProvider{host: host}
	eng := New(db, provider, host, artsSvc, 5)
	cleanupEngineDB(t, eng, db)
	return eng, db, provider
}

// TestGateReactReviseInPlace: a pending human_gate can push a ReAct reject
// into the upstream producer; the gate stays pending and the producer session
// is retired only when the gate is finally resolved.
func TestGateReactReviseInPlace(t *testing.T) {
	eng, db, provider := setupGateReactEngine(t)

	run, err := eng.StartRun("gate-react-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "select")
	// Gate row can appear briefly before run status flips to waiting_human;
	// GateReactRevise requires waiting_human via loadPendingGate.
	waitRunStatus(t, db, run.ID, "waiting_human")

	pid, alive := eng.GateReactInfo(run.ID, "select")
	if pid != "prop" || !alive {
		t.Fatalf("GateReactInfo = (%q, %v), want (prop, true)", pid, alive)
	}

	anns := []models.ReactAnnotation{{JSONPath: "findings[r1]", Note: "标题更具体"}}
	if err := eng.GateReactRevise(run.ID, "select", "按标注改", nil, anns); err != nil {
		t.Fatalf("GateReactRevise: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatalf("wait gate revise: %v", err)
	}
	if provider.reviseCalls["prop"] != 1 {
		t.Fatalf("expected one ReviseInPlace on prop, got %d", provider.reviseCalls["prop"])
	}
	// Gate must still be pending after an in-place revise.
	waitGatePending(t, db, run.ID, "select")
	waitRunStatus(t, db, run.ID, "waiting_human")

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("producer review conv missing: %v", err)
	}
	if conv.Done {
		t.Fatalf("producer review conv should stay open after gate-react revise")
	}
	var sawAnn bool
	for _, m := range conv.Messages {
		if m.Role == "human" && len(m.Annotations) == 1 && m.Annotations[0].JSONPath == "findings[r1]" {
			sawAnn = true
		}
	}
	if !sawAnn {
		t.Fatalf("annotation not persisted on gate-react human turn: %+v", conv.Messages)
	}

	// Approve retires the upstream parked session.
	if err := eng.ResumeGate(run.ID, "select", "approve", nil); err != nil {
		t.Fatalf("ResumeGate: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	if !provider.retired[provider.parkKey(run.ID, "prop")] {
		t.Fatalf("expected producer session retired after gate approve")
	}
	pid, alive = eng.GateReactInfo(run.ID, "select")
	if alive {
		t.Fatalf("session should not be alive after retire, GateReactInfo=(%q, %v)", pid, alive)
	}
}

// TestDownstreamGateKeepAlive: a producer bound by a downstream human_gate
// body template keeps its session alive for a ReAct reject.
func TestDownstreamGateKeepAlive(t *testing.T) {
	eng, _, _ := setupGateReactEngine(t)
	c := &execCtx{
		graph: models.Graph{
			Nodes: []models.Node{
				{ID: "prop", Type: "agent", Caps: capsResearch},
				{ID: "select", Type: "human_gate", Config: map[string]any{"body_template": "{{nodes.prop.outputs.research}}"}},
			},
			Edges: []models.Edge{{ID: "e", Source: "prop", Target: "select"}},
		},
		run: &models.Run{ID: "r-keep"},
	}
	if !eng.hasDownstreamReactGate(c, &c.graph.Nodes[0]) {
		t.Fatalf("research → human_gate should keep-alive")
	}
}

// TestRenderReviewHuman folds annotations into the agent-facing instruction.
func TestRenderReviewHuman(t *testing.T) {
	if got := renderReviewHuman("  ", nil); got != "" {
		t.Fatalf("empty: %q", got)
	}
	anns := []models.ReactAnnotation{{JSONPath: "a.b", Note: "改这里"}}
	got := renderReviewHuman("请处理", anns)
	for _, part := range []string{"a.b", "改这里", "请处理"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
	got = renderReviewHuman("", anns)
	for _, part := range []string{"a.b", "按上述标注修改"} {
		if !strings.Contains(got, part) {
			t.Fatalf("anns-only missing %q in %q", part, got)
		}
	}
}

// TestReviewEnterPreservesUsage: completed → enterReview → saveState must keep
// production-phase usage on the paused StateRun (not drop to nil / timeline "—").
func TestReviewEnterPreservesUsage(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	provider.agentUsage = &models.TokenUsage{
		InputTokens: 100, OutputTokens: 40, CacheReadTokens: 10, CacheWriteTokens: 2,
	}
	provider.agentUsageByModel = models.TokenUsageByModel{
		"m-review": {InputTokens: 100, OutputTokens: 40, CacheReadTokens: 10, CacheWriteTokens: 2},
	}

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		t.Fatalf("load state_run: %v", err)
	}
	if sr.Status != "waiting_human" {
		t.Fatalf("status=%q want waiting_human", sr.Status)
	}
	if sr.Usage == nil {
		t.Fatal("StateRun.Usage must be preserved across enterReview pause")
	}
	if sr.Usage.InputTokens != 100 || sr.Usage.OutputTokens != 40 ||
		sr.Usage.CacheReadTokens != 10 || sr.Usage.CacheWriteTokens != 2 {
		t.Fatalf("usage mismatch: %+v", sr.Usage)
	}
	if b, ok := sr.UsageByModel["m-review"]; !ok || b.Total() != 152 {
		t.Fatalf("UsageByModel must survive enterReview, got %+v", sr.UsageByModel)
	}
	assertLedgerTotal(t, db, run.ID, "m-review", 152)
}

// assertLedgerTotal checks the token ledger mirrors the StateRun accounting.
func assertLedgerTotal(t *testing.T, db *gorm.DB, runID, modelKey string, want int64) {
	t.Helper()
	var rows []models.TokenUsageEvent
	q := db.Where("run_id = ?", runID)
	if modelKey != "" {
		q = q.Where("model_key = ?", modelKey)
	}
	if err := q.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var got int64
	for _, r := range rows {
		if r.Source != models.TokenLedgerSourceWorkflow || r.NodeID == "" {
			t.Fatalf("ledger row missing source/node: %+v", r)
		}
		got += r.Total()
	}
	if got != want {
		t.Fatalf("ledger total=%d want %d (rows=%+v)", got, want, rows)
	}
}

// TestReviewReviseFlushesTokenUsage: ReviseInPlace mid-turn usage is merged onto
// the same StateRun via flushTokenUsage (aligned with clarify resume path).
func TestReviewReviseFlushesTokenUsage(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	provider.agentUsage = &models.TokenUsage{InputTokens: 50, OutputTokens: 20}
	provider.reviseUsage = &models.TokenUsage{InputTokens: 7, OutputTokens: 3, CacheReadTokens: 1}

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.ReactReply(run.ID, "prop", "按标注改一下", nil, nil, false); err != nil {
		t.Fatalf("revise reply: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatalf("wait revise: %v", err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		t.Fatalf("load state_run: %v", err)
	}
	if sr.Usage == nil {
		t.Fatal("StateRun.Usage nil after revise flush")
	}
	if sr.Usage.InputTokens != 57 || sr.Usage.OutputTokens != 23 || sr.Usage.CacheReadTokens != 1 {
		t.Fatalf("expected agent+revise sum, got %+v", sr.Usage)
	}
	assertLedgerTotal(t, db, run.ID, "", 81)
}

// TestGateReactReviseFlushesTokenUsage: gate-react ReviseInPlace also merges
// usage onto the producer StateRun.
func TestGateReactReviseFlushesTokenUsage(t *testing.T) {
	eng, db, provider := setupGateReactEngine(t)
	provider.agentUsage = &models.TokenUsage{InputTokens: 30, OutputTokens: 10}
	provider.reviseUsage = &models.TokenUsage{InputTokens: 5, OutputTokens: 2}

	run, err := eng.StartRun("gate-react-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "select")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.GateReactRevise(run.ID, "select", "改一下", nil, nil); err != nil {
		t.Fatalf("GateReactRevise: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatalf("wait gate revise: %v", err)
	}

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		t.Fatalf("load state_run: %v", err)
	}
	if sr.Usage == nil {
		t.Fatal("producer StateRun.Usage nil after gate-react revise flush")
	}
	if sr.Usage.InputTokens != 35 || sr.Usage.OutputTokens != 12 {
		t.Fatalf("expected agent+revise sum, got %+v", sr.Usage)
	}
}
