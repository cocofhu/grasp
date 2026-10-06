package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func createPublishedWorkflow(t *testing.T, h *harness, name string) (id string, dto map[string]any) {
	t.Helper()
	body := map[string]any{
		"name": name, "projectId": models.DefaultProjectID, "description": "v1",
		"nodes": []map[string]any{
			{
				"id": "in", "type": "input", "label": "Start",
				"position": map[string]any{"x": 0, "y": 0},
				"config": map[string]any{
					"variables": []map[string]any{
						{"name": "repo", "type": "string", "value": "a"},
					},
				},
			},
			{"id": "out", "type": "output", "label": "End", "position": map[string]any{"x": 1, "y": 0}, "config": map[string]any{}},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	}
	w := h.do("POST", "/api/workflows", body)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id, _ = created["id"].(string)
	if id == "" {
		t.Fatal("no id")
	}
	if created["showOnHome"] != false {
		t.Fatalf("create showOnHome=%v want false (plan g1.1)", created["showOnHome"])
	}
	if w := h.do("POST", "/api/workflows/"+id+"/publish", nil); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}
	w = h.do("GET", "/api/workflows/"+id, nil)
	if w.Code != 200 {
		t.Fatalf("get: %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &dto)
	return id, dto
}

// plan g1.1 / g1.2 / g1.4 — dedicated PATCH toggles showOnHome without touching graph/status/version.
func TestPatchWorkflowHomeVisibilityOnly(t *testing.T) {
	h := newHarness(t)
	id, before := createPublishedWorkflow(t, h, "HomeOnly")
	if before["status"] != "published" {
		t.Fatalf("precondition status=%v", before["status"])
	}
	beforeNodes, _ := json.Marshal(before["nodes"])
	beforeVersion := before["version"]

	w := h.do("PATCH", "/api/workflows/"+id+"/home-visibility", map[string]any{
		"showOnHome": true,
	})
	if w.Code != 200 {
		t.Fatalf("patch home: %d %s", w.Code, w.Body)
	}
	var after map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &after)
	if after["showOnHome"] != true {
		t.Fatalf("showOnHome=%v", after["showOnHome"])
	}
	if after["status"] != "published" {
		t.Fatalf("home-visibility PATCH demoted status to %v", after["status"])
	}
	if after["version"] != beforeVersion {
		t.Fatalf("version mutated %v → %v", beforeVersion, after["version"])
	}
	afterNodes, _ := json.Marshal(after["nodes"])
	if string(beforeNodes) != string(afterNodes) {
		t.Fatalf("home-visibility PATCH rewrote graph:\nbefore=%s\nafter=%s", beforeNodes, afterNodes)
	}

	if w := h.do("PATCH", "/api/workflows/"+id+"/home-visibility", map[string]any{}); w.Code != 400 {
		t.Fatalf("missing showOnHome want 400 got %d %s", w.Code, w.Body)
	}
	if w := h.do("PATCH", "/api/workflows/wf-missing/home-visibility", map[string]any{
		"showOnHome": true,
	}); w.Code != 404 {
		t.Fatalf("missing id want 404 got %d %s", w.Code, w.Body)
	}
}

// plan g1.3 / g1.4 — editor PUT without showOnHome must keep a previously enabled flag.
func TestSaveWorkflowOmitsShowOnHomePreservesTrue(t *testing.T) {
	h := newHarness(t)
	id, _ := createPublishedWorkflow(t, h, "KeepHome")

	if w := h.do("PATCH", "/api/workflows/"+id+"/home-visibility", map[string]any{
		"showOnHome": true,
	}); w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body)
	}

	put := map[string]any{
		"name": "KeepHome", "projectId": models.DefaultProjectID, "description": "renamed subtitle",
		"nodes": []map[string]any{
			{
				"id": "in", "type": "input", "label": "Start",
				"position": map[string]any{"x": 0, "y": 0},
				"config": map[string]any{
					"variables": []map[string]any{
						{"name": "repo", "type": "string", "value": "a"},
					},
				},
			},
			{"id": "out", "type": "output", "label": "End", "position": map[string]any{"x": 1, "y": 0}, "config": map[string]any{}},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	}
	w := h.do("PUT", "/api/workflows/"+id, put)
	if w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	var after map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &after)
	if after["showOnHome"] != true {
		t.Fatalf("omitted showOnHome reset flag to %v", after["showOnHome"])
	}
	// A description edit is a content change: new head version, published stays v1.
	if after["status"] != "draft" || after["publishedVersion"] != float64(1) {
		t.Fatalf("status=%v publishedVersion=%v", after["status"], after["publishedVersion"])
	}
	if after["description"] != "renamed subtitle" {
		t.Fatalf("description=%v", after["description"])
	}

	// Explicit false via PUT is allowed (not the list-row path).
	put["showOnHome"] = false
	w = h.do("PUT", "/api/workflows/"+id, put)
	if w.Code != 200 {
		t.Fatalf("put false: %d %s", w.Code, w.Body)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &after)
	if after["showOnHome"] != false {
		t.Fatalf("explicit false: %v", after["showOnHome"])
	}
}

// plan g1.3 — create with showOnHome true in body is still stored as false.
func TestCreateWorkflowIgnoresShowOnHomeTrue(t *testing.T) {
	h := newHarness(t)
	body := map[string]any{
		"name": "ForceOn", "projectId": models.DefaultProjectID,
		"showOnHome": true,
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "Start", "position": map[string]any{"x": 0, "y": 0}, "config": map[string]any{}},
			{"id": "out", "type": "output", "label": "End", "position": map[string]any{"x": 1, "y": 0}, "config": map[string]any{}},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	}
	w := h.do("POST", "/api/workflows", body)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created["showOnHome"] != false {
		t.Fatalf("create showOnHome=%v", created["showOnHome"])
	}
}
