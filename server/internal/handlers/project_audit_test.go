package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestProjectAuditListExportAndPermission(t *testing.T) {
	hn := newHarness(t)

	// Create project → should write project.config audit
	w := hn.do(http.MethodPost, "/api/projects", map[string]any{
		"name": "AuditProj", "description": "d",
		"variables": []map[string]any{
			{"name": "API_TOKEN", "type": "string", "value": "plain-secret", "secret": true},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid, _ := proj["id"].(string)
	if pid == "" {
		t.Fatal("no project id")
	}

	// Update with secret var → audit payload must not contain plaintext
	w = hn.do(http.MethodPatch, "/api/projects/"+pid, map[string]any{
		"variables": []map[string]any{
			{"name": "API_TOKEN", "type": "string", "value": "new-secret-value", "secret": true},
			{"name": "REGION", "type": "string", "value": "cn-east"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	// List audit (default 24h)
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit?page=1&pageSize=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list audit: %d %s", w.Code, w.Body.String())
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.Total < 2 {
		t.Fatalf("expected >=2 audit events, got %d body=%s", page.Total, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "new-secret-value") || strings.Contains(body, "plain-secret") {
		t.Fatalf("plaintext secret leaked in audit list: %s", body)
	}
	foundActor := false
	for _, it := range page.Items {
		if it["actor"] == "admin" && it["unattributable"] == false {
			foundActor = true
		}
		if it["action"] == "project.config" {
			payload, _ := json.Marshal(it["payload"])
			if strings.Contains(string(payload), "new-secret-value") {
				t.Fatalf("secret in payload: %s", payload)
			}
		}
	}
	if !foundActor {
		t.Fatalf("expected attributable admin actor in events: %s", body)
	}

	// Cross-project isolation: create another project, ensure no leak
	w2 := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "OtherProj"})
	var other map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &other)
	oid, _ := other["id"].(string)
	w = hn.do(http.MethodGet, "/api/projects/"+oid+"/audit?page=1&pageSize=20&time=all", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	for _, it := range page.Items {
		if it["projectId"] != oid {
			t.Fatalf("cross-project leak: %#v", it)
		}
	}

	beforeExport := page.Total
	_ = beforeExport

	// Export JSON → meta audit
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit/export?format=json&time=all", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export json: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("missing content-disposition: %v", w.Header())
	}
	if strings.Contains(w.Body.String(), "new-secret-value") {
		t.Fatalf("secret in export: %s", w.Body.String())
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit?page=1&pageSize=20&time=all&action=audit.export", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.Total < 1 {
		t.Fatalf("expected export meta-audit event, got %d", page.Total)
	}

	// Export text
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit/export?format=text&time=all", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export text: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Grasp Project Audit Export") {
		t.Fatalf("text export header missing: %s", w.Body.String())
	}

	// Permission deny via hook (simulates read-only member)
	hn.h.CanViewProjectAudit = func(username, projectID string) bool {
		return false
	}
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit?page=1&pageSize=20", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for denied audit, got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit/export?format=json", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for denied export, got %d %s", w.Code, w.Body.String())
	}
}

func TestProjectAuditWorkflowAndRunCoverage(t *testing.T) {
	hn := newHarness(t)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "WFAudit"})
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid, _ := proj["id"].(string)

	graph := map[string]any{
		"name": "wf-audit", "projectId": pid,
		"nodes": []map[string]any{
			{"id": "in", "type": "input"},
			{"id": "out", "type": "output"},
		},
		"edges": []map[string]any{
			{"id": "e1", "source": "in", "target": "out"},
		},
	}
	w = hn.do(http.MethodPost, "/api/workflows", graph)
	if w.Code != http.StatusOK {
		t.Fatalf("save wf: %d %s", w.Code, w.Body.String())
	}
	var wf map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &wf)
	wfID, _ := wf["id"].(string)

	w = hn.do(http.MethodPost, "/api/workflows/"+wfID+"/publish", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit?time=all&action=workflow&page=1&pageSize=50", nil)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.Total < 2 {
		t.Fatalf("expected workflow create+publish audits, got %d %s", page.Total, w.Body.String())
	}

	// Facets: distinct resources for cascade dropdowns
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit/facets?time=all", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("facets: %d %s", w.Code, w.Body.String())
	}
	var facets struct {
		Resources []struct {
			ResourceType string `json:"resourceType"`
			ResourceID   string `json:"resourceId"`
			Resource     string `json:"resource"`
		} `json:"resources"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &facets)
	if len(facets.Resources) < 1 {
		t.Fatalf("expected at least one resource facet, got %#v", facets.Resources)
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/audit/facets?time=all&action=workflow", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("facets action=workflow: %d %s", w.Code, w.Body.String())
	}
}
