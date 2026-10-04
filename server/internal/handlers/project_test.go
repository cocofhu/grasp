package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
)

func TestProjectCRUDAndErrors(t *testing.T) {
	hn := newHarness(t)

	// List (includes default project from migrate)
	w := hn.do("GET", "/api/projects", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}

	// Create
	w = hn.do("POST", "/api/projects", map[string]any{
		"name":        "ProjA",
		"description": "desc",
		"variables":   []map[string]any{{"name": "x", "type": "string", "value": "1"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := jsonField(w.Body.String(), "id")
	if id == "" {
		t.Fatalf("missing id: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sandboxEnv") {
		t.Fatalf("project API must not return sandboxEnv: %s", w.Body.String())
	}

	// Get
	w = hn.do("GET", "/api/projects/"+id, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ProjA") {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}

	// Get missing
	w = hn.do("GET", "/api/projects/missing-id", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d", w.Code)
	}

	// Create empty name
	w = hn.do("POST", "/api/projects", map[string]any{"name": "  "})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty name: %d %s", w.Code, w.Body.String())
	}

	// Create duplicate name
	w = hn.do("POST", "/api/projects", map[string]any{"name": "ProjA"})
	if w.Code != http.StatusConflict {
		t.Fatalf("dup name: %d %s", w.Code, w.Body.String())
	}

	// Create bad JSON
	w = hn.do("POST", "/api/projects", "not-an-object")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json create: %d", w.Code)
	}

	// Shared agent config stores project-level env (replaces sandboxEnv API)
	w = hn.do("PUT", "/api/projects/"+id+"/shared-agent-config", map[string]any{
		"env": map[string]string{"CURSOR_API_KEY": "x"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("shared agent put: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CURSOR_API_KEY") {
		t.Fatalf("shared agent env missing: %s", w.Body.String())
	}

	// Update
	newName := "ProjA2"
	newDesc := "updated"
	w = hn.do("PUT", "/api/projects/"+id, map[string]any{
		"name":        newName,
		"description": newDesc,
		"variables":   []map[string]any{{"name": "y", "type": "string", "value": "2"}},
	})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ProjA2") {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	// Update missing
	w = hn.do("PUT", "/api/projects/missing-id", map[string]any{"name": "x"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing: %d", w.Code)
	}

	// Update empty name
	empty := "  "
	w = hn.do("PATCH", "/api/projects/"+id, map[string]any{"name": empty})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update empty name: %d %s", w.Code, w.Body.String())
	}

	// Update bad JSON
	w = hn.do("PUT", "/api/projects/"+id, "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json update: %d", w.Code)
	}

	// Delete ok
	w = hn.do("DELETE", "/api/projects/"+id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	// Delete missing
	w = hn.do("DELETE", "/api/projects/"+id, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", w.Code)
	}
}

func TestProjectDeleteWithWorkflows(t *testing.T) {
	hn := newHarness(t)
	w := hn.do("POST", "/api/projects", map[string]any{"name": "HasWF"})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := jsonField(w.Body.String(), "id")

	wf := models.WorkflowDef{
		ID: "wf-proj-block", Name: "blocked", Status: "draft", Version: 1,
		ProjectID: id, Graph: models.Graph{},
	}
	if err := hn.db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}

	w = hn.do("DELETE", "/api/projects/"+id, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete with workflows: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "流水线") {
		t.Fatalf("expected workflows error body: %s", w.Body.String())
	}
}

func TestProjectNilService(t *testing.T) {
	hn := newHarness(t)
	hn.h.Projects = nil

	if w := hn.do("GET", "/api/projects", nil); w.Code != http.StatusOK {
		t.Fatalf("list nil: %d", w.Code)
	}
	if w := hn.do("GET", "/api/projects/x", nil); w.Code != http.StatusNotFound {
		t.Fatalf("get nil: %d", w.Code)
	}
	if w := hn.do("POST", "/api/projects", map[string]any{"name": "n"}); w.Code != http.StatusInternalServerError {
		t.Fatalf("create nil: %d", w.Code)
	}
	if w := hn.do("PUT", "/api/projects/x", map[string]any{"name": "n"}); w.Code != http.StatusInternalServerError {
		t.Fatalf("update nil: %d", w.Code)
	}
	if w := hn.do("DELETE", "/api/projects/x", nil); w.Code != http.StatusInternalServerError {
		t.Fatalf("delete nil: %d", w.Code)
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

func TestProjectTotalTokensInListAndGet(t *testing.T) {
	hn := newHarness(t)

	w := hn.do("POST", "/api/projects", map[string]any{"name": "TokProj", "description": "d"})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"totalTokens":null`) {
		t.Fatalf("create should return null totalTokens: %s", w.Body.String())
	}
	id := jsonField(w.Body.String(), "id")

	w = hn.do("GET", "/api/projects/"+id, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"totalTokens":null`) {
		t.Fatalf("get empty: %d %s", w.Code, w.Body.String())
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(hn.db.Create(&models.WorkflowDef{ID: "wf-tok", ProjectID: id, Name: "w", Status: "draft", Version: 1}).Error)
	must(hn.db.Create(&models.Run{ID: "run-tok", WorkflowID: "wf-tok", Status: "completed"}).Error)
	must(hn.db.Create(&models.StateRun{
		RunID: "run-tok", NodeID: "n1", Status: "completed",
		Usage: &models.TokenUsage{InputTokens: 128000, OutputTokens: 400},
	}).Error)

	must(tokenledger.Backfill(hn.db))

	w = hn.do("GET", "/api/projects/"+id, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"totalTokens":128400`) {
		t.Fatalf("get with usage: %d %s", w.Code, w.Body.String())
	}

	w = hn.do("GET", "/api/projects", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"totalTokens":128400`) {
		t.Fatalf("list with usage: %d %s", w.Code, w.Body.String())
	}
}

func TestGetProjectTokenStats(t *testing.T) {
	hn := newHarness(t)

	w := hn.do("POST", "/api/projects", map[string]any{"name": "StatsProj", "description": "d"})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := jsonField(w.Body.String(), "id")

	w = hn.do("GET", "/api/projects/"+id+"/token-stats?window=30d&timezone=UTC", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("empty stats: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"empty":true`) {
		t.Fatalf("want empty true: %s", w.Body.String())
	}

	w = hn.do("GET", "/api/projects/"+id+"/token-stats?timezone=UTC", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("omit window: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"window":"30d"`) {
		t.Fatalf("project token-stats omitted window must stay 30d: %s", w.Body.String())
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	must(hn.db.Create(&models.WorkflowDef{ID: "wf-stats", ProjectID: id, Name: "w", Status: "draft", Version: 1}).Error)
	must(hn.db.Create(&models.Run{
		ID: "run-stats", WorkflowID: "wf-stats", WorkflowName: "approve-main",
		Status: "completed", StartedAt: now,
	}).Error)
	must(hn.db.Create(&models.StateRun{
		RunID: "run-stats", NodeID: "n1", Status: "completed",
		StartedAt: &now,
		Usage:     &models.TokenUsage{InputTokens: 10, OutputTokens: 5},
	}).Error)

	must(tokenledger.Backfill(hn.db))

	w = hn.do("GET", "/api/projects/"+id+"/token-stats?window=all&timezone=UTC", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"empty":false`) || !strings.Contains(body, `"total":15`) {
		t.Fatalf("unexpected body: %s", body)
	}
	if !strings.Contains(body, `"bucketWidth":"week"`) {
		t.Fatalf("all should be week: %s", body)
	}

	// Same ledger the board reads: studio with this project counts; studio
	// without a project and another project's rows do not. Query projectId
	// cannot retarget the path project. plan coverage: g1.1 g1.2 g1.3 g4.1
	other := hn.do("POST", "/api/projects", map[string]any{"name": "OtherStats", "description": "d"})
	if other.Code != http.StatusOK {
		t.Fatalf("create other: %d %s", other.Code, other.Body.String())
	}
	otherID := jsonField(other.Body.String(), "id")
	must(hn.db.Create(&models.TokenUsageEvent{
		CreatedAt: now, Source: "studio", Phase: "chat", Status: "ok",
		ProjectID: id, ProjectName: "StatsProj", ModelKey: "m",
		InputTokens: 7, OutputTokens: 3,
	}).Error)
	must(hn.db.Create(&models.TokenUsageEvent{
		CreatedAt: now, Source: "studio", Phase: "chat", Status: "ok",
		ModelKey: "orphan", InputTokens: 1000,
	}).Error)
	must(hn.db.Create(&models.TokenUsageEvent{
		CreatedAt: now, Source: "workflow", Phase: "production", Status: "ok",
		ProjectID: otherID, ProjectName: "OtherStats", WorkflowID: "wf-other", ModelKey: "m",
		InputTokens: 50,
	}).Error)

	w = hn.do("GET", "/api/projects/"+id+"/token-stats?window=all&timezone=UTC&projectId="+otherID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("locked stats: %d %s", w.Code, w.Body.String())
	}
	lockedBody := w.Body.String()
	if !strings.Contains(lockedBody, `"total":25`) || strings.Contains(lockedBody, `"total":1000`) || strings.Contains(lockedBody, `"total":50`) {
		t.Fatalf("project lock/studio scope: %s", lockedBody)
	}
	global := hn.do("GET", "/api/stats/token?window=all&timezone=UTC&projectId="+id, nil)
	if global.Code != http.StatusOK {
		t.Fatalf("global: %d %s", global.Code, global.Body.String())
	}
	if !strings.Contains(global.Body.String(), `"total":25`) {
		t.Fatalf("global project filter should match board: %s", global.Body.String())
	}

	w = hn.do("GET", "/api/projects/"+id+"/token-stats?window=bad", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad window: %d %s", w.Code, w.Body.String())
	}

	w = hn.do("GET", "/api/projects/nope/token-stats?window=7d", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing project: %d", w.Code)
	}
}
