package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/driver/fake"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func testRouter(t *testing.T, apiKeys []string) (*gin.Engine, *fake.Driver, *service.SandboxService) {
	t.Helper()
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "api.db")})
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	st := store.New(db)
	drv := fake.New()
	if err := drv.WithSessionListener(8765); err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(drv.Close)
	svc := service.New(drv, st, service.Config{
		Image:           "test-image:local",
		Ports:           []int{8765, 22},
		SessionPort:     8765,
		WorkspaceDir:    "/root/workspace",
		FinalizeTimeout: 5 * time.Second,
		Resources: config.ResourceDefaults{
			DefaultCPUCores: 2, DefaultMemoryMB: 4096, DefaultDiskGi: 25,
			MaxCPUCores: 8, MaxMemoryMB: 16384, MaxDiskGi: 500,
		},
	})
	ports := config.PortsConfig{Session: 8765, CodeServer: 8744, SSH: 22, CDP: 9222, NoVNC: 6080}
	h := NewHandler(svc, ports)
	cfg := config.Default()
	cfg.Driver = "fake"
	cfg.Auth.APIKeys = apiKeys
	return NewRouter(h, cfg), drv, svc
}

func TestAPIKeyAuthDisabled(t *testing.T) {
	r, _, _ := testRouter(t, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != 200 {
		t.Fatalf("healthz=%d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes", nil))
	if w.Code != 200 {
		t.Fatalf("list without auth (disabled)=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIKeyAuthRequired(t *testing.T) {
	r, _, _ := testRouter(t, []string{"secret-key"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("with key=%d %s", w.Code, w.Body.String())
	}
}

func TestCreateAndHost(t *testing.T) {
	r, _, svc := testRouter(t, nil)
	body := `{"config":{"bundleUrl":"http://x/b.tgz","configRoot":"/root/.cursor"},"labels":{"t":"api"}}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes", bytes.NewBufferString(body)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("no id: %v", created)
	}
	// Persist inject into store env.
	sb, err := svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if sb.Env()["SANDBOX_INJECT"] != "http://x/b.tgz|/root/.cursor" {
		t.Fatalf("inject not persisted: %v", sb.Env())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ := svc.Get(id)
		if cur != nil && cur.Status == "running" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/hosts/8765", nil))
	if w.Code != 200 {
		t.Fatalf("hosts exposed=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/hosts/12345", nil))
	if w.Code != 404 {
		t.Fatalf("hosts missing want 404 got %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes",
		bytes.NewBufferString(`{"resources":{"cpuCores":99}}`)))
	if w.Code != 400 {
		t.Fatalf("over-limit want 400 got %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/does-not-exist", nil))
	if w.Code != 404 {
		t.Fatalf("get missing want 404 got %d", w.Code)
	}
}

func TestBearerToken(t *testing.T) {
	if bearerToken("Bearer abc") != "abc" {
		t.Fatal("Bearer abc")
	}
	if bearerToken("bearer xyz") != "xyz" {
		t.Fatal("case insensitive")
	}
	if bearerToken("Token x") != "" {
		t.Fatal("non-bearer")
	}
}

func waitRunning(t *testing.T, svc *service.SandboxService, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ := svc.Get(id)
		if cur != nil && cur.Status == "running" {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("sandbox %s did not become running", id)
}

func createSandbox(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes",
		bytes.NewBufferString(`{"labels":{"t":"1"}}`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("no id: %v", created)
	}
	return id
}

func TestCreateBadJSON(t *testing.T) {
	r, _, _ := testRouter(t, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes",
		bytes.NewBufferString(`{not-json`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestInvalidHostPort(t *testing.T) {
	r, _, svc := testRouter(t, nil)
	id := createSandbox(t, r)
	waitRunning(t, svc, id)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/hosts/notaport", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestListLabelFilter(t *testing.T) {
	r, _, svc := testRouter(t, nil)
	body := `{"labels":{"owner":"team-a","env":"prod"}}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes", bytes.NewBufferString(body)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	waitRunning(t, svc, id)

	// second sandbox with different labels
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes",
		bytes.NewBufferString(`{"labels":{"owner":"team-b","env":"prod"}}`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create2=%d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes?label=owner:team-a", nil))
	if w.Code != 200 {
		t.Fatalf("list filter=%d %s", w.Code, w.Body.String())
	}
	var listResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	sandboxes, _ := listResp["sandboxes"].([]any)
	if len(sandboxes) != 1 {
		t.Fatalf("want 1 sandbox, got %v", listResp)
	}
	item, _ := sandboxes[0].(map[string]any)
	if item["id"] != id {
		t.Fatalf("want id %s got %v", id, item["id"])
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes?label=owner:team-a&label=env:prod", nil))
	if w.Code != 200 {
		t.Fatalf("list AND=%d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes?label=bad", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad label want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestLifecycleStartStopDestroyStatusList(t *testing.T) {
	r, _, svc := testRouter(t, nil)
	id := createSandbox(t, r)
	waitRunning(t, svc, id)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes", nil))
	if w.Code != 200 {
		t.Fatalf("list=%d %s", w.Code, w.Body.String())
	}
	var listResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	sandboxes, _ := listResp["sandboxes"].([]any)
	if len(sandboxes) < 1 {
		t.Fatalf("list empty: %v", listResp)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/status", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/stop", nil))
	if w.Code != 200 {
		t.Fatalf("stop=%d %s", w.Code, w.Body.String())
	}
	sb, _ := svc.Get(id)
	if sb.Status != "stopped" {
		t.Fatalf("status after stop=%s", sb.Status)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/start", nil))
	if w.Code != 200 {
		t.Fatalf("start=%d %s", w.Code, w.Body.String())
	}
	waitRunning(t, svc, id)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/sandboxes/"+id, nil))
	if w.Code != 200 {
		t.Fatalf("destroy=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id, nil))
	if w.Code != 404 {
		t.Fatalf("get after destroy want 404 got %d", w.Code)
	}
}

func TestReinstallAccepted(t *testing.T) {
	r, _, svc := testRouter(t, nil)
	id := createSandbox(t, r)
	waitRunning(t, svc, id)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/reinstall",
		bytes.NewBufferString(`{"preserveData":true}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("reinstall want 202 got %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "reinstalling" {
		t.Fatalf("%v", body)
	}
	if body["preserveData"] != true {
		t.Fatalf("preserveData: %v", body)
	}
	// Async reinstall must finish before TempDir cleanup (sqlite still open otherwise).
	waitRunning(t, svc, id)

	// missing sandbox
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/missing/reinstall",
		bytes.NewBufferString(`{}`)))
	if w.Code != 404 {
		t.Fatalf("missing reinstall want 404 got %d", w.Code)
	}
}

func TestLifecycleNotFound(t *testing.T) {
	r, _, _ := testRouter(t, nil)
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/sandboxes/nope/start"},
		{http.MethodPost, "/api/v1/sandboxes/nope/stop"},
		{http.MethodDelete, "/api/v1/sandboxes/nope"},
		{http.MethodGet, "/api/v1/sandboxes/nope/status"},
		{http.MethodGet, "/api/v1/sandboxes/nope/logs"},
		{http.MethodGet, "/api/v1/sandboxes/nope/exit"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != 404 {
			t.Fatalf("%s %s want 404 got %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestSandboxLogsEndpoint(t *testing.T) {
	r, drv, svc := testRouter(t, nil)
	id := createSandbox(t, r)
	waitRunning(t, svc, id)
	drv.SetLogs(id, "[boot] sandbox started\n")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/logs?tail=100", nil))
	if w.Code != 200 {
		t.Fatalf("logs=%d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["content"] != "[boot] sandbox started\n" {
		t.Fatalf("content=%v", body["content"])
	}

	// Successful empty content.
	drv.SetLogs(id, "")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/logs", nil))
	if w.Code != 200 {
		t.Fatalf("empty logs=%d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["content"] != "" {
		t.Fatalf("want empty content, got %v", body["content"])
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/logs?tail=abc", nil))
	if w.Code != 400 {
		t.Fatalf("bad tail want 400 got %d", w.Code)
	}
}

func TestSandboxExitEndpoint(t *testing.T) {
	r, drv, svc := testRouter(t, nil)
	id := createSandbox(t, r)
	waitRunning(t, svc, id)

	get := func() map[string]any {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandboxes/"+id+"/exit", nil))
		if w.Code != 200 {
			t.Fatalf("exit=%d %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if body := get(); body["exit"] != nil {
		t.Fatalf("never exited: %v", body)
	}
	drv.SetExit(id, &driver.ExitInfo{Reason: "OOMKilled", ExitCode: 137, OOMKilled: true, MemoryMB: 8192})
	e, _ := get()["exit"].(map[string]any)
	if e["reason"] != "OOMKilled" || e["oomKilled"] != true || e["memoryMB"] != float64(8192) {
		t.Fatalf("exit=%v", e)
	}
}

func TestPublishPortEndpoint(t *testing.T) {
	r, _, svc := testRouter(t, nil)
	id := createSandbox(t, r)
	waitRunning(t, svc, id)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/ports",
		bytes.NewBufferString(`{"port":8765}`)))
	if w.Code != 200 {
		t.Fatalf("existing port=%d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/ports",
		bytes.NewBufferString(`{"port":5173}`)))
	if w.Code != 404 {
		t.Fatalf("unpublished port want 404 got %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/ports",
		bytes.NewBufferString(`{"port":0}`)))
	if w.Code != 400 {
		t.Fatalf("port 0 want 400 got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/ports",
		bytes.NewBufferString(`{`)))
	if w.Code != 400 {
		t.Fatalf("bad json want 400 got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/missing/ports",
		bytes.NewBufferString(`{"port":80}`)))
	if w.Code != 404 {
		t.Fatalf("missing sandbox want 404 got %d", w.Code)
	}
}
