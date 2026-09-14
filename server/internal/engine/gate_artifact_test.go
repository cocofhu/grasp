package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

func TestArtifactETagWithAndWithoutUpdatedAt(t *testing.T) {
	withTime := ArtifactETag("body", 4, time.Unix(100, 0))
	if withTime == "" || withTime == ArtifactETag("body", 4, time.Time{}) {
		t.Fatalf("time etag=%q", withTime)
	}
	noTime := ArtifactETag("body", 4, time.Time{})
	if noTime == "" {
		t.Fatal("size etag empty")
	}
}

func TestGateArtifactSaveAndListPrimary(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-gate-art"
	now := time.Now()
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "research"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":         "审阅",
				"body_template": "{{nodes.research.outputs.research}}",
				"actions":       []any{map[string]any{"id": "approve", "label": "批准"}},
			}},
		},
	}
	researchJSON := `{"summary":"original","findings":[{"title":"f1","detail":"d"}]}`
	if err := db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "waiting_human",
		Graph: g, StartedAt: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.StateRun{
		RunID: runID, NodeID: "research", NodeType: "research", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"research_json": researchJSON, "research": "md"},
	})
	db.Create(&models.Gate{
		RunID: runID, NodeID: "gate", Iteration: 1, Title: "审阅", RequestedAt: now,
		UpstreamNodeID: "research", UpstreamIteration: 1,
		Actions: []models.GateAction{{ID: "approve", Label: "批准"}},
	})
	if _, err := eng.store.Save(runID, "research", mcp.ResearchArtifactName, "json", researchJSON); err != nil {
		t.Fatal(err)
	}

	items, err := eng.ListGatePrimaryProducts(runID, "gate")
	if err != nil || len(items) != 1 {
		t.Fatalf("list primary: items=%v err=%v", items, err)
	}

	updated := `{"summary":"edited","findings":[{"title":"f1","detail":"d2"}]}`
	res, err := eng.SaveGateArtifact(runID, "gate", mcp.ResearchArtifactName, updated, "")
	if err != nil || res == nil || res.ETag == "" {
		t.Fatalf("save: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Content, "edited") {
		t.Fatalf("content=%q", res.Content)
	}
	content, ok := eng.store.Get(runID, mcp.ResearchArtifactName)
	if !ok || content == researchJSON {
		t.Fatalf("store not updated: %q", content)
	}

	_, err = eng.SaveGateArtifact(runID, "gate", "secret.md", "x", "")
	if err == nil || !IsArtifactConflict(err) && !strings.Contains(err.Error(), "不是该门禁的主产物") {
		t.Fatalf("non-primary err=%v", err)
	}

	_, err = eng.SaveGateArtifact(runID, "gate", mcp.ResearchArtifactName, updated, "W/\"stale\"")
	if err == nil || !IsArtifactConflict(err) {
		t.Fatalf("if-match conflict err=%v", err)
	}

	res2, err := eng.SaveGateArtifact(runID, "gate", mcp.ResearchArtifactName, updated, res.ETag)
	if err != nil || res2 == nil {
		t.Fatalf("if-match ok: res=%+v err=%v", res2, err)
	}
}

func TestGateArtifactErrorsAndHalt(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-gate-err"
	now := time.Now()
	g := models.Graph{Nodes: []models.Node{{ID: "gate", Type: "human_gate"}}}
	db.Create(&models.Run{ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "completed", Graph: g, StartedAt: now})
	if _, err := eng.ListGatePrimaryProducts(runID, "gate"); err == nil {
		t.Fatal("completed run should fail")
	}
	db.Model(&models.Run{}).Where("id = ?", runID).Update("status", "waiting_human")
	db.Create(&models.Gate{RunID: runID, NodeID: "gate", Iteration: 1, Title: "G", RequestedAt: now})
	if _, err := eng.SaveGateArtifact(runID, "gate", "nope.json", "{}", ""); err == nil {
		t.Fatal("missing upstream should fail save")
	}
	eng.Halt()
	if _, err := eng.SaveGateArtifact(runID, "gate", "nope.json", "{}", ""); err == nil {
		t.Fatal("halted engine should reject save")
	}
}

func TestSaveGateArtifactRecordsPageHistory(t *testing.T) {
	eng, db := setupEngine(t)
	runID := "run-gate-page-hist"
	now := time.Now()
	oldHTML := "<!doctype html><html><body>v1</body></html>"
	newHTML := "<!doctype html><html><body>v2</body></html>"
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
	if _, err := eng.store.Save(runID, "page", visualPageName, "html", oldHTML); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.SaveGateArtifact(runID, "gate", visualPageName, newHTML, ""); err != nil {
		t.Fatalf("save page: %v", err)
	}
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", runID, "page").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := sr.Outputs["page"].(string); got != newHTML {
		t.Fatalf("outputs.page=%q", got)
	}
	if _, ok := sr.Outputs["page_history"]; ok {
		t.Fatalf("page_history must not be written: %v", sr.Outputs["page_history"])
	}
}
