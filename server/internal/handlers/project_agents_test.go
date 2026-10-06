package handlers_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/services"
)

func createProjectForTest(t *testing.T, hn *harness, name string) string {
	t.Helper()
	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": name})
	if w.Code != http.StatusOK {
		t.Fatalf("create project %s: %d %s", name, w.Code, w.Body.String())
	}
	return jsonField(w.Body.String(), "id")
}

func postMultipart(t *testing.T, hn *harness, path string, fields map[string]string, file []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if file != nil {
		fw, err := mw.CreateFormFile("file", "bundle.zip")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(file)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	return w
}

func TestProjectAgentsExportImportHTTP(t *testing.T) {
	hn := newHarness(t)
	src := createProjectForTest(t, hn, "BundleSrc")
	dst := createProjectForTest(t, hn, "BundleDst")
	for _, name := range []string{"bundle-a", "bundle-b"} {
		if err := hn.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: name, ProjectID: src}); err != nil {
			t.Fatal(err)
		}
	}

	w := hn.do(http.MethodGet, "/api/projects/"+src+"/agents/export", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "BundleSrc-agents.zip") {
		t.Fatalf("content-disposition = %q", cd)
	}
	bundle := append([]byte(nil), w.Body.Bytes()...)

	if w := hn.do(http.MethodGet, "/api/projects/proj_missing/agents/export", nil); w.Code != http.StatusNotFound {
		t.Fatalf("export missing project: %d", w.Code)
	}

	w = postMultipart(t, hn, "/api/projects/"+dst+"/agents/import", nil, bundle)
	if w.Code != http.StatusOK {
		t.Fatalf("import rename: %d %s", w.Code, w.Body.String())
	}
	var res services.ImportProjectAgentsResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Renamed) != 2 {
		t.Fatalf("existing names must be renamed: %+v", res)
	}
	for _, name := range res.Renamed {
		ag, ok := hn.h.Agents.Get(name)
		if !ok || ag.ProjectID != dst {
			t.Fatalf("imported %s = %+v", name, ag)
		}
	}

	w = postMultipart(t, hn, "/api/projects/"+dst+"/agents/import", map[string]string{"mode": "overwrite"}, bundle)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("overwrite foreign agents want 400 got %d %s", w.Code, w.Body.String())
	}
	if ag, _ := hn.h.Agents.Get("bundle-a"); ag.ProjectID != src {
		t.Fatalf("foreign agent rebound: %+v", ag)
	}

	single := hn.do(http.MethodGet, "/api/agents/bundle-a/export", nil)
	if single.Code != http.StatusOK {
		t.Fatalf("single export: %d", single.Code)
	}
	w = postMultipart(t, hn, "/api/projects/"+dst+"/agents/import", nil, single.Body.Bytes())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("single agent zip want 400 got %d %s", w.Code, w.Body.String())
	}
	if w := postMultipart(t, hn, "/api/projects/"+dst+"/agents/import", nil, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing file want 400 got %d", w.Code)
	}
}

func TestImportAgentRequiresProjectID(t *testing.T) {
	hn := newHarness(t)
	pid := createProjectForTest(t, hn, "ImportHome")
	if err := hn.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "zip-src", ProjectID: pid}); err != nil {
		t.Fatal(err)
	}
	w := hn.do(http.MethodGet, "/api/agents/zip-src/export", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d", w.Code)
	}
	raw := append([]byte(nil), w.Body.Bytes()...)

	w = postMultipart(t, hn, "/api/agents/import", map[string]string{"targetName": "zip-copy"}, raw)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("import without projectId want 400 got %d %s", w.Code, w.Body.String())
	}
	w = postMultipart(t, hn, "/api/agents/import", map[string]string{"targetName": "zip-copy", "projectId": "proj_missing"}, raw)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("import with missing project want 400 got %d %s", w.Code, w.Body.String())
	}
	w = postMultipart(t, hn, "/api/agents/import", map[string]string{"targetName": "zip-copy", "projectId": pid}, raw)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	if ag, ok := hn.h.Agents.Get("zip-copy"); !ok || ag.ProjectID != pid {
		t.Fatalf("imported = %+v", ag)
	}
}
