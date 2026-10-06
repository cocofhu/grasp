package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestReconcileInterruptedOnStartup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	db, err := database.OpenSQLiteTest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// A run + node caught mid-flight when the process died.
	db.Create(&models.Run{ID: "orphan", WorkflowID: "w", WorkflowName: "w", Status: "running"})
	db.Create(&models.StateRun{RunID: "orphan", NodeID: "n", NodeType: "agent", Iteration: 1, Status: "running"})
	// A legitimately paused run must be left alone.
	db.Create(&models.Run{ID: "paused", WorkflowID: "w", WorkflowName: "w", Status: "waiting_human"})

	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	eng := New(db, &fakeProvider{host: host}, host, arts, 5) // triggers reconcileInterrupted
	cleanupEngineDB(t, eng, db)

	var orphan, paused models.Run
	db.First(&orphan, "id = ?", "orphan")
	db.First(&paused, "id = ?", "paused")
	if orphan.Status != "failed" {
		t.Errorf("orphan run status = %q, want failed", orphan.Status)
	}
	if paused.Status != "waiting_human" {
		t.Errorf("paused run must stay resumable, got %q", paused.Status)
	}
	var sr models.StateRun
	db.First(&sr, "run_id = ?", "orphan")
	if sr.Status != "failed" {
		t.Errorf("mid-flight node state = %q, want failed", sr.Status)
	}
}

func TestReactReplyErrorPaths(t *testing.T) {
	eng, _ := setupEngine(t)
	// Unknown run.
	if err := eng.ReactReply("nope", "clarify", "x", nil, nil, false); err == nil {
		t.Error("expected error for unknown run")
	}
	// Known run but wrong node type / no conversation.
	run, _ := eng.StartRun("clarify-to-design", map[string]any{"idea": "x"}, "test")
	if err := eng.ReactReply(run.ID, "design", "x", nil, nil, false); err == nil {
		t.Error("expected error replying to a non-react node")
	}
	// Gate errors: resume unknown run / non-gate node.
	if err := eng.ResumeGate("nope", "approve", "approve", nil); err == nil {
		t.Error("expected error for unknown run gate")
	}
	if err := eng.ResumeGate(run.ID, "design", "approve", nil); err == nil {
		t.Error("expected error resuming a non-gate node")
	}
}

// TestSetVarBranchImplementPipeline exercises set_var (expr eval + literal
// fallback), branch routing (a matched case goto), the implement node's
// structured contract + branch export, and the output result template.
func TestSetVarBranchImplementPipeline(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "sv", Type: "set_var", Config: map[string]any{"assignments": []any{
				map[string]any{"var": "x", "expr": "1 + 2"},      // evaluates -> 3
				map[string]any{"var": "note", "expr": "hello ("}, // parse error -> literal
				map[string]any{"expr": "ignored"},                // no var name -> skipped
			}}},
			{ID: "br", Type: "branch", Config: map[string]any{"cases": []any{
				map[string]any{"when": "vars.x == 3", "id": "impl"},
			}}},
			{ID: "impl", Type: "agent", Caps: capsImplement, Config: map[string]any{"prompt": "build", "agent_profile": "dev"}},
			{ID: "output", Type: "output", Config: map[string]any{"result": "done x={{vars.x}} branch={{vars.branches}}"}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "sv"},
			{ID: "e2", Source: "sv", Target: "br"},
			{ID: "e3", Source: "br", Target: "impl", SourceHandle: "impl"},
			{ID: "e4", Source: "impl", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// set_var results persisted.
	var xv, note models.RunVariable
	db.Where("run_id = ? AND name = ?", run.ID, "x").First(&xv)
	if fmt.Sprint(xv.Value) != "3" {
		t.Errorf("x = %v, want 3", xv.Value)
	}
	db.Where("run_id = ? AND name = ?", run.ID, "note").First(&note)
	if fmt.Sprint(note.Value) != "hello (" {
		t.Errorf("note literal fallback = %v", note.Value)
	}
	// implement exported its working branches to the `branches` global var.
	var br models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "branches").First(&br).Error; err != nil {
		t.Errorf("branches var not exported: %v", err)
	}
}

