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

func TestWorkspaceAgentsExportImportHTTP(t *testing.T) {
	hn := newHarness(t)
	if w := hn.seedWorkspace(map[string]any{"name": "BundleDst"}); w.Code != http.StatusOK {
		t.Fatalf("rename workspace: %d %s", w.Code, w.Body.String())
	}
	for _, name := range []string{"bundle-a", "bundle-b"} {
		if err := hn.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: name}); err != nil {
			t.Fatal(err)
		}
	}

	w := hn.do(http.MethodGet, "/api/workspace/agents/export", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "BundleDst-agents.zip") {
		t.Fatalf("content-disposition = %q", cd)
	}
	bundle := append([]byte(nil), w.Body.Bytes()...)

	w = postMultipart(t, hn, "/api/workspace/agents/import", nil, bundle)
	if w.Code != http.StatusOK {
		t.Fatalf("import rename: %d %s", w.Code, w.Body.String())
	}
	var res services.ImportWorkspaceAgentsResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Renamed) != 2 {
		t.Fatalf("existing names must be renamed: %+v", res)
	}
	for _, name := range res.Renamed {
		if !hn.h.Agents.Exists(name) {
			t.Fatalf("imported %s missing", name)
		}
	}

	w = postMultipart(t, hn, "/api/workspace/agents/import", map[string]string{"mode": "overwrite"}, bundle)
	if w.Code != http.StatusOK {
		t.Fatalf("overwrite workspace agents want 200 got %d %s", w.Code, w.Body.String())
	}

	single := hn.do(http.MethodGet, "/api/agents/bundle-a/export", nil)
	if single.Code != http.StatusOK {
		t.Fatalf("single export: %d", single.Code)
	}
	w = postMultipart(t, hn, "/api/workspace/agents/import", nil, single.Body.Bytes())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("single agent zip want 400 got %d %s", w.Code, w.Body.String())
	}
	if w := postMultipart(t, hn, "/api/workspace/agents/import", nil, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing file want 400 got %d", w.Code)
	}
}

func TestImportAgentHTTP(t *testing.T) {
	hn := newHarness(t)
	if err := hn.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "zip-src"}); err != nil {
		t.Fatal(err)
	}
	w := hn.do(http.MethodGet, "/api/agents/zip-src/export", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d", w.Code)
	}
	raw := append([]byte(nil), w.Body.Bytes()...)

	w = postMultipart(t, hn, "/api/agents/import", map[string]string{"targetName": "zip-copy"}, raw)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	if !hn.h.Agents.Exists("zip-copy") {
		t.Fatal("imported agent missing")
	}
}
