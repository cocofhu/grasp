package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

var errReviseBoom = errors.New("revise boom")

// TestReviewReplySyncsOutputsAndBodyMd: reviewReply(!force) after writing page.html
// must refresh StateRun.outputs.page and pending gate BodyMd to match the live store.
func TestReviewReplySyncsOutputsAndBodyMd(t *testing.T) {
	db, err := database.OpenSQLiteTest(t.TempDir() + "/visual-review-sync.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	g := models.Graph{
		Variables: []models.Variable{{Name: "review", Type: "bool", Value: true}},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "page", Type: "visual", Config: map[string]any{}},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":         "审阅",
				"body_template": "{{nodes.page.outputs.page}}",
				"actions":       []any{map[string]any{"id": "approve", "label": "批准"}},
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "page"},
			{ID: "e2", Source: "page", Target: "output"},
		},
	}
	wf := models.WorkflowDef{ID: "vr-sync", Name: "vr-sync", Status: "published", Version: 1, Graph: g}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: g}).Error; err != nil {
		t.Fatal(err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	provider := &fakeProvider{
		host: host,
		structuredBodySeq: map[string][]string{
			"page": {"<!doctype html><html><body><h1>v2</h1></body></html>"},
		},
	}
	eng := New(db, provider, host, arts, 5)
	cleanupEngineDB(t, eng, db)

	run, err := eng.StartRun("vr-sync", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "page")
	waitRunStatus(t, db, run.ID, "waiting_human")

	var srBefore models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "page").First(&srBefore).Error; err != nil {
		t.Fatalf("load state: %v", err)
	}
	pageBefore, _ := srBefore.Outputs["page"].(string)
	if !strings.Contains(pageBefore, "demo") {
		t.Fatalf("expected initial demo page in outputs, got %q", pageBefore)
	}

	// Pending gate bound to page: reviewReply must re-interpolate BodyMd (not only outputs).
	now := time.Now()
	if err := db.Create(&models.Gate{
		RunID: run.ID, NodeID: "gate", Iteration: 1, Title: "审阅", RequestedAt: now,
		UpstreamNodeID: "page", UpstreamIteration: srBefore.Iteration, BodyMd: pageBefore,
		Actions: []models.GateAction{{ID: "approve", Label: "批准"}},
	}).Error; err != nil {
		t.Fatalf("seed pending gate: %v", err)
	}

	if err := eng.ReactReply(run.ID, "page", "改标题", nil, nil, false); err != nil {
		t.Fatalf("revise: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "page", 5*time.Second); err != nil {
		t.Fatalf("wait revise: %v", err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if provider.reviseCalls["page"] != 1 {
		t.Fatalf("reviseCalls=%d", provider.reviseCalls["page"])
	}

	storePage, ok := arts.Get(run.ID, visualPageName)
	if !ok || !strings.Contains(storePage, "v2") {
		t.Fatalf("store page.html not revised: ok=%v content=%q", ok, storePage)
	}
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "page").First(&sr).Error; err != nil {
		t.Fatalf("load state after revise: %v", err)
	}
	pageOut, _ := sr.Outputs["page"].(string)
	if pageOut != storePage {
		t.Fatalf("outputs.page != store after reviewReply sync:\noutputs=%q\nstore=%q", pageOut, storePage)
	}
	var gate models.Gate
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "gate").First(&gate).Error; err != nil {
		t.Fatalf("load gate after revise: %v", err)
	}
	if gate.BodyMd != storePage {
		t.Fatalf("BodyMd not refreshed after reviewReply:\nbody=%q\nstore=%q", gate.BodyMd, storePage)
	}
}

// TestWriteArtifactSyncsOutputsAndPendingBodyMd covers the MCP WriteArtifact hook:
// writing page.html during waiting_human syncs outputs and pending gate BodyMd.
func TestWriteArtifactSyncsOutputsAndPendingBodyMd(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-visual-body"
	now := time.Now()
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "page", Type: "visual"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":         "审阅",
				"body_template": "{{nodes.page.outputs.page}}",
				"actions":       []any{map[string]any{"id": "approve", "label": "批准"}},
			}},
		},
	}
	oldHTML := "<!doctype html><html><body>old</body></html>"
	newHTML := "<!doctype html><html><body>new-live</body></html>"
	if err := db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "waiting_human",
		Graph: g, StartedAt: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "page", NodeType: "visual", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"page": oldHTML},
	})
	db.Create(&models.Gate{
		RunID: runID, NodeID: "gate", Iteration: 1, Title: "审阅", RequestedAt: now,
		UpstreamNodeID: "page", UpstreamIteration: 1, BodyMd: oldHTML,
		Actions: []models.GateAction{{ID: "approve", Label: "批准"}},
	})
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, "page", "visual")
	eng.host.SetActiveReview(runID, true)

	if _, err := eng.host.WriteArtifact(runID, tok, "page", visualPageName, newHTML, "html"); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", runID, "page").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := sr.Outputs["page"].(string); got != newHTML {
		t.Fatalf("outputs.page not synced: %q", got)
	}
	if _, ok := sr.Outputs["page_history"]; ok {
		t.Fatalf("page_history must not be written: %v", sr.Outputs["page_history"])
	}

	newerHTML := "<!doctype html><html><body>newer-live</body></html>"
	if _, err := eng.host.WriteArtifact(runID, tok, "page", visualPageName, newerHTML, "html"); err != nil {
		t.Fatalf("WriteArtifact second: %v", err)
	}
	if err := db.Where("run_id = ? AND node_id = ?", runID, "page").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := sr.Outputs["page"].(string); got != newerHTML {
		t.Fatalf("outputs.page after second write: %q", got)
	}
	if _, ok := sr.Outputs["page_history"]; ok {
		t.Fatalf("page_history must not be written after second overwrite: %v", sr.Outputs["page_history"])
	}
	var gate models.Gate
	if err := db.Where("run_id = ? AND node_id = ?", runID, "gate").First(&gate).Error; err != nil {
		t.Fatal(err)
	}
	if gate.BodyMd != newerHTML {
		t.Fatalf("BodyMd not refreshed: %q", gate.BodyMd)
	}
}

