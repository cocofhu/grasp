package handlers_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// listAgentNames returns the names GET /api/agents reports.
func listAgentNames(t *testing.T, h *harness) map[string]bool {
	t.Helper()
	w := h.do("GET", "/api/agents", nil)
	if w.Code != 200 {
		t.Fatalf("list agents: %d %s", w.Code, w.Body.String())
	}
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, a := range list {
		out[a.Name] = true
	}
	return out
}

// A create rejected by validation must not leave the agent on disk.
func TestCreateAgent_validationFailureWritesNothing(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing acpBackend", map[string]any{"name": "probe1", "files": []any{}}},
		{"unknown acpBackend", map[string]any{"name": "probe2", "acpBackend": "nope", "files": []any{}}},
		{"bad credential binding key", map[string]any{
			"name": "probe3", "acpBackend": "cursor", "files": []any{},
			"credentialBindings": map[string]any{" padded": nil},
		}},
		{"secret env key", map[string]any{
			"name": "probe4", "acpBackend": "cursor", "files": []any{},
			"env": map[string]any{"GITHUB_TOKEN": "x"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := tc.body["name"].(string)
			w := h.do("POST", "/api/agents", tc.body)
			if w.Code != 400 {
				t.Fatalf("want 400, got %d %s", w.Code, w.Body.String())
			}
			if listAgentNames(t, h)[agent] {
				t.Fatalf("%s listed after a rejected create", agent)
			}
			if h.h.Agents.Exists(agent) {
				t.Fatalf("%s directory left on disk after a rejected create", agent)
			}
			// The name stays free: a valid retry succeeds instead of 409.
			if w := h.do("POST", "/api/agents", map[string]any{"name": agent, "acpBackend": "cursor", "files": []any{}}); w.Code != 201 {
				t.Fatalf("valid retry: want 201, got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

// PUT validation failures beyond acpBackend/env are request errors (400), not
// 500s, and record no workspace revision.
func TestSaveAgent_validationFailureIs400(t *testing.T) {
	h := newHarness(t)
	if w := h.do("POST", "/api/agents", map[string]any{"name": "probe5", "acpBackend": "cursor", "files": []any{}}); w.Code != 201 {
		t.Fatalf("seed: %d %s", w.Code, w.Body.String())
	}
	w := h.do("PUT", "/api/agents/probe5", map[string]any{
		"acpBackend": "cursor", "files": []any{},
		"credentialBindings": map[string]any{" padded": nil},
	})
	if w.Code != 400 {
		t.Fatalf("want 400, got %d %s", w.Code, w.Body.String())
	}
	if w := h.do("GET", "/api/agents/probe5/workspace/revisions", nil); w.Code != 200 || strings.Contains(w.Body.String(), "sha") {
		t.Fatalf("rejected save recorded a revision: %d %s", w.Code, w.Body.String())
	}
}
