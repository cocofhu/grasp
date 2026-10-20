package handlers_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
)

func assertWorkspaceScope(t *testing.T, label string, w *httptest.ResponseRecorder) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusBadRequest || body.Code != "workspace_scope" {
		t.Fatalf("%s: want 400 workspace_scope, got %d %s", label, w.Code, w.Body.String())
	}
}

func TestForeignBodyProjectIDIsRejectedWithoutWrites(t *testing.T) {
	hn := newHarness(t)
	graph := map[string]any{
		"nodes": []map[string]any{{"id": "in", "type": "input", "label": "S"}, {"id": "out", "type": "output", "label": "E"}},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	}
	existing := models.WorkflowDef{ID: "wf-home", ProjectID: models.DefaultProjectID, Name: "Home", Version: 1}
	if err := hn.db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	if w := hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "home-agent"}); w.Code != http.StatusCreated {
		t.Fatalf("seed agent: %d %s", w.Code, w.Body.String())
	}
	for _, foreign := range []string{"proj-former-beta", "proj_dead"} {
		create := map[string]any{"name": "Foreign " + foreign, "projectId": foreign}
		for k, v := range graph {
			create[k] = v
		}
		assertWorkspaceScope(t, "create workflow", hn.do(http.MethodPost, "/api/workflows", create))
		update := map[string]any{"name": "Home renamed", "projectId": foreign}
		assertWorkspaceScope(t, "update workflow", hn.do(http.MethodPut, "/api/workflows/"+existing.ID, update))
		assertWorkspaceScope(t, "baseline workflow", hn.do(http.MethodPost, "/api/workflows/from-baseline", map[string]any{
			"name": "Baseline " + foreign, "projectId": foreign, "repos": []map[string]any{{"url": "https://example.com/r.git"}},
		}))
		assertWorkspaceScope(t, "create agent", hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "foreign-agent", "projectId": foreign}))
		assertWorkspaceScope(t, "save agent", hn.do(http.MethodPut, "/api/agents/home-agent", map[string]any{"acpBackend": "cursor", "name": "home-agent", "projectId": foreign, "env": map[string]string{"CHANGED": "1"}}))

		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("targetName", "imported-foreign")
		_ = mw.WriteField("mode", "create")
		_ = mw.WriteField("projectId", foreign)
		fw, _ := mw.CreateFormFile("file", "agent.zip")
		_, _ = fw.Write([]byte("not-a-zip"))
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/agents/import", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
		w := httptest.NewRecorder()
		hn.r.ServeHTTP(w, req)
		assertWorkspaceScope(t, "import agent", w)
	}

	var flows []models.WorkflowDef
	if err := hn.db.Find(&flows).Error; err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].Name != "Home" || flows[0].ProjectID != models.DefaultProjectID {
		t.Fatalf("rejected workflow writes persisted: %+v", flows)
	}
	for _, name := range []string{"foreign-agent", "imported-foreign"} {
		if hn.h.Agents.Exists(name) {
			t.Fatalf("rejected agent %s persisted", name)
		}
	}
	agent, ok := hn.h.Agents.Get("home-agent")
	if !ok || agent.ProjectID != models.DefaultProjectID || agent.Env["CHANGED"] != "" {
		t.Fatalf("rejected agent save changed the profile: %+v", agent)
	}
	var projects int64
	if err := hn.db.Model(&models.Project{}).Count(&projects).Error; err != nil || projects != 1 {
		t.Fatalf("workspace count changed: %d %v", projects, err)
	}
}
