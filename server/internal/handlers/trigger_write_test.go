package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestInternalStartRunTriggerDefaultsAndRejects(t *testing.T) {
	hn := newHarness(t)
	graph := minimalGraph()
	wf := models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-trig", Name: "Trig", Version: 0, Graph: graph}
	if err := hn.db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}

	// Empty body → default manual
	w := hn.do("POST", "/api/workflows/wf-trig/runs", map[string]any{"inputs": map[string]any{}})
	if w.Code != http.StatusOK {
		t.Fatalf("empty trigger: %d %s", w.Code, w.Body.String())
	}
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	run, ok := hn.h.Runs.Get(res["id"])
	if !ok {
		t.Fatal("run missing")
	}
	if run.Trigger != models.TriggerManual {
		t.Fatalf("default trigger = %q, want manual", run.Trigger)
	}

	// Explicit legal api takes precedence over source default
	w = hn.do("POST", "/api/workflows/wf-trig/runs", map[string]any{"trigger": "api"})
	if w.Code != http.StatusOK {
		t.Fatalf("explicit api: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	run, _ = hn.h.Runs.Get(res["id"])
	if run.Trigger != models.TriggerAPI {
		t.Fatalf("explicit api = %q", run.Trigger)
	}

	illegal := []string{"channel", "qq:cron-timezone-bug", "手动触发", "Manual", "test"}
	for _, trig := range illegal {
		w = hn.do("POST", "/api/workflows/wf-trig/runs", map[string]any{"trigger": trig})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("illegal %q: want 400 got %d %s", trig, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "manual|api|pm_mcp") {
			t.Fatalf("illegal %q: error missing allow-list: %s", trig, w.Body.String())
		}
		// Ensure no run was persisted with the illegal trigger.
		var count int64
		hn.db.Model(&models.Run{}).Where("trigger = ?", trig).Count(&count)
		if count != 0 {
			t.Fatalf("illegal %q must not be persisted", trig)
		}
	}
}

func TestV1StartRunTriggerDefaultsExplicitAndRejects(t *testing.T) {
	hn := newHarness(t)
	seedPublishedWorkflow(t, hn, "wf-v1-trig")
	key := createAPIKey(t, hn, "wf-v1-trig", "k")

	start := func(body map[string]any) (*httptest.ResponseRecorder, string) {
		t.Helper()
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/workflows/wf-v1-trig/runs", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		hn.r.ServeHTTP(w, req)
		var res map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return w, res["run_id"]
	}

	w, runID := start(map[string]any{"inputs": map[string]any{}})
	if w.Code != http.StatusOK {
		t.Fatalf("default: %d %s", w.Code, w.Body.String())
	}
	run, ok := hn.h.Runs.Get(runID)
	if !ok || run.Trigger != models.TriggerAPI {
		t.Fatalf("v1 default trigger = %q ok=%v", run.Trigger, ok)
	}

	w, runID = start(map[string]any{"trigger": "manual"})
	if w.Code != http.StatusOK {
		t.Fatalf("explicit manual: %d %s", w.Code, w.Body.String())
	}
	run, _ = hn.h.Runs.Get(runID)
	if run.Trigger != models.TriggerManual {
		t.Fatalf("explicit manual = %q", run.Trigger)
	}

	w, _ = start(map[string]any{"trigger": "channel"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("illegal: want 400 got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "manual|api|pm_mcp") {
		t.Fatalf("error missing allow-list: %s", w.Body.String())
	}
	var count int64
	hn.db.Model(&models.Run{}).Where("trigger = ?", "channel").Count(&count)
	if count != 0 {
		t.Fatal("illegal channel must not be persisted via /v1")
	}
}
