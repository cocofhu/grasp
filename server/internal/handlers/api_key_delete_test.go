package handlers_test

import (
	"net/http"
	"testing"
)

func TestCreateAPIKeyBranches(t *testing.T) {
	hn := newHarness(t)
	seedPublishedWorkflow(t, hn, "wf-keys")

	w := hn.do("POST", "/api/workflows/missing/api-keys", map[string]any{"name": "k"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing wf: %d", w.Code)
	}
	w = hn.do("POST", "/api/workflows/wf-keys/api-keys", "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", w.Code)
	}
	w = hn.do("POST", "/api/workflows/wf-keys/api-keys", map[string]any{"name": "good"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	w = hn.do("DELETE", "/api/workflows/wf-keys/api-keys/nope", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("revoke missing: %d", w.Code)
	}
}

func TestDeleteWorkflowAndAgentOK(t *testing.T) {
	hn := newHarness(t)
	w := hn.do("POST", "/api/workflows", map[string]any{
		"name":  "to-del",
		"nodes": []any{}, "edges": []any{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("save wf: %d %s", w.Code, w.Body.String())
	}
	id := jsonField(w.Body.String(), "id")

	if id == "" {
		t.Fatalf("no id: %s", w.Body.String())
	}
	w = hn.do("DELETE", "/api/workflows/"+id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete wf: %d %s", w.Code, w.Body.String())
	}
	w = hn.do("DELETE", "/api/workflows/missing-wf", nil)
	// Delete is idempotent for missing ids → 200 deleted (never 5xx).
	if w.Code != http.StatusOK {
		t.Fatalf("delete missing wf: %d %s", w.Code, w.Body.String())
	}

	seedAgent(t, hn, "DelMe")
	w = hn.do("DELETE", "/api/agents/DelMe", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete agent: %d %s", w.Code, w.Body.String())
	}
	w = hn.do("DELETE", "/api/agents/DelMe", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete agent again (idempotent): %d %s", w.Code, w.Body.String())
	}
}