// TestWriteArtifactFailureDoesNotClearOutputs: a failed Save must not wipe prior
// successful outputs (hook only runs after successful Save).
func TestWriteArtifactFailureDoesNotClearOutputs(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-write-fail"
	now := time.Now()
	oldHTML := "<!doctype html><html><body>keep-me</body></html>"
	g := models.Graph{Nodes: []models.Node{{ID: "page", Type: "visual"}}}
	db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "waiting_human",
		Graph: g, StartedAt: now, CreatedAt: now,
	})
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "page", NodeType: "visual", Iteration: 1, Status: "waiting_human",
		Outputs: map[string]any{"page": oldHTML},
	})
	if _, err := eng.store.Save(runID, "page", visualPageName, "html", oldHTML); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.host.WriteArtifact(runID, "bad-token", "page", visualPageName, "<html>x</html>", "html"); err == nil {
		t.Fatal("expected unauthorized error")
	}
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", runID, "page").First(&sr)
	if got, _ := sr.Outputs["page"].(string); got != oldHTML {
		t.Fatalf("outputs overwritten on failed write: %q", got)
	}
	if content, ok := eng.store.Get(runID, visualPageName); !ok || content != oldHTML {
		t.Fatalf("store overwritten on failed write: ok=%v %q", ok, content)
	}
}

