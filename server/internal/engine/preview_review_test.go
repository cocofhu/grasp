package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// TestAppPreviewSetPreviewEntersReviewNoGate: healthy set_preview must pause
// waiting_human with a seeded ReAct review conversation and NO Gate row.
func TestAppPreviewSetPreviewEntersReviewNoGate(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "preview", Type: "agent", Caps: capsPreview, Label: "预览", Config: map[string]any{
				"title": "应用预览",
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "preview"},
			{ID: "e2", Source: "preview", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")

	if !eng.host.HasHealthyPreviewPorts(run.ID, "preview") {
		t.Fatal("expected healthy preview port after RunAgent")
	}

	var gate models.Gate
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&gate).Error; err == nil {
		t.Fatal("preview review must not create a Gate")
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&conv).Error; err != nil {
		t.Fatalf("expected review conversation after set_preview: %v", err)
	}
	if conv.Done || len(conv.Messages) == 0 {
		t.Fatalf("review conv should be open with agent summary: %+v", conv)
	}

	// GateReactInfo does not resolve a preview review agent as a gate producer.
	if pid, _ := eng.GateReactInfo(run.ID, "preview"); pid != "" {
		t.Fatalf("GateReactInfo producer=%q want empty (no Gate)", pid)
	}

	if rp, ok := eng.provider.(runtime.ReviewProvider); ok {
		if !rp.HasLiveSession(run.ID, "preview") {
			t.Fatal("expected parked review session alive (fakeProvider)")
		}
	}

	// node_complete must not be required on the success path.
	if _, ok := eng.host.TakeOutcome(run.ID, "preview"); ok {
		t.Fatal("node_complete should not be required for preview review success")
	}
}

// TestAppPreviewReviewConfirmInjectsPassAndClearsIssues: force confirm finishes
// via finalize + routeSuccess with action=pass and cleared preview_issues.
func TestAppPreviewReviewConfirmInjectsPassAndClearsIssues(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "preview", Type: "agent", Caps: capsPreview, Label: "预览", Config: map[string]any{
				"title": "应用预览",
			}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "preview"},
			{ID: "e2", Source: "preview", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")

	iss := models.PreviewIssue{
		ID: "iss-ap-confirm", RunID: run.ID, NodeID: "preview", Body: "btn too wide", Status: "open",
	}
	if err := db.Create(&iss).Error; err != nil {
		t.Fatal(err)
	}
	c, err := eng.loadCtx(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.setVar("preview_issues", "stale")
	eng.persistVar(run.ID, "preview_issues", "stale")

	if err := eng.ReactReply(run.ID, "preview", "确认", nil, nil, true); err != nil {
		t.Fatalf("force confirm: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "preview").
		Order("iteration desc").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if sr.Status != "completed" {
		t.Fatalf("preview status=%s", sr.Status)
	}
	var outSR models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&outSR).Error; err != nil {
		t.Fatalf("expected output visit: %v", err)
	}

	var open int64
	db.Model(&models.PreviewIssue{}).Where("run_id = ? AND node_id = ? AND status = ?", run.ID, "preview", "open").Count(&open)
	if open != 0 {
		t.Fatalf("open preview issues after confirm: %d", open)
	}
	c2, _ := eng.loadCtx(run.ID)
	if previewIssuesVarNonEmpty(c2.vars["preview_issues"]) {
		t.Fatalf("preview_issues still set: %v", c2.vars["preview_issues"])
	}
}
