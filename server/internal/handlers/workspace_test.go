package handlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkspaceCatalogAndHistoricalAuditHaveNoProjectFilter(t *testing.T) {
	h := newHarness(t)
	if response := h.do("GET", "/api/workspace/token-stats", nil); response.Code != 404 {
		t.Fatalf("removed board analytics endpoint = %d", response.Code)
	}
	if err := h.db.Create(&models.WorkflowDef{ID: "catalog", ProjectID: models.DefaultProjectID, Name: "Unsaved graph", Version: 2, PublishedVersion: 1, Graph: models.Graph{Variables: []models.Variable{{Name: "sensitive", Value: "never-in-catalog"}}}, Defaults: &models.WorkflowDefaults{Variables: []models.ProjectVariable{{Name: "token", Secret: true, Value: "never-in-catalog"}}}}).Error; err != nil {
		t.Fatal(err)
	}
	w := h.do("GET", "/api/workflows/catalog", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"draft"`) || strings.Contains(w.Body.String(), "never-in-catalog") || strings.Contains(w.Body.String(), `"nodes"`) || strings.Contains(w.Body.String(), `"defaults"`) {
		t.Fatalf("catalog=%d %s", w.Code, w.Body)
	}
	if err := h.db.Create(&models.ProjectAuditEvent{ProjectID: "deleted-origin", Actor: "admin", Action: "migration-history", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	w = h.do("GET", "/api/workspace/audit?time=all", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "deleted-origin") {
		t.Fatalf("historical origin hidden: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/api/workflows/catalog?projectId=foreign", "/api/workspace?projectId=" + models.DefaultProjectID + "&projectId=foreign"} {
		if w = h.do("GET", path, nil); w.Code != 400 {
			t.Fatalf("foreign scope=%d %s", w.Code, w.Body)
		}
	}
}

func TestWorkspaceMaintenanceReportsPendingDrain(t *testing.T) {
	h := newHarness(t)
	called := false
	h.h.Maintenance = func(_ context.Context, begin bool) (map[string]any, error) {
		called = true
		if begin {
			return map[string]any{"safeToStop": false, "pmThreads": 1}, errors.New("draining")
		}
		return map[string]any{"safeToStop": false}, nil
	}
	w := h.do("POST", "/api/workspace/maintenance", nil)
	if called || w.Code != 403 {
		t.Fatalf("maintenance requires administrator: %d %s", w.Code, w.Body)
	}
	enableAdmin(t)
	w = h.do("POST", "/api/workspace/maintenance", nil)
	if !called || w.Code != 409 || !strings.Contains(w.Body.String(), `"pmThreads":1`) || !strings.Contains(w.Body.String(), `"safeToStop":false`) {
		t.Fatalf("drain=%d %s", w.Code, w.Body)
	}
	w = h.do("GET", "/api/workspace/maintenance", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"safeToStop":false`) {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
}

func TestWorkflowDefaultsSecretFlagCannotRevealStoredValue(t *testing.T) {
	h := newHarness(t)
	wf := models.WorkflowDef{ID: "secret-defaults", ProjectID: models.DefaultProjectID, Name: "Private defaults", Version: 1, Defaults: &models.WorkflowDefaults{Variables: []models.ProjectVariable{{Name: "TOKEN", Secret: true, Value: "private-original-token"}}}}
	if err := h.db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	variable := map[string]any{"name": "TOKEN", "type": "string", "secret": false, "value": "****"}
	body := map[string]any{"name": wf.Name, "defaults": map[string]any{"inheritWorkspace": false, "variables": []any{variable}}}
	path := "/api/workflows/" + wf.ID
	response := h.do("PUT", path, body)
	if response.Code != 400 || strings.Contains(response.Body.String(), "private-original-token") {
		t.Fatalf("masked public write=%d %s", response.Code, response.Body)
	}
	response = h.do("GET", path, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"secret":true`) || strings.Contains(response.Body.String(), "private-original-token") {
		t.Fatalf("rejected update revealed or changed secret=%d %s", response.Code, response.Body)
	}
	variable["value"] = ""
	response = h.do("PUT", path, body)
	if response.Code != 200 || strings.Contains(response.Body.String(), "private-original-token") || !strings.Contains(response.Body.String(), `"value":""`) {
		t.Fatalf("clear did not stay empty=%d %s", response.Code, response.Body)
	}
	variable["name"], variable["secret"], variable["value"] = "RENAMED", true, "****"
	response = h.do("PUT", path, body)
	if response.Code != 400 {
		t.Fatalf("renamed masked variable should be a validation error=%d %s", response.Code, response.Body)
	}
}
