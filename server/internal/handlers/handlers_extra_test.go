package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/shutdown"
)

func TestSettingsAndLiveEndpoints(t *testing.T) {
	h := newHarness(t)
	h.h.Settings = services.NewSettingsService(h.db, h.h.Eng, h.h.Sbx)
	h.h.Shutdown = shutdown.New(600 * time.Second)

	if w := h.do("GET", "/api/live", nil); w.Code != 200 {
		t.Fatalf("live: %d", w.Code)
	}
	if w := h.do("GET", "/api/settings", nil); w.Code != 200 {
		t.Fatalf("get settings: %d %s", w.Code, w.Body)
	}
	if w := h.do("PUT", "/api/settings", map[string]any{
		services.KeyBrandProductName: "Acme",
	}); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin brand update: %d %s", w.Code, w.Body)
	}
	cfg := config.GetConfig()
	cfg.Auth.Users[0].IsAdmin = true
	config.StoreConfig(cfg)
	if w := h.do("PUT", "/api/settings", map[string]any{
		services.KeyBrandProductName:  " Acme ",
		services.KeyBrandHomeSubtitle: " Build clearly ",
	}); w.Code != http.StatusOK {
		t.Fatalf("admin brand update: %d %s", w.Code, w.Body)
	}
	if got := h.h.Settings.Brand(); got.ProductName != "Acme" || got.HomeSubtitle != "Build clearly" {
		t.Fatalf("saved brand: %+v", got)
	}
	if w := h.do("PUT", "/api/settings", map[string]any{
		services.KeyBrandProductName: string(make([]rune, services.BrandProductNameMaxLength+1)),
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("overlong brand update: %d %s", w.Code, w.Body)
	}
	if w := h.do("PUT", "/api/settings", map[string]int{
		services.KeyMaxConcurrentRuns: 3,
		services.KeyAgentIdleMin:      25,
	}); w.Code != 200 || !strings.Contains(w.Body.String(), `"agent_idle_timeout_minutes"`) {
		t.Fatalf("update settings: %d %s", w.Code, w.Body)
	}
	if w := h.do("PUT", "/api/settings", "bad"); w.Code != 400 {
		t.Fatalf("update settings bad body: %d", w.Code)
	}
	if w := h.do("PUT", "/api/settings", map[string]int{
		services.KeyMaxConcurrentRuns: 0,
	}); w.Code != 400 {
		t.Fatalf("update settings below min: %d", w.Code)
	}

	h.h.Shutdown.BeginDraining()
	if w := h.do("GET", "/api/health", nil); w.Code != 503 {
		t.Fatalf("health draining: %d %s", w.Code, w.Body)
	}
}

func TestHealthReportsDBError(t *testing.T) {
	h := newHarness(t)
	if w := h.do("GET", "/api/health", nil); w.Code != 200 {
		t.Fatalf("health ok: %d %s", w.Code, w.Body)
	}
	sqlDB, err := h.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if w := h.do("GET", "/api/health", nil); w.Code != 503 || !strings.Contains(w.Body.String(), "db_error") {
		t.Fatalf("health db down: %d %s", w.Code, w.Body)
	}
}

func TestWorkflowImportCopyAndVersionGraph(t *testing.T) {
	h := newHarness(t)
	body := map[string]any{
		"schemaVersion": 1,
		"name":          "Import WF",
		"graph": map[string]any{
			"nodes": []map[string]any{
				{"id": "in", "type": "input", "label": "S"},
				{"id": "out", "type": "output", "label": "E"},
			},
			"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/import", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	var imported map[string]any
	json.Unmarshal(w.Body.Bytes(), &imported)
	id, _ := imported["id"].(string)
	if id == "" {
		t.Fatal("import missing id")
	}

	if w := h.do("POST", "/api/workflows/import", "bad"); w.Code != 400 {
		t.Fatalf("import bad: %d", w.Code)
	}
	if w := h.do("GET", "/api/workflows/"+id+"/copy-preview", nil); w.Code != 200 {
		t.Fatalf("copy preview: %d %s", w.Code, w.Body)
	}
	if w := h.do("GET", "/api/workflows/ghost/copy-preview", nil); w.Code != 404 {
		t.Fatalf("copy preview missing: %d", w.Code)
	}
	if w := h.do("POST", "/api/workflows/"+id+"/copy", map[string]string{"name": "Copy Of Import"}); w.Code != 201 {
		t.Fatalf("copy: %d %s", w.Code, w.Body)
	}
	if w := h.do("POST", "/api/workflows/"+id+"/copy", map[string]string{"name": ""}); w.Code != 400 {
		t.Fatalf("copy empty name: %d", w.Code)
	}
	if w := h.do("POST", "/api/workflows/ghost/copy", map[string]string{"name": "x"}); w.Code != 404 {
		t.Fatalf("copy missing: %d", w.Code)
	}

	if w := h.do("POST", "/api/workflows/"+id+"/publish", nil); w.Code != 200 {
		t.Fatalf("publish: %d", w.Code)
	}
	if w := h.do("GET", "/api/workflows/"+id+"/versions/1/graph", nil); w.Code != 200 {
		t.Fatalf("version graph: %d %s", w.Code, w.Body)
	}
	if w := h.do("GET", "/api/workflows/"+id+"/versions/bad/graph", nil); w.Code != 400 {
		t.Fatalf("version graph bad: %d", w.Code)
	}
	if w := h.do("GET", "/api/workflows/ghost/versions/1/graph", nil); w.Code != 404 {
		t.Fatalf("version graph missing wf: %d", w.Code)
	}
}

func TestResumeRunEndpoint(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "r-failed", Status: "failed", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}}},
	})
	if w := h.do("POST", "/api/runs/r-failed/resume", map[string]string{"nodeId": "in"}); w.Code != 200 {
		t.Fatalf("resume: %d %s", w.Code, w.Body)
	}
	if w := h.do("POST", "/api/runs/ghost/resume", nil); w.Code != 404 {
		t.Fatalf("resume missing: %d", w.Code)
	}
	if w := h.do("POST", "/api/runs/r-failed/resume", "bad"); w.Code != 400 {
		t.Fatalf("resume bad body: %d", w.Code)
	}
}
