package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
)

func TestListNodePreviews(t *testing.T) {
	hn := newHarness(t)
	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.host.SetPreviewStore(preview)
	hn.h.Preview = preview

	_ = preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: "run1", NodeID: "n1", Port: 3000, Label: "app",
		ProxyURL: "/preview/run1/n1/3000/", Healthy: true, RegisteredAt: time.Now(),
	})

	w := hn.do("GET", "/api/runs/run1/nodes/n1/previews", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list previews: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	ports, _ := body["ports"].([]any)
	if len(ports) == 0 {
		t.Fatalf("expected ports: %s", w.Body.String())
	}

	// MCP nil path
	h2 := &harness{r: hn.r, h: hn.h, cookie: hn.cookie}
	hn.h.MCP = nil
	w = hn.do("GET", "/api/runs/run1/nodes/n1/previews", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("nil mcp: %d", w.Code)
	}
	_ = h2
}

func TestPreviewProxyUnavailable(t *testing.T) {
	hn := newHarness(t)
	// Preview nil → 503
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/r/n/3000/", nil)
	req.Host = "pv.example.com"
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil preview: %d %s", w.Code, w.Body.String())
	}

	preview := services.NewPreviewService(hn.db, hn.h.Sbx.Manager())
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/preview/r/n/bad/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad port: %d", w.Code)
	}
}

func TestSandboxInject(t *testing.T) {
	hn := newHarness(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sandbox-inject/abc", nil)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: %d", w.Code)
	}

	store := sandbox.NewBundleStore()
	id, token := store.Put([]byte("tarball"), time.Minute)
	hn.h.InjectBundles = store

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/sandbox-inject/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "tarball" {
		t.Fatalf("inject get: %d %s", w.Code, w.Body.String())
	}
}

func TestExportImportAgentHandlers(t *testing.T) {
	hn := newHarness(t)
	seedAgent(t, hn, "ZipAgent")

	w := hn.do("GET", "/api/agents/ZipAgent/export", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type=%q", ct)
	}
	zipBytes := append([]byte(nil), w.Body.Bytes()...)

	w = hn.do("GET", "/api/agents/missing/export", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing export: %d", w.Code)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("targetName", "ImportedAgent")
	_ = mw.WriteField("mode", "create")
	fw, err := mw.CreateFormFile("file", "agent.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(zipBytes); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agents/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/agents/import", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("import no file: %d", w.Code)
	}
}

func TestV1ArtifactsAndListKeys(t *testing.T) {
	hn := newHarness(t)
	seedPublishedWorkflow(t, hn, "wf-art")
	key := createAPIKey(t, hn, "wf-art", "k1")

	w := hn.do("GET", "/api/workflows/wf-art/api-keys", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list keys: %d %s", w.Code, w.Body.String())
	}
	w = hn.do("GET", "/api/workflows/missing/api-keys", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing wf keys: %d", w.Code)
	}

	art := models.Artifact{
		ID: "art-1", RunID: "run-art", WorkflowID: "wf-art",
		Name: "out.md", Kind: "markdown", Content: "# hi", SizeBytes: 4,
	}
	if err := hn.db.Create(&art).Error; err != nil {
		t.Fatal(err)
	}
	run := models.Run{
		ID: "run-art", WorkflowID: "wf-art", WorkflowName: "API WF",
		Status: "succeeded", Trigger: models.TriggerAPI, Graph: minimalGraph(),
	}
	if err := hn.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}

	doV1 := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		hn.r.ServeHTTP(w, req)
		return w
	}

	w = doV1("GET", "/v1/runs/run-art/artifacts")
	if w.Code != http.StatusOK {
		t.Fatalf("artifacts: %d %s", w.Code, w.Body.String())
	}
	w = doV1("GET", "/v1/runs/missing/artifacts")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing run arts: %d", w.Code)
	}

	w = doV1("GET", "/v1/artifacts/art-1/download")
	if w.Code != http.StatusOK {
		t.Fatalf("download: %d %s", w.Code, w.Body.String())
	}
	body, _ := io.ReadAll(w.Body)
	if len(body) == 0 {
		t.Fatal("empty download")
	}
	w = doV1("GET", "/v1/artifacts/missing/download")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing download: %d", w.Code)
	}
}
