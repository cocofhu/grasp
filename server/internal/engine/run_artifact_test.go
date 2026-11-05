package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

func seedEditRun(t *testing.T, db *gorm.DB, runID, status string) {
	t.Helper()
	now := time.Now()
	g := models.Graph{Nodes: []models.Node{
		{ID: "input", Type: "input"},
		{ID: "clarify", Type: "agent", Caps: capsClarify},
		{ID: "test", Type: "agent", Caps: capsTest},
	}}
	if err := db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: status, Graph: g, StartedAt: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestArtifactEditStateMatrix(t *testing.T) {
	eng, db := setupEngine(t)
	cases := map[string]ArtifactEditState{
		"failed":        {Editable: true},
		"waiting_human": {Editable: true},
		"running":       {Reason: ArtifactEditReasonRunning},
		"queued":        {Reason: ArtifactEditReasonRunning},
		"completed":     {Reason: ArtifactEditReasonEnded},
		"cancelled":     {Reason: ArtifactEditReasonEnded},
	}
	for status, want := range cases {
		runID := "run-edit-state-" + status
		seedEditRun(t, db, runID, status)
		if got := eng.ArtifactEditState(runID); got != want {
			t.Fatalf("%s: got %+v want %+v", status, got, want)
		}
	}
	if got := eng.ArtifactEditState("run-missing"); got.Editable {
		t.Fatalf("missing run editable: %+v", got)
	}
}

func TestSaveRunArtifactRejectsNonEditableRuns(t *testing.T) {
	eng, db := setupEngine(t)
	for status, code := range map[string]string{
		"running":   ArtifactEditCodeRunNotEditable,
		"completed": ArtifactEditCodeRunNotEditable,
		"cancelled": ArtifactEditCodeRunNotEditable,
	} {
		runID := "run-edit-deny-" + status
		seedEditRun(t, db, runID, status)
		if _, err := eng.store.Save(runID, "clarify", "notes.md", "markdown", "v1"); err != nil {
			t.Fatal(err)
		}
		_, err := eng.SaveRunArtifact(runID, "notes.md", "v2", "", "alice")
		ae, ok := AsArtifactEditError(err)
		if !ok || ae.Code != code {
			t.Fatalf("%s: err=%v", status, err)
		}
		if content, _ := eng.store.Get(runID, "notes.md"); content != "v1" {
			t.Fatalf("%s: content changed to %q", status, content)
		}
	}
	if _, err := eng.SaveRunArtifact("run-nope", "notes.md", "x", "", ""); err == nil {
		t.Fatal("missing run should fail")
	}
}

func TestSaveRunArtifactRejectsBusySession(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	hold := make(chan struct{})
	provider.reactHold = hold
	provider.reactPending = 10

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		provider.mu.Lock()
		provider.reactHold = nil
		provider.mu.Unlock()
		close(hold)
		if err := eng.waitReviewReadyForTest(run.ID, "clarify", waitPollTimeout); err != nil {
			t.Error(err)
		}
		waitDriversIdle(t, eng)
	})
	waitReactPause(t, db, run.ID, "clarify")
	// The conversation exists before the opening driver parks the run. Artifact
	// editing requires the run's committed pause, not just an open conversation.
	waitRunStatus(t, db, run.ID, "waiting_human")
	waitDriversIdle(t, eng)
	if _, err := eng.store.Save(run.ID, "clarify", "notes.md", "markdown", "v1"); err != nil {
		t.Fatal(err)
	}
	if got := eng.ArtifactEditState(run.ID); !got.Editable {
		t.Fatalf("idle session should be editable: %+v", got)
	}

	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "第一条", nil, nil); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitAdmissionUntil(t, func() bool {
		_, thinking := eng.ReviewSessionState(run.ID, "clarify")
		return thinking
	}, waitPollTimeout)
	if got := eng.ArtifactEditState(run.ID); got != (ArtifactEditState{Reason: ArtifactEditReasonSessionBusy}) {
		t.Fatalf("busy session: got %+v", got)
	}
	_, err = eng.SaveRunArtifact(run.ID, "notes.md", "v2", "", "alice")
	if ae, ok := AsArtifactEditError(err); !ok || ae.Code != ArtifactEditCodeSessionBusy {
		t.Fatalf("busy save: err=%v", err)
	}
	if content, _ := eng.store.Get(run.ID, "notes.md"); content != "v1" {
		t.Fatalf("busy save changed content to %q", content)
	}
}

