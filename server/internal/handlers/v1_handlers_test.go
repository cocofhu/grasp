package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func minimalGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "Start"},
			{ID: "out", Type: "output", Label: "End"},
		},
		Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
	}
}

func seedPublishedWorkflow(t *testing.T, hn *harness, id string) {
	t.Helper()
	graph := minimalGraph()
	wf := models.WorkflowDef{
		ID: id, ProjectID: models.DefaultProjectID, Name: "API WF " + id, Version: 1, PublishedVersion: 1,
		Graph: graph,
	}
	if err := hn.db.Create(&wf).Error; err != nil {
		t.Fatalf("create wf: %v", err)
	}
	snap := models.WorkflowVersion{WorkflowID: id, Version: 1, Graph: graph}
	if err := hn.db.Create(&snap).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
}

func createAPIKey(t *testing.T, hn *harness, wfID, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/"+wfID+"/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "cf_session", Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return res["key"]
}

func TestV1APIAuthAndIsolation(t *testing.T) {
	hn := newHarness(t)
	seedPublishedWorkflow(t, hn, "wf-a")
	seedPublishedWorkflow(t, hn, "wf-b")
	keyA := createAPIKey(t, hn, "wf-a", "test")

	// No auth -> 401
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-nonexist", nil)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: want 401 got %d", w.Code)
	}

	// Start run on published workflow
	body, _ := json.Marshal(map[string]any{"inputs": map[string]any{}})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/workflows/wf-a/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyA)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start run: %d %s", w.Code, w.Body.String())
	}
	var startRes map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &startRes)
	runID := startRes["run_id"]
	if startRes["status"] == "" {
		t.Fatal("missing status")
	}

	// Verify trigger defaults to api for /v1
	run, ok := hn.h.Runs.Get(runID)
	if !ok {
		t.Fatal("run not found")
	}
	if run.Trigger != models.TriggerAPI {
		t.Fatalf("trigger: want %q got %q", models.TriggerAPI, run.Trigger)
	}

	// Get run with valid key
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/runs/"+runID, nil)
	req.Header.Set("Authorization", "Bearer "+keyA)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get run: %d %s", w.Code, w.Body.String())
	}
	var runRes map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &runRes)
	if runRes["run_id"] != runID {
		t.Fatalf("run_id mismatch: %v", runRes["run_id"])
	}
	if _, hasGate := runRes["gate"]; hasGate {
		t.Fatal("v1 response must not include gate")
	}

	// Cross-workflow path mismatch -> 404
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/workflows/wf-b/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyA)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross wf start: want 404 got %d", w.Code)
	}

	// Revoked key -> 401
	var keys []models.WorkflowAPIKey
	hn.db.Where("workflow_id = ?", "wf-a").Find(&keys)
	if len(keys) == 0 {
		t.Fatal("no keys")
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/workflows/wf-a/api-keys/"+keys[0].ID, nil)
	req.AddCookie(&http.Cookie{Name: "cf_session", Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke: %d", w.Code)
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/runs/"+runID, nil)
	req.Header.Set("Authorization", "Bearer "+keyA)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key: want 401 got %d", w.Code)
	}
}

func TestV1DraftWorkflowRejected(t *testing.T) {
	hn := newHarness(t)
	graph := minimalGraph()
	wf := models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-draft", Name: "Draft", Version: 0, Graph: graph}
	if err := hn.db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	key := createAPIKey(t, hn, "wf-draft", "k")
	body, _ := json.Marshal(map[string]any{"inputs": map[string]any{}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/workflows/wf-draft/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("draft start: want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestV1CancelRun(t *testing.T) {
	hn := newHarness(t)
	seedPublishedWorkflow(t, hn, "wf-cancel")
	key := createAPIKey(t, hn, "wf-cancel", "k")
	graph := minimalGraph()
	run := models.Run{
		ID: "run-cancel-test", WorkflowID: "wf-cancel", WorkflowName: "API WF",
		Status: "queued", Trigger: models.TriggerAPI, Graph: graph,
	}
	if err := hn.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/runs/run-cancel-test/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	run, _ = hn.h.Runs.Get("run-cancel-test")
	if run.Status != "cancelled" {
		t.Fatalf("status: want cancelled got %s", run.Status)
	}

	// V1 cancel must write run.cancel with system+unattributable (no Session).
	aw := hn.do(http.MethodGet, "/api/projects/"+models.DefaultProjectID+"/audit?time=all&action=run.cancel&page=1&pageSize=20", nil)
	if aw.Code != http.StatusOK {
		t.Fatalf("list audit: %d %s", aw.Code, aw.Body.String())
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.Unmarshal(aw.Body.Bytes(), &page)
	found := false
	for _, it := range page.Items {
		if it["resourceId"] == "run-cancel-test" && it["action"] == "run.cancel" {
			found = true
			if it["actor"] != "system" || it["unattributable"] != true {
				t.Fatalf("v1 cancel actor want system+unattributable: %#v", it)
			}
		}
	}
	if !found {
		t.Fatalf("expected run.cancel audit for v1 cancel, body=%s", aw.Body.String())
	}
}

func TestV1StartRunWritesAudit(t *testing.T) {
	hn := newHarness(t)
	seedPublishedWorkflow(t, hn, "wf-v1-audit")
	key := createAPIKey(t, hn, "wf-v1-audit", "k")

	body, _ := json.Marshal(map[string]any{"inputs": map[string]any{}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/workflows/wf-v1-audit/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start run: %d %s", w.Code, w.Body.String())
	}
	var startRes map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &startRes)
	runID := startRes["run_id"]
	if runID == "" {
		t.Fatalf("no run id: %s", w.Body.String())
	}

	aw := hn.do(http.MethodGet, "/api/projects/"+models.DefaultProjectID+"/audit?time=all&action=run.start&page=1&pageSize=50", nil)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.Unmarshal(aw.Body.Bytes(), &page)
	found := false
	for _, it := range page.Items {
		if it["resourceId"] == runID && it["action"] == "run.start" {
			found = true
			if it["actor"] != "system" || it["unattributable"] != true {
				t.Fatalf("v1 start actor want system+unattributable: %#v", it)
			}
		}
	}
	if !found {
		t.Fatalf("expected run.start audit for v1 start, body=%s", aw.Body.String())
	}
}

func TestInternalStartRunUsesDraftGraph(t *testing.T) {
	hn := newHarness(t)
	graph := minimalGraph()
	graph.Nodes[0].Label = "draft-head"
	wf := models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-internal", Name: "Internal", Version: 0, Graph: graph}
	if err := hn.db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"inputs": map[string]any{}, "trigger": "manual"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/wf-internal/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "cf_session", Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("internal start: %d %s", w.Code, w.Body.String())
	}
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	run, ok := hn.h.Runs.Get(res["id"])
	if !ok {
		t.Fatal("run not found")
	}
	if run.Graph.Nodes[0].Label != "draft-head" {
		t.Fatalf("internal run should use live draft graph")
	}
	if run.Trigger != models.TriggerManual {
		t.Fatalf("trigger: want %q got %q", models.TriggerManual, run.Trigger)
	}
}

func TestV1NoRoute404(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", w.Code)
	}
}
