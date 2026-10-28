package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func TestProjectHTTPIgnoresLegacyRuntimeFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	h := &Handlers{Solo: services.NewSoloService(db, nil, nil, services.NewProjectService(db))}
	r := gin.New()
	r.POST("/api/projects", h.CreateProject)
	r.PUT("/api/projects/:id", h.UpdateProject)
	r.GET("/api/projects/:id", h.GetProject)

	body := `{
		"name":" app ",
		"description":"clone only",
		"repoUrl":"https://github.com/acme/app.git",
		"defaultBranch":"main",
		"repoDirName":"app",
		"gitCredentialType":"github_https",
		"gitCredentialId":"cred-1",
		"sshHostsCredentialId":"hosts-1",
		"env":{"APP_ENV":"dev","GITHUB_TOKEN":"nope"},
		"credentialBindings":{"NPM_TOKEN":"cred-2"}
	}`
	created := doProject(t, r, http.MethodPost, "/api/projects", body)
	if created["name"] != "app" || created["repoUrl"] != "https://github.com/acme/app.git" || created["defaultBranch"] != "main" || created["repoDirName"] != "app" || created["description"] != "clone only" {
		t.Fatalf("create response lost clone fields: %#v", created)
	}
	assertNoRuntimeFields(t, created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}

	updated := doProject(t, r, http.MethodPut, "/api/projects/"+id, `{
		"name":"app",
		"repoUrl":"git@github.com:acme/app.git",
		"defaultBranch":"dev",
		"gitCredentialType":"ssh",
		"gitCredentialId":"key",
		"env":{"APP_ENV":"prod"},
		"credentialBindings":{"API_TOKEN":"cred-3"}
	}`)
	if updated["repoUrl"] != "git@github.com:acme/app.git" || updated["defaultBranch"] != "dev" {
		t.Fatalf("update did not keep clone fields: %#v", updated)
	}
	assertNoRuntimeFields(t, updated)
	got := doProject(t, r, http.MethodGet, "/api/projects/"+id, "")
	project, _ := got["project"].(map[string]any)
	if project["repoUrl"] != "git@github.com:acme/app.git" || project["name"] != "app" {
		t.Fatalf("readback: %#v", got)
	}
	assertNoRuntimeFields(t, project)

	var columns []string
	if err := db.Raw("SELECT name FROM pragma_table_info('projects')").Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(columns, ",")
	for _, column := range []string{"git_credential_type", "git_credential_id", "ssh_hosts_credential_id", "env", "credential_bindings"} {
		if strings.Contains(joined, column) {
			t.Fatalf("column %s survived: %s", column, joined)
		}
	}
}

func doProject(t *testing.T, r http.Handler, method, path, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertNoRuntimeFields(t *testing.T, row map[string]any) {
	t.Helper()
	for _, key := range []string{"gitCredentialType", "gitCredentialId", "sshHostsCredentialId", "env", "credentialBindings"} {
		if _, ok := row[key]; ok {
			t.Fatalf("response still has %s: %#v", key, row)
		}
	}
}