// TestSyncAfterPrimaryArtifactWriteSkipsNonPrimaryAndNonWaiting covers hook scope:
// freeform artifacts and non-waiting runs must not rewrite StateRun outputs.
func TestSyncAfterPrimaryArtifactWriteSkipsNonPrimaryAndNonWaiting(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-sync-skip"
	now := time.Now()
	old := "<html>keep</html>"
	g := models.Graph{Nodes: []models.Node{{ID: "page", Type: "visual"}}}
	db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "running",
		Graph: g, StartedAt: now, CreatedAt: now,
	})
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "page", NodeType: "visual", Iteration: 1, Status: "running",
		Outputs: map[string]any{"page": old},
	})
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveNode(runID, "page", "visual")

	// Non-primary name: store updates, outputs untouched.
	if _, err := eng.host.WriteArtifact(runID, tok, "page", "notes.md", "hello", "markdown"); err != nil {
		t.Fatal(err)
	}
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", runID, "page").First(&sr)
	if got, _ := sr.Outputs["page"].(string); got != old {
		t.Fatalf("non-primary write mutated outputs: %q", got)
	}

	// Primary page.html while run is still "running" (not review / waiting_human): no sync.
	if _, err := eng.host.WriteArtifact(runID, tok, "page", visualPageName, "<html>new</html>", "html"); err != nil {
		t.Fatal(err)
	}
	db.Where("run_id = ? AND node_id = ?", runID, "page").First(&sr)
	if got, _ := sr.Outputs["page"].(string); got != old {
		t.Fatalf("running-state write should not sync outputs: %q", got)
	}
}

// TestSyncAfterPrimaryArtifactWriteStructuredProduct syncs research.json outputs
// (raw JSON + rendered markdown) for a waiting_human gate.
func TestSyncAfterPrimaryArtifactWriteStructuredProduct(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-sync-research"
	now := time.Now()
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "research", Type: "research"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":         "审阅",
				"body_template": "{{nodes.research.outputs.research}}",
				"actions":       []any{map[string]any{"id": "approve", "label": "批准"}},
			}},
		},
	}
	oldJSON := `{"summary":"old","findings":[{"title":"f1"}]}`
	newJSON := `{"summary":"new-live","findings":[{"title":"f2","detail":"d"}]}`
	db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "waiting_human",
		Graph: g, StartedAt: now, CreatedAt: now,
	})
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "research", NodeType: "research", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"research_json": oldJSON, "research": "old-md"},
	})
	db.Create(&models.Gate{
		RunID: runID, NodeID: "gate", Iteration: 1, Title: "审阅", RequestedAt: now,
		UpstreamNodeID: "research", UpstreamIteration: 1, BodyMd: "old-md",
		Actions: []models.GateAction{{ID: "approve", Label: "批准"}},
	})
	tok := eng.host.RegisterRun(runID)
	eng.host.SetActiveReview(runID, true)
	eng.host.SetActiveNode(runID, "research", "research")

	if _, err := eng.host.WriteArtifact(runID, tok, "research", mcp.ResearchArtifactName, newJSON, "json"); err != nil {
		t.Fatal(err)
	}
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", runID, "research").First(&sr)
	if got, _ := sr.Outputs["research_json"].(string); got != newJSON {
		t.Fatalf("research_json not synced: %q", got)
	}
	if md, _ := sr.Outputs["research"].(string); !strings.Contains(md, "new-live") {
		t.Fatalf("research markdown not rendered: %q", md)
	}
	var gate models.Gate
	db.Where("run_id = ? AND node_id = ?", runID, "gate").First(&gate)
	if !strings.Contains(gate.BodyMd, "new-live") {
		t.Fatalf("BodyMd not refreshed from research: %q", gate.BodyMd)
	}
}

// TestStructuredRenderForArtifactCoversKnownNames exercises the render switch.
func TestStructuredRenderForArtifactCoversKnownNames(t *testing.T) {
	for _, name := range []string{
		mcp.ResearchArtifactName,
		mcp.ProposalsArtifactName,
		mcp.PlanArtifactName,
		mcp.ReviewArtifactName,
		mcp.TestResultArtifactName,
		mcp.ClarifiedRequirementArtifactName,
		mcp.ImplementationResultArtifactName,
		mcp.ProposalArtifactName,
	} {
		if structuredRenderForArtifact(name) == nil {
			t.Fatalf("expected renderer for %s", name)
		}
	}
	if structuredRenderForArtifact("unknown.bin") != nil {
		t.Fatal("unknown artifact should have no renderer")
	}
}

