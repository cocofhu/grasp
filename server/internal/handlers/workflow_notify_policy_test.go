package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// TestPatchWorkflowNotifyPolicyOnly covers review v1: notify-only PATCH must
// not demote published→draft and must not rewrite Graph even when the client
// could have had a stale list-cache snapshot (payload has no nodes/edges).
func TestPatchWorkflowNotifyPolicyOnly(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{
		"name": "NotifyOnly", "projectId": models.DefaultProjectID, "description": "v1",
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
	id, _ := created["id"].(string)

	if id == "" {
		t.Fatal("no id")
	}

	if w := h.do("POST", "/api/workflows/"+id+"/publish", nil); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}

	// Simulate editor publishing a newer graph while list cache is stale.
	body["nodes"].([]map[string]any)[0]["label"] = "Start Published"
	body["id"] = id
	body["name"] = "NotifyOnly"
	if w := h.do("PUT", "/api/workflows/"+id, body); w.Code != 200 {
		t.Fatalf("graph put: %d %s", w.Code, w.Body)
	}
	if w := h.do("POST", "/api/workflows/"+id+"/publish", nil); w.Code != 200 {
		t.Fatalf("re-publish: %d %s", w.Code, w.Body)
	}

	var before map[string]any
	if w := h.do("GET", "/api/workflows/"+id, nil); w.Code != 200 {
		t.Fatalf("get before: %d", w.Code)
	} else {
		_ = json.Unmarshal(w.Body.Bytes(), &before)
	}
	if before["status"] != "published" {
		t.Fatalf("precondition status=%v", before["status"])
	}
	beforeNodes, _ := json.Marshal(before["nodes"])

	w = h.do("PATCH", "/api/workflows/"+id+"/notify-policy", map[string]any{
		"notifyPolicy": map[string]any{
			"mode":   "custom",
			"events": []string{"failed"},
		},
	})
	if w.Code != 200 {
		t.Fatalf("patch notify: %d %s", w.Code, w.Body)
	}
	var after map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &after)
	if after["status"] != "published" {
		t.Fatalf("notify-only PATCH demoted status to %v", after["status"])
	}
	afterNodes, _ := json.Marshal(after["nodes"])
	if string(beforeNodes) != string(afterNodes) {
		t.Fatalf("notify-only PATCH rewrote graph:\nbefore=%s\nafter=%s", beforeNodes, afterNodes)
	}
	np, _ := after["notifyPolicy"].(map[string]any)
	if np["mode"] != "custom" {
		t.Fatalf("notifyPolicy.mode=%v", np["mode"])
	}
	events, _ := np["events"].([]any)
	if len(events) != 1 || events[0] != "failed" {
		t.Fatalf("notifyPolicy.events=%v", events)
	}

	// 404 for missing workflow
	if w := h.do("PATCH", "/api/workflows/wf-missing/notify-policy", map[string]any{
		"notifyPolicy": map[string]any{"mode": "off"},
	}); w.Code != 404 {
		t.Fatalf("missing id want 404 got %d %s", w.Code, w.Body)
	}
}
