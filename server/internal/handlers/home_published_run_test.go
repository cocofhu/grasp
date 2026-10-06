package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestHomeUsesPublishedSnapshotNotDraftHead(t *testing.T) {
	hn := newHarness(t)

	published := minimalGraph()
	published.Nodes[0].Label = "published-v1"
	draft := minimalGraph()
	draft.Nodes[0].Label = "draft-head"

	wf := models.WorkflowDef{
		ID: "wf-home", ProjectID: models.DefaultProjectID,
		Name: "草稿名", Description: "草稿说明",
		Version: 2, PublishedVersion: 1, ShowOnHome: true,
		Graph: draft,
	}
	if err := hn.db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Create(&models.WorkflowVersion{
		WorkflowID: "wf-home", Version: 1, Name: "已发布名", Description: "已发布说明", Graph: published,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Create(&models.WorkflowVersion{
		WorkflowID: "wf-home", Version: 2, Name: "草稿名", Description: "草稿说明", Graph: draft,
	}).Error; err != nil {
		t.Fatal(err)
	}
	never := models.WorkflowDef{
		ID: "wf-never", ProjectID: models.DefaultProjectID,
		Name: "从未发布", Version: 1, PublishedVersion: 0, ShowOnHome: true,
		Graph: published,
	}
	if err := hn.db.Create(&never).Error; err != nil {
		t.Fatal(err)
	}

	list := decodeWorkflowList(t, hn.do(http.MethodGet, "/api/workflows", nil))
	home := findWorkflow(t, list, "wf-home")
	if home.Name != "草稿名" || home.Status != "draft" || home.Version != 2 || home.PublishedVersion != 1 {
		t.Fatalf("head: %+v", home)
	}
	if len(home.Nodes) == 0 || home.Nodes[0].Label != "draft-head" {
		t.Fatalf("head graph = %+v, want draft-head", home.Nodes)
	}
	if home.PublishedSnapshot == nil || home.PublishedSnapshot.Name != "已发布名" || home.PublishedSnapshot.Description != "已发布说明" || home.PublishedSnapshot.Version != 1 {
		t.Fatalf("snapshot = %+v", home.PublishedSnapshot)
	}
	if len(home.PublishedSnapshot.Nodes) == 0 || home.PublishedSnapshot.Nodes[0].Label != "published-v1" {
		t.Fatalf("snapshot graph = %+v", home.PublishedSnapshot.Nodes)
	}
	neverDTO := findWorkflow(t, list, "wf-never")
	if neverDTO.PublishedSnapshot != nil || neverDTO.PublishedVersion != 0 {
		t.Fatalf("never published must not expose a snapshot: %+v", neverDTO)
	}

	detail := decodeWorkflow(t, hn.do(http.MethodGet, "/api/workflows/wf-home", nil))
	if detail.Nodes[0].Label != "draft-head" || detail.PublishedSnapshot == nil || detail.PublishedSnapshot.Nodes[0].Label != "published-v1" {
		t.Fatalf("detail = %+v snap=%+v", detail, detail.PublishedSnapshot)
	}

	pubRun := startAndLoad(t, hn, "/api/workflows/wf-home/runs", map[string]any{
		"inputs":            map[string]any{},
		"trigger":           "manual",
		"priority":          "high",
		"title":             "第一句话",
		"publishedSnapshot": true,
		"firstMessage":      map[string]any{"text": "第一句话"},
	})
	if pubRun.WorkflowVersion != 1 || pubRun.Graph.Nodes[0].Label != "published-v1" {
		t.Fatalf("home run version=%d label=%q", pubRun.WorkflowVersion, pubRun.Graph.Nodes[0].Label)
	}
	if pubRun.WorkflowName != "已发布名" || pubRun.Priority != models.PriorityHigh || pubRun.Title != "第一句话" || pubRun.Trigger != models.TriggerManual {
		t.Fatalf("home run name=%q pri=%d title=%q trigger=%q", pubRun.WorkflowName, pubRun.Priority, pubRun.Title, pubRun.Trigger)
	}
	if pubRun.FirstMessage == nil || pubRun.FirstMessage.Text != "第一句话" {
		t.Fatalf("first message = %+v", pubRun.FirstMessage)
	}

	headRun := startAndLoad(t, hn, "/api/workflows/wf-home/runs", map[string]any{
		"inputs":  map[string]any{},
		"trigger": "manual",
	})
	if headRun.WorkflowVersion != 2 || headRun.Graph.Nodes[0].Label != "draft-head" || headRun.WorkflowName != "草稿名" {
		t.Fatalf("editor run version=%d label=%q name=%q", headRun.WorkflowVersion, headRun.Graph.Nodes[0].Label, headRun.WorkflowName)
	}

	rejected := hn.do(http.MethodPost, "/api/workflows/wf-never/runs", map[string]any{
		"publishedSnapshot": true,
		"trigger":           "manual",
	})
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("never published start: %d %s", rejected.Code, rejected.Body.String())
	}

	if err := hn.db.Model(&models.WorkflowDef{}).Where("id = ?", "wf-home").Update("published_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	next := startAndLoad(t, hn, "/api/workflows/wf-home/runs", map[string]any{
		"publishedSnapshot": true,
		"trigger":           "manual",
		"priority":          "low",
	})
	if next.WorkflowVersion != 2 || next.Graph.Nodes[0].Label != "draft-head" || next.WorkflowName != "草稿名" || next.Priority != models.PriorityLow {
		t.Fatalf("post-publish run version=%d label=%q name=%q pri=%d", next.WorkflowVersion, next.Graph.Nodes[0].Label, next.WorkflowName, next.Priority)
	}
	after := decodeWorkflow(t, hn.do(http.MethodGet, "/api/workflows/wf-home", nil))
	if after.Status != "published" || after.PublishedSnapshot == nil || after.PublishedSnapshot.Version != 2 || after.PublishedSnapshot.Name != "草稿名" {
		t.Fatalf("after publish: %+v snap=%+v", after, after.PublishedSnapshot)
	}
}

type wfNodeDTO struct {
	Label string `json:"label"`
}

type publishedSnapDTO struct {
	Version     int         `json:"version"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Nodes       []wfNodeDTO `json:"nodes"`
}

type wfListDTO struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	Status            string            `json:"status"`
	Version           int               `json:"version"`
	PublishedVersion  int               `json:"publishedVersion"`
	Nodes             []wfNodeDTO       `json:"nodes"`
	PublishedSnapshot *publishedSnapDTO `json:"publishedSnapshot"`
}

func decodeWorkflowList(t *testing.T, w *httptest.ResponseRecorder) []wfListDTO {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("list workflows: %d %s", w.Code, w.Body.String())
	}
	var out []wfListDTO
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeWorkflow(t *testing.T, w *httptest.ResponseRecorder) wfListDTO {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("get workflow: %d %s", w.Code, w.Body.String())
	}
	var out wfListDTO
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func findWorkflow(t *testing.T, list []wfListDTO, id string) wfListDTO {
	t.Helper()
	for _, wf := range list {
		if wf.ID == id {
			return wf
		}
	}
	t.Fatalf("workflow %s not in list", id)
	return wfListDTO{}
}

func startAndLoad(t *testing.T, hn *harness, path string, body map[string]any) models.Run {
	t.Helper()
	w := hn.do(http.MethodPost, path, body)
	if w.Code != http.StatusOK {
		t.Fatalf("start %s: %d %s", path, w.Code, w.Body.String())
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	run, ok := hn.h.Runs.Get(res.ID)
	if !ok {
		t.Fatalf("run %s not found", res.ID)
	}
	return run
}