func TestSaveRunArtifactRevisionsOwnershipAndSync(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-edit-save"
	seedEditRun(t, db, runID, "waiting_human")
	plan := `{"goals":[{"title":"A","subgoals":[{"title":"a1"}]},{"title":"B","subgoals":[{"title":"b1"}]}]}`
	db.Create(&models.StateRun{RunID: runID, NodeID: "clarify", NodeType: "agent", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"plan_json": plan}})
	db.Create(&models.StateRun{RunID: runID, NodeID: "test", NodeType: "agent", Iteration: 1, Status: "waiting_human"})
	if _, err := eng.store.Save(runID, "clarify", mcp.PlanArtifactName, "json", plan); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := eng.store.Save(runID, "clarify", mcp.PlanArtifactName, "json", plan+strings.Repeat(" ", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	start, _ := eng.artifactMeta(runID, mcp.PlanArtifactName)
	if start.Revision != 3 {
		t.Fatalf("seed revision=%d", start.Revision)
	}
	etag := ArtifactETag(start.Content, start.SizeBytes, start.UpdatedAt)

	edited := `{"goals":[{"title":"A","subgoals":[{"title":"a1"}]}]}`
	res, err := eng.SaveRunArtifact(runID, mcp.PlanArtifactName, edited, etag, "alice")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.Changed || res.Revision != 4 || res.NodeID != "clarify" || res.ETag == "" {
		t.Fatalf("res=%+v", res)
	}
	if strings.Contains(res.Content, `"B"`) || !strings.Contains(res.Content, `"g1"`) {
		t.Fatalf("normalized content=%s", res.Content)
	}
	for _, info := range eng.store.List(runID) {
		if info.Name == mcp.PlanArtifactName && info.Node != "clarify" {
			t.Fatalf("owner changed to %q", info.Node)
		}
	}
	if v, ok := eng.store.(interface {
		GetVersion(string, int) (models.ArtifactVersion, bool)
	}); ok {
		if old, ok := v.GetVersion(res.ID, 3); !ok || !strings.Contains(old.Content, `"B"`) {
			t.Fatalf("v3 not archived: %+v", old)
		}
	}

	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", runID, "clarify").First(&sr)
	if got, _ := sr.Outputs["plan_json"].(string); got != res.Content {
		t.Fatalf("outputs not synced: %v", sr.Outputs["plan_json"])
	}
	if md, _ := sr.Outputs["plan"].(string); !strings.Contains(md, "A") {
		t.Fatalf("plan markdown not synced: %q", md)
	}

	var run models.Run
	db.First(&run, "id = ?", runID)
	var trace *models.TraceEntry
	for i := range run.Trace {
		if run.Trace[i].Event == "artifact_edit" {
			trace = &run.Trace[i]
		}
	}
	if trace == nil || trace.NodeID != "clarify" ||
		!strings.Contains(trace.Detail, "name=plan.json") || !strings.Contains(trace.Detail, "revision=4") ||
		!strings.Contains(trace.Detail, "reviewer=alice") {
		t.Fatalf("trace=%+v", run.Trace)
	}

	// Same content after normalization: no new version.
	same, err := eng.SaveRunArtifact(runID, mcp.PlanArtifactName, edited, "", "alice")
	if err != nil || same.Changed || same.Revision != 4 {
		t.Fatalf("unchanged save: res=%+v err=%v", same, err)
	}

	// Stale If-Match: conflict carries live revision and nothing changes.
	_, err = eng.SaveRunArtifact(runID, mcp.PlanArtifactName, plan, etag, "bob")
	ae, ok := AsArtifactEditError(err)
	if !ok || ae.Code != ArtifactEditCodeConflict || ae.Revision != 4 {
		t.Fatalf("conflict err=%v", err)
	}

	// Validation failure: field path surfaced, revision unchanged.
	_, err = eng.SaveRunArtifact(runID, mcp.PlanArtifactName, `{"goals":[]}`, "", "alice")
	ae, ok = AsArtifactEditError(err)
	if !ok || ae.Code != ArtifactEditCodeInvalid || ae.Field != "goals" {
		t.Fatalf("invalid err=%v (%+v)", err, ae)
	}

	again, err := eng.SaveRunArtifact(runID, mcp.PlanArtifactName, plan, "", "alice")
	if err != nil || again.Revision != 5 {
		t.Fatalf("second edit: res=%+v err=%v", again, err)
	}
}

func TestSaveRunArtifactRejectsReadonlyNames(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-edit-readonly"
	seedEditRun(t, db, runID, "failed")
	if _, err := eng.store.Save(runID, "test", "shot.png", "image", "aGVsbG8="); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.store.Save(runID, "clarify", mcp.FeedbackIndexArtifactName, "json", "{}"); err != nil {
		t.Fatal(err)
	}
	for name, code := range map[string]string{
		"shot.png":                    ArtifactEditCodeInvalid,
		mcp.FeedbackIndexArtifactName: ArtifactEditCodeInvalid,
		"../etc/passwd":               ArtifactEditCodeInvalid,
		"plan.json#iter-1":            ArtifactEditCodeInvalid,
		"missing.md":                  ArtifactEditCodeNotFound,
	} {
		_, err := eng.SaveRunArtifact(runID, name, "x", "", "")
		ae, ok := AsArtifactEditError(err)
		if !ok || ae.Code != code {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	eng.Halt()
	if _, err := eng.SaveRunArtifact(runID, "notes.md", "x", "", ""); err == nil {
		t.Fatal("halted engine should reject save")
	}
}

func TestSaveRunArtifactFreeformAndMarksPausedWriter(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-edit-freeform"
	seedEditRun(t, db, runID, "waiting_human")
	db.Create(&models.StateRun{RunID: runID, NodeID: "test", NodeType: "agent", Iteration: 1, Status: "waiting_human"})
	if _, err := eng.store.Save(runID, "test", "notes.md", "markdown", "v1"); err != nil {
		t.Fatal(err)
	}
	res, err := eng.SaveRunArtifact(runID, "notes.md", "v2", "", "")
	if err != nil || !res.Changed || res.Revision != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", runID, "test").First(&sr)
	if got, _ := sr.Outputs["artifact_notes.md"].(string); got != "v2" {
		t.Fatalf("outputs=%v", sr.Outputs)
	}
	if len(sr.ArtifactVisitWrites) != 1 || sr.ArtifactVisitWrites[0] != "notes.md" {
		t.Fatalf("visit writes=%v", sr.ArtifactVisitWrites)
	}
	if _, err := eng.SaveRunArtifact(runID, "notes.md", "  ", "", ""); err == nil {
		t.Fatal("empty markdown should be rejected")
	}
}

func TestArtifactErrorField(t *testing.T) {
	for msg, want := range map[string]string{
		"summary 不能为空":                                "summary",
		"data_design.entities[0].fields[1].name 不能为空": "data_design.entities[0].fields[1].name",
		"fields[2].verification 须为 sandbox_probe":     "fields[2].verification",
		"解析计划失败: bad":                                 "",
		"第 1 个大目标缺少 title":                            "",
	} {
		if got := artifactErrorField(msg); got != want {
			t.Fatalf("%q: got %q want %q", msg, got, want)
		}
	}
}

// TestHumanEditUnblocksPlanCoverageReview mirrors run-87e6cd15: during the
// test review the operator drops the real-environment goal g2 from plan.json
// and its coverage from test_result.json via SaveRunArtifact. Confirm then
// re-judges both edited bodies (either edit alone would fail PlanCoverageOK)
// and the run leaves through the pass outlet without an Agent rewrite.
func TestHumanEditUnblocksPlanCoverageReview(t *testing.T) {
	capsTestReview := *capsTest
	capsTestReview.Review = true
	g := planCoverageGateGraph()
	for i := range g.Nodes {
		if g.Nodes[i].ID == "test" {
			g.Nodes[i].Caps = &capsTestReview
		}
	}
	eng, db, p := setupEngineGraphP(t, g)
	p.structuredBodies = map[string]string{
		"plan": `{"goals":[{"id":"g1","title":"A","subgoals":[{"id":"g1.1","title":"x"}]},{"id":"g2","title":"现网实机验收","subgoals":[{"id":"g2.1","title":"vm"}]}]}`,
		"test": `{"summary":"all","cases":[{"name":"a","status":"passed"},{"name":"vm","status":"passed"}],"plan_coverage":[
			{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]},
			{"plan_id":"g2.1","passed":true,"evidence":"ok","cases":["vm"]}
		]}`,
	}
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitReactPause(t, db, run.ID, "test")
	waitRunStatus(t, db, run.ID, "waiting_human")
	if st := eng.ArtifactEditState(run.ID); !st.Editable {
		t.Fatalf("paused review not editable: %+v", st)
	}

	if _, err := eng.SaveRunArtifact(run.ID, mcp.PlanArtifactName,
		`{"goals":[{"title":"A","subgoals":[{"title":"x"}]}]}`, "", "op"); err != nil {
		t.Fatalf("edit plan: %v", err)
	}
	if _, err := eng.SaveRunArtifact(run.ID, mcp.TestResultArtifactName,
		`{"summary":"vm removed","cases":[{"name":"a","status":"passed"}],"plan_coverage":[{"plan_id":"g1.1","passed":true,"evidence":"ok","cases":["a"]}]}`,
		"", "op"); err != nil {
		t.Fatalf("edit test_result: %v", err)
	}
	for _, info := range eng.store.List(run.ID) {
		if info.Name == mcp.PlanArtifactName && info.Node != "plan" ||
			info.Name == mcp.TestResultArtifactName && info.Node != "test" {
			t.Fatalf("owner changed: %+v", info)
		}
	}

	if err := eng.ReactReply(run.ID, "test", "确认", nil, nil, true); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	assertNodeExecuted(t, db, run.ID, "ok", true)
	assertNodeExecuted(t, db, run.ID, "bad", false)
}
