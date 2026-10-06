package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// TestSaveWorkflowStatusGuard covers f6/f7: after publish, PUT with no graph
// diff keeps published (even when client always sends draft intent); graph
// change downgrades; metadata-only updates land without downgrade. Also
// exercises LiftInputVariables so variables in input.config do not look like
// a spurious graph diff vs DB Graph.Variables.
func TestSaveWorkflowStatusGuard(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{
		"name": "Guard", "projectId": models.DefaultProjectID, "description": "",
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
	json.Unmarshal(w.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id")
	}
	if created["status"] != "draft" {
		t.Fatalf("create status=%v", created["status"])
	}

	if w := h.do("POST", "/api/workflows/"+id+"/publish", nil); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}

	// Identical PUT (variables still in input.config as DTO shape) is a
	// version no-op and stays published.
	body["id"] = id
	w = h.do("PUT", "/api/workflows/"+id, body)
	if w.Code != 200 {
		t.Fatalf("noop put: %d %s", w.Code, w.Body)
	}
	var afterNoop map[string]any
	json.Unmarshal(w.Body.Bytes(), &afterNoop)
	if afterNoop["status"] != "published" || afterNoop["version"] != float64(1) || afterNoop["publishedVersion"] != float64(1) {
		t.Fatalf("noop PUT: status=%v version=%v published=%v", afterNoop["status"], afterNoop["version"], afterNoop["publishedVersion"])
	}

	// Graph change → v2 draft; v1 stays the published version.
	nodes := body["nodes"].([]map[string]any)
	nodes[0]["label"] = "Start Edited"
	w = h.do("PUT", "/api/workflows/"+id, body)
	if w.Code != 200 {
		t.Fatalf("graph put: %d %s", w.Code, w.Body)
	}
	var afterGraph map[string]any
	json.Unmarshal(w.Body.Bytes(), &afterGraph)
	if afterGraph["status"] != "draft" || afterGraph["version"] != float64(2) || afterGraph["publishedVersion"] != float64(1) {
		t.Fatalf("graph change: status=%v version=%v published=%v", afterGraph["status"], afterGraph["version"], afterGraph["publishedVersion"])
	}

	// Versions list carries metadata only.
	w = h.do("GET", "/api/workflows/"+id+"/versions", nil)
	if w.Code != 200 {
		t.Fatalf("versions: %d %s", w.Code, w.Body)
	}
	var versions []map[string]any
	json.Unmarshal(w.Body.Bytes(), &versions)
	if len(versions) != 2 || versions[0]["version"] != float64(2) || versions[0]["source"] != "save" || versions[0]["nodeCount"] != float64(2) {
		t.Fatalf("versions list: %+v", versions)
	}
	if _, ok := versions[0]["publishedAt"]; ok {
		t.Fatalf("v2 must not be published: %+v", versions[0])
	}
	if versions[1]["publishedAt"] == nil {
		t.Fatalf("v1 should carry publishedAt: %+v", versions[1])
	}
	if _, ok := versions[0]["nodes"]; ok {
		t.Fatal("versions list must not include graphs")
	}

	// Any version's graph is readable.
	if w := h.do("GET", "/api/workflows/"+id+"/versions/2/graph", nil); w.Code != 200 {
		t.Fatalf("unpublished version graph: %d %s", w.Code, w.Body)
	}

	// Restore v1 → v3 (restore source); the 404 path for a missing version.
	w = h.do("POST", "/api/workflows/"+id+"/versions/1/restore", nil)
	if w.Code != 200 {
		t.Fatalf("restore: %d %s", w.Code, w.Body)
	}
	var restored map[string]any
	json.Unmarshal(w.Body.Bytes(), &restored)
	if restored["version"] != float64(3) || restored["status"] != "draft" {
		t.Fatalf("restore: %+v", restored)
	}
	if w := h.do("POST", "/api/workflows/"+id+"/versions/42/restore", nil); w.Code != 404 {
		t.Fatalf("restore missing version: %d", w.Code)
	}
}