// TestGateFormValidation exercises validateGateForm: a required form field (and
// an action that requires the whole form) blocks resume until filled, then the
// field values persist as run variables.
func TestGateFormValidationRequired(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title": "审批",
				"actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
					map[string]any{"id": "reject", "label": "拒绝", "requireForm": true},
				},
				"form": []any{
					map[string]any{"key": "comment", "label": "评论", "required": true},
				},
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "output", SourceHandle: "approve"},
			{ID: "e3", Source: "gate", Target: "output", SourceHandle: "reject"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")

	// Resume with a blank required field -> rejected, gate stays pending.
	if err := eng.ResumeGate(run.ID, "gate", "approve", map[string]any{"comment": "  "}); err == nil {
		t.Fatal("expected required-field validation error")
	}
	// Now fill it -> completes and the value persists.
	if err := eng.ResumeGate(run.ID, "gate", "approve", map[string]any{"comment": "看起来不错"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var v models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "comment").First(&v).Error; err != nil {
		t.Errorf("form field not persisted: %v", err)
	}
}

// TestFinishWaitingHumanDoesNotClobberResume covers the race where a gate/
// react pause's finish("waiting_human") lands after ResumeGate already queued
// or completed the run. An unconditional status write would strand the run.
func TestFinishWaitingHumanDoesNotClobberResume(t *testing.T) {
	eng, db := setupEngine(t)
	// Halt so the live dispatcher cannot claim fixture runs we park as queued.
	eng.Halt()

	t.Run("from_running_pauses", func(t *testing.T) {
		id := "finish-race-running"
		if err := db.Create(&models.Run{
			ID: id, WorkflowID: "w", WorkflowName: "w", Status: "running",
		}).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
		eng.finish(id, "waiting_human")
		var got models.Run
		if err := db.First(&got, "id = ?", id).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.Status != "waiting_human" {
			t.Fatalf("status = %q, want waiting_human", got.Status)
		}
	})

	for _, before := range []string{"queued", "completed", "cancelled", "failed"} {
		t.Run("preserve_"+before, func(t *testing.T) {
			id := "finish-race-" + before
			// Graph is required for queued fixtures: a dispatcher that races past
			// Halt would otherwise claimNextQueued → StartNode nil → finish(failed).
			if err := db.Create(&models.Run{
				ID: id, WorkflowID: "w", WorkflowName: "w", Status: before,
				Graph: testClarifyGraph(),
			}).Error; err != nil {
				t.Fatalf("create: %v", err)
			}
			eng.finish(id, "waiting_human")
			var got models.Run
			if err := db.First(&got, "id = ?", id).Error; err != nil {
				t.Fatalf("reload: %v", err)
			}
			if got.Status != before {
				t.Fatalf("status = %q, want preserved %q", got.Status, before)
			}
		})
	}
}

// TestPauseStillPendingRespectsResolvedGate ensures a late pause unwind does
// not apply waiting_human after ResumeGate resolved the gate (CI flake class).
func TestPauseStillPendingRespectsResolvedGate(t *testing.T) {
	eng, db := setupEngine(t)
	eng.Halt()
	runID := "pause-pending-" + t.Name()
	if err := db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "queued",
		Graph: testClarifyGraph(),
	}).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := db.Create(&models.Gate{
		RunID: runID, NodeID: "gate", Iteration: 1, Title: "审批", Resolved: true,
	}).Error; err != nil {
		t.Fatalf("create gate: %v", err)
	}
	node := &models.Node{ID: "gate", Type: "human_gate"}
	if eng.pauseStillPending(runID, node) {
		t.Fatal("resolved gate should not still be pending")
	}
	// Departing driver's late finish must not clobber the resume's queued status.
	eng.finish(runID, "waiting_human")
	var got models.Run
	if err := db.First(&got, "id = ?", runID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Status != "queued" {
		t.Fatalf("status = %q, want queued", got.Status)
	}
}

func TestGateFormValidationImagesOnly(t *testing.T) {
	gate := models.Gate{
		Actions: []models.GateAction{{ID: "approve", Label: "批准"}},
		Form:    []models.GateField{{Key: "comment", Label: "评论", Required: true}},
	}
	onlyImages := map[string]any{
		"comment": map[string]any{
			"text":   "",
			"images": []any{map[string]any{"data": "abc", "mimeType": "image/png"}},
		},
	}
	if err := validateGateForm(gate, "approve", onlyImages); err != nil {
		t.Fatalf("images-only required field should pass: %v", err)
	}
}

// liveEventProvider wraps fakeProvider to also implement
// runtime.LiveEventSource so LiveNodeEvents' happy branch is exercised.
type liveEventProvider struct{ *fakeProvider }

func (liveEventProvider) LiveNodeEvents(ctx context.Context, runID, nodeID string) ([]models.AcpEvent, bool, error) {
	return []models.AcpEvent{{Kind: "message", Text: "live"}}, true, nil
}

func (liveEventProvider) LiveNodeEventsPage(ctx context.Context, runID, nodeID, cursor string, limit int) ([]models.AcpEvent, string, bool, bool, error) {
	return []models.AcpEvent{{Kind: "message", Text: "page"}}, "next", false, true, nil
}