// TestReviewReplyFailureDoesNotSyncOutputs: ReviseInPlace error must not refresh
// outputs when the turn skipped writing a new product. Failure is surfaced as
// Interrupted=true + turn_done{interrupted:true} so UI never shows Done/已完成.
func TestReviewReplyFailureDoesNotSyncOutputs(t *testing.T) {
	db, err := database.OpenSQLiteTest(t.TempDir() + "/review-fail.db")
	if err != nil {
		t.Fatal(err)
	}
	g := models.Graph{
		Variables: []models.Variable{{Name: "review", Type: "bool", Value: true}},
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "page", Type: "visual"},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "page"},
			{ID: "e2", Source: "page", Target: "output"},
		},
	}
	wf := models.WorkflowDef{ID: "vr-fail", Name: "vr-fail", Status: "published", Version: 1, Graph: g}
	db.Create(&wf)
	db.Create(&models.WorkflowVersion{WorkflowID: wf.ID, Version: 1, Graph: g})
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	provider := &fakeProvider{host: host, reviseErr: errReviseBoom, reviseSkipWrite: true}
	eng := New(db, provider, host, arts, 5)
	cleanupEngineDB(t, eng, db)

	run, err := eng.StartRun("vr-fail", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "page")
	// Conversation can open before saveState finishes unwinding; wait until the
	// pause is fully committed so the baseline outputs snapshot is stable.
	waitRunStatus(t, db, run.ID, "waiting_human")

	var srBefore models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "page").First(&srBefore).Error; err != nil {
		t.Fatalf("load state: %v", err)
	}
	before, _ := srBefore.Outputs["page"].(string)
	if !strings.Contains(before, "demo") {
		t.Fatalf("expected initial demo page in outputs, got %q", before)
	}

	ch, unsub := eng.Broker().Subscribe(run.ID)
	defer unsub()
	gotTurnDone := make(chan bool, 1)
	go func() {
		deadline := time.After(5 * time.Second)
		for {
			select {
			case <-deadline:
				gotTurnDone <- false
				return
			case raw, ok := <-ch:
				if !ok {
					gotTurnDone <- false
					return
				}
				var frame map[string]any
				if json.Unmarshal(raw, &frame) != nil {
					continue
				}
				if frame["type"] != "review" || frame["event"] != "turn_done" {
					continue
				}
				interrupted, _ := frame["interrupted"].(bool)
				gotTurnDone <- interrupted
				return
			}
		}
	}()

	if err := eng.ReactReply(run.ID, "page", "改", nil, nil, false); err != nil {
		t.Fatalf("revise reply should not fail HTTP: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "page", 5*time.Second); err != nil {
		t.Fatalf("wait revise: %v", err)
	}

	select {
	case interrupted := <-gotTurnDone:
		if !interrupted {
			t.Fatal("expected turn_done with interrupted:true on revise failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for turn_done on revise failure")
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "page").
		Order("id desc").First(&conv).Error; err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	var sawFailedAgent bool
	for _, m := range conv.Messages {
		if m.Role != "agent" || !strings.Contains(m.Text, "就地修改失败") {
			continue
		}
		sawFailedAgent = true
		if !m.Interrupted {
			t.Fatalf("failed revise agent message must be Interrupted=true, got %+v", m)
		}
	}
	if !sawFailedAgent {
		t.Fatalf("expected agent failure message in conversation, got %+v", conv.Messages)
	}

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "page").First(&sr).Error; err != nil {
		t.Fatalf("load state after revise: %v", err)
	}
	after, _ := sr.Outputs["page"].(string)
	if after != before {
		t.Fatalf("outputs.page changed on revise failure: before=%q after=%q", before, after)
	}

	// Session stays parked/retryable: enqueue again must still succeed.
	if err := eng.ReactReply(run.ID, "page", "再试", nil, nil, false); err != nil {
		t.Fatalf("retry after revise failure should still enqueue: %v", err)
	}
}
