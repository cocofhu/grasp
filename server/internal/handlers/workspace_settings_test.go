package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger/ledgertest"
)

func TestWorkspaceSettings(t *testing.T) {
	h := newHarness(t)
	w := h.do("GET", "/api/workspace", nil)
	if w.Code != 200 || jsonField(w.Body.String(), "name") != models.DefaultWorkspaceName {
		t.Fatal(w.Body.String())
	}
	w = h.do("PUT", "/api/workspace", map[string]any{"name": "Workspace", "description": "updated", "variables": []map[string]any{{"name": "TOKEN", "type": "string", "secret": true, "value": "sensitive"}}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "****") || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatal(w.Body.String())
	}
	for _, body := range []any{map[string]any{"name": " "}, "invalid"} {
		if w = h.do("PUT", "/api/workspace", body); w.Code != 400 {
			t.Fatalf("invalid settings %d", w.Code)
		}
	}
	for _, method := range []string{"POST", "DELETE"} {
		if w = h.do(method, "/api/workspace", nil); w.Code != 404 {
			t.Fatalf("singleton mutation %s = %d", method, w.Code)
		}
	}
}

func TestWorkspaceCannotBeDeletedWithWorkflows(t *testing.T) {
	h := newHarness(t)
	if err := h.db.Create(&models.WorkflowDef{ID: "protected", Name: "protected"}).Error; err != nil {
		t.Fatal(err)
	}
	if w := h.do("DELETE", "/api/workspace", nil); w.Code != 404 {
		t.Fatal(w.Body.String())
	}
	if _, err := h.h.Workspace.Workspace(); err != nil {
		t.Fatalf("workspace deleted: %v", err)
	}
}

func TestWorkspaceUnavailableFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.h.Workspace = nil
	for _, method := range []string{"GET", "PUT"} {
		if w := h.do(method, "/api/workspace", nil); w.Code != 500 {
			t.Fatalf("missing workspace %s: %d", method, w.Code)
		}
	}
}

func jsonField(body, key string) string {
	// tiny extractor: "key":"value"
	needle := `"` + key + `":"`
	i := strings.Index(body, needle)
	if i < 0 {
		return ""
	}
	rest := body[i+len(needle):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func TestWorkspaceTotalTokens(t *testing.T) {
	hn := newHarness(t)

	w := hn.seedWorkspace(map[string]any{"name": "TokWorkspace", "description": "d"})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"totalTokens":null`) {
		t.Fatalf("update should return null totalTokens: %s", w.Body.String())
	}

	w = hn.do("GET", "/api/workspace", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"totalTokens":null`) {
		t.Fatalf("get empty: %d %s", w.Code, w.Body.String())
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(hn.db.Create(&models.WorkflowDef{ID: "wf-tok", Name: "w", Version: 1}).Error)
	must(hn.db.Create(&models.Run{ID: "run-tok", WorkflowID: "wf-tok", Status: "completed"}).Error)
	must(hn.db.Create(&models.StateRun{
		RunID: "run-tok", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 128000, OutputTokens: 400},
	}).Error)

	must(ledgertest.Sync(hn.db))

	w = hn.do("GET", "/api/workspace", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"totalTokens":128400`) {
		t.Fatalf("get with usage: %d %s", w.Code, w.Body.String())
	}
}