func TestLiveNodeEventsPageAndProjectVarsLookup(t *testing.T) {
	eng, _, p := setupEngineGraphP(t, reactOnlyGraph())
	eng.SetProjectVarsLookup(func(workflowID string) []models.ProjectVariable {
		return []models.ProjectVariable{{Name: "seed", Value: "v"}}
	})
	eng.provider = liveEventProvider{p}
	ev, cur, more, ok, err := eng.LiveNodeEventsPage(context.Background(), "r", "n", "", 10)
	if err != nil || !ok || cur != "next" || more || len(ev) != 1 {
		t.Fatalf("page events: ok=%v cur=%q more=%v ev=%v err=%v", ok, cur, more, ev, err)
	}
}

func TestLiveNodeEventsAndPublishAcp(t *testing.T) {
	eng, _, p := setupEngineGraphP(t, reactOnlyGraph())

	// Default fakeProvider does not implement LiveEventSource -> ok=false.
	if _, ok, err := eng.LiveNodeEvents(context.Background(), "r", "n"); ok || err != nil {
		t.Errorf("plain provider should not be a live source, got ok=%v err=%v", ok, err)
	}
	// Swap in a provider that does implement it -> the live branch returns data.
	eng.provider = liveEventProvider{p}
	ev, ok, err := eng.LiveNodeEvents(context.Background(), "r", "n")
	if err != nil || !ok || len(ev) == 0 {
		t.Fatalf("live events: ok=%v ev=%v err=%v", ok, ev, err)
	}

	// publishAcp delivers to a run subscriber.
	ch, unsub := eng.Broker().Subscribe("run-x")
	defer unsub()
	eng.publishAcp("run-x", "node-1", []models.AcpEvent{{Kind: "message", Text: "hi"}}, true)
	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), "node-1") {
			t.Errorf("published msg = %s", msg)
		}
	default:
		t.Fatal("publishAcp delivered nothing")
	}
}

// TestCaptureDeliverableNoProduces: a plain agent node that declares no
// produces has its primary output auto-captured as <node>.md.
func TestCaptureDeliverableNoProduces(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "work", Type: "agent", Caps: capsPlain, Config: map[string]any{"prompt": "做点事"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "work"},
			{ID: "e2", Source: "work", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var c int64
	db.Model(&models.Artifact{}).Where("run_id = ? AND name = ?", run.ID, "work.md").Count(&c)
	if c == 0 {
		t.Error("expected auto-captured work.md deliverable")
	}
}

func reactOnlyGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "clarify", Type: "agent", Caps: capsClarify, Config: map[string]any{"agent_profile": "pm", "prompt": "澄清"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "clarify"},
			{ID: "e2", Source: "clarify", Target: "output"},
		},
	}
}

// TestReactMultiRound: a non-confirm reply keeps the dialogue open; the
// confirm reply finishes the node.
func TestReactMultiRound(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, reactOnlyGraph())
	p.reactPending = 1
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")

	// First reply -> agent still asking (Done:false); run stays paused.
	if err := eng.ReactReply(run.ID, "clarify", "第一轮", nil, nil, false); err != nil {
		t.Fatalf("reply 1: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait after reply 1: %v", err)
	}
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").Order("iteration desc").First(&conv)
	if conv.Done {
		t.Fatal("conversation should still be open after round 1")
	}
	// Ensure the follow-up pause is visible before the finishing reply, so we
	// do not race a late finish("waiting_human") from the opening turn.
	waitReactPause(t, db, run.ID, "clarify")
	waitRunStatus(t, db, run.ID, "waiting_human")
	// Second reply confirms -> the node finishes; run completes.
	if err := eng.ReactReply(run.ID, "clarify", "第二轮", nil, nil, true); err != nil {
		t.Fatalf("reply 2: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// Replying to a done conversation is rejected.
	if err := eng.ReactReply(run.ID, "clarify", "再来", nil, nil, false); err == nil {
		t.Error("expected 'react already done' error")
	}
}

// TestReactContractMiss: the react agent finishes without writing its reserved
// clarified_requirement.json, so the produces contract fails and, with no
// failure edge, the run fails.
func TestReactContractMiss(t *testing.T) {
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
}

func TestBrokerGetterLiveEventsAndPublishAcp(t *testing.T) {
	eng, _, _ := setupEngineGraphP(t, researchContractGraph())
	if eng.Broker() == nil {
		t.Fatal("Broker() nil")
	}
	// fakeProvider is not a LiveEventSource -> ok=false.
	if _, ok, err := eng.LiveNodeEvents(context.Background(), "r", "n"); ok || err != nil {
		t.Errorf("expected LiveNodeEvents ok=false err=nil for non-live provider, got ok=%v err=%v", ok, err)
	}
	// publishAcp pushes onto a run subscriber.
	ch, unsub := eng.Broker().Subscribe("run-x")
	defer unsub()
	eng.publishAcp("run-x", "node-y", []models.AcpEvent{{Kind: "message", Text: "hi"}}, true)
	select {
	case msg := <-ch:
		if len(msg) == 0 {
			t.Error("empty acp message")
		}
	default:
		t.Error("publishAcp did not deliver to subscriber")
	}
}
