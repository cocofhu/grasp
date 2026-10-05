package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

func TestSaveGateArtifactHTTP(t *testing.T) {
	h := newHarness(t)
	runID := "run-http-edit"
	now := time.Now()
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "agent", Caps: testReviewCaps},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":         "审阅",
				"body_template": "{{nodes.research.outputs.research}}",
				"actions":       []any{map[string]any{"id": "approve", "label": "批准"}},
			}},
		},
	}
	researchJSON := `{"summary":"original","findings":[{"title":"f1","detail":"d"}]}`
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "waiting_human",
		Graph: g, StartedAt: now, CreatedAt: now,
	})
	h.db.Create(&models.StateRun{
		RunID: runID, NodeID: "research", NodeType: "agent", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"research_json": researchJSON, "research": "md"},
	})
	h.db.Create(&models.Gate{
		RunID: runID, NodeID: "gate", Iteration: 1, Title: "审阅", RequestedAt: now,
		UpstreamNodeID: "research", UpstreamIteration: 1,
		Actions: []models.GateAction{{ID: "approve", Label: "批准"}},
	})
	if _, err := h.h.Arts.Save(runID, "research", mcp.ResearchArtifactName, "json", researchJSON); err != nil {
		t.Fatal(err)
	}

	list := h.do(http.MethodGet, "/api/runs/"+runID+"/gates/gate/primary-artifacts", nil)
	if list.Code != 200 {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var listBody struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listBody); err != nil || len(listBody.Items) != 1 {
		t.Fatalf("list body: %s err=%v", list.Body.String(), err)
	}

	updated := `{"summary":"http-edited","findings":[{"title":"f1","detail":"d2"}]}`
	w := h.do(http.MethodPut, "/api/runs/"+runID+"/gates/gate/artifacts/research.json", map[string]any{
		"content": updated,
	})
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var saved struct {
		ETag    string `json:"etag"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.ETag == "" {
		t.Fatalf("save body: %s", w.Body.String())
	}
	if saved.Content == "" || !strings.Contains(saved.Content, "http-edited") {
		t.Fatalf("save response missing normalized content: %s", w.Body.String())
	}
	content, ok := h.h.Arts.Get(runID, mcp.ResearchArtifactName)
	if !ok || content == "" || content == researchJSON {
		t.Fatalf("artifact not updated: ok=%v", ok)
	}
	if content != saved.Content {
		t.Fatalf("response content != store: resp=%q store=%q", saved.Content, content)
	}

	// Non-primary rejected
	bad := h.do(http.MethodPut, "/api/runs/"+runID+"/gates/gate/artifacts/secret.md", map[string]any{
		"content": "x",
	})
	if bad.Code != 400 {
		t.Fatalf("non-primary want 400, got %d", bad.Code)
	}
}

func TestSaveAnnotationArtifactHTTP(t *testing.T) {
	h := newHarness(t)
	runID := "run-http-ann"
	now := time.Now()
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "visual", Type: "agent", Caps: testReviewCaps},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title":         "审阅",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions":       []any{map[string]any{"id": "pass", "label": "通过"}},
			}},
		},
	}
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "waiting_human",
		Graph: g, StartedAt: now, CreatedAt: now,
	})
	h.db.Create(&models.Gate{
		RunID: runID, NodeID: "gate", Iteration: 2, Title: "审阅", RequestedAt: now,
		UpstreamNodeID: "visual", UpstreamIteration: 1,
		Actions: []models.GateAction{{ID: "pass", Label: "通过"}},
	})

	w := h.do(http.MethodPut, "/api/runs/"+runID+"/gates/gate/annotation-artifact", map[string]any{
		"annotations": []map[string]any{
			{
				"seq": 1, "selector": "h1.title", "comment": "字号过大",
				"screenshot": "MISSING", "markKind": "click",
			},
		},
	})
	if w.Code != 200 {
		t.Fatalf("save ann: %d %s", w.Code, w.Body.String())
	}
	var saved struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
		ETag    string `json:"etag"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Name != "preview_annotations.json" || saved.Kind != "preview_annotations" || saved.ETag == "" {
		t.Fatalf("saved=%+v", saved)
	}
	if !strings.Contains(saved.Content, "仅改标中区域") || !strings.Contains(saved.Content, "h1.title") {
		t.Fatalf("content=%s", saved.Content)
	}
	content, ok := h.h.Arts.Get(runID, "preview_annotations.json")
	if !ok || content == "" {
		t.Fatal("artifact missing in store")
	}

	// Empty annotations clears delivery (f4 dirty invalidate).
	cleared := h.do(http.MethodPut, "/api/runs/"+runID+"/gates/gate/annotation-artifact", map[string]any{
		"annotations": []any{},
	})
	if cleared.Code != 200 {
		t.Fatalf("empty clear want 200, got %d %s", cleared.Code, cleared.Body.String())
	}
	var clearBody struct {
		Cleared bool   `json:"cleared"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(cleared.Body.Bytes(), &clearBody); err != nil {
		t.Fatal(err)
	}
	if !clearBody.Cleared || !strings.Contains(clearBody.Content, `"status": "cleared"`) {
		t.Fatalf("clear body=%+v", clearBody)
	}
}
