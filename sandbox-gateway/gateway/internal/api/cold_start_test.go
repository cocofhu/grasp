package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/driver/fake"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"
)

type coldAPIProvider struct {
	*fake.Driver
	starts, reads int
	receipt       driver.ColdStartReceipt
}

func (p *coldAPIProvider) Status(context.Context, string) (driver.Status, error) {
	return driver.StatusStopped, nil
}
func (p *coldAPIProvider) PreviewColdStart(context.Context, string) (*driver.ColdStartTarget, error) {
	return &driver.ColdStartTarget{NativeIdentity: "original-native", WorkspaceDir: "/root/workspace"}, nil
}
func (p *coldAPIProvider) StartConfigured(_ context.Context, _ string, opts driver.StartOptions) (*driver.ColdStartReceipt, error) {
	p.starts++
	c := opts.ColdConfig
	p.receipt = driver.ColdStartReceipt{RequestID: c.RequestID, InputSHA256: c.InputSHA256, PayloadSHA256: c.PayloadSHA256, NativeIdentity: "original-native", WorkspaceDir: "/root/workspace", State: "unknown"}
	copy := p.receipt
	return &copy, nil
}
func (p *coldAPIProvider) GetColdStart(context.Context, string, string) (*driver.ColdStartReceipt, error) {
	p.reads++
	copy := p.receipt
	return &copy, nil
}

func coldAPIFixture(t *testing.T, keys []string) (http.Handler, *coldAPIProvider) {
	t.Helper()
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "gateway.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Create(&models.Sandbox{ID: "owned-original", Status: models.StatusStopped}).Error; err != nil {
		t.Fatal(err)
	}
	p := &coldAPIProvider{Driver: fake.New()}
	t.Cleanup(p.Close)
	svc := service.New(p, store.New(db), service.Config{})
	cfg := config.Default()
	cfg.Auth.APIKeys = keys
	cfg.Driver = "kubernetes"
	return NewRouter(NewHandler(svc, cfg.Image.Ports), cfg), p
}
func coldAPIOptions() driver.StartOptions {
	env := map[string]string{"ROOT_PASSWORD": "synthetic-root", "ACP_BRIDGE_PASSWORD": "synthetic-bridge", "CONFIG_ROOT": "/root/.cursor", "WORKSPACE_DIR": "/root/workspace", "GIT_REPOS": "", "GRASP_RUNTIME_URL": "http://platform/runtime", "SANDBOX_INJECT_HEADERS": "Authorization: Bearer synthetic", "SANDBOX_INJECT": ""}
	c := &driver.ColdStartConfig{ExpectedNativeIdentity: "original-native", RequestID: "original-request", InputSHA256: strings.Repeat("a", 64), Env: env, Injections: []driver.ColdConfigInjection{{URL: "http://platform/git", Destination: "/tmp/grasp-ssh-inject", Bytes: 10, SHA256: strings.Repeat("b", 64)}, {URL: "http://platform/config", Destination: env["CONFIG_ROOT"], Bytes: 20, SHA256: strings.Repeat("c", 64)}}}
	opts := driver.StartOptions{BootstrapScript: "#!/bin/bash\ntrue\n", RuntimeURL: env["GRASP_RUNTIME_URL"], RuntimeHeaders: env["SANDBOX_INJECT_HEADERS"], ColdConfig: c}
	c.PayloadSHA256 = driver.ColdStartPayloadSHA256(opts)
	return opts
}
func coldAPIRequest(h http.Handler, method, path, key string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestColdConfigurationAPIRequiresConfiguredAuthenticationAndDoesNotLeakInputs(t *testing.T) {
	for _, keys := range [][]string{nil, {"platform-key"}} {
		h, p := coldAPIFixture(t, keys)
		for _, method := range []string{"POST", "GET"} {
			want := 401
			if len(keys) == 0 {
				want = 503
			}
			w := coldAPIRequest(h, method, "/api/v1/sandboxes/owned-original/cold-starts", "", []byte(`{}`))
			if w.Code != want || p.starts != 0 || p.reads != 0 {
				t.Fatalf("unauthenticated cold access: %d %s", w.Code, w.Body.String())
			}
		}
	}
	h, p := coldAPIFixture(t, []string{"platform-key"})
	for _, body := range []string{`{`, `{"unrecognized":"synthetic-secret"}`, `{} {}`, strings.Repeat("x", (512<<10)+1)} {
		w := coldAPIRequest(h, "POST", "/api/v1/sandboxes/owned-original/cold-starts", "platform-key", []byte(body))
		if w.Code != 400 || strings.Contains(w.Body.String(), "synthetic-secret") {
			t.Fatalf("invalid body not safely rejected: %d", w.Code)
		}
	}
	if p.starts != 0 {
		t.Fatal("invalid request reached provider")
	}
}

func TestColdConfigurationAPIKeepsOriginalPendingReceiptAndConflictsOnChangedInput(t *testing.T) {
	h, p := coldAPIFixture(t, []string{"platform-key"})
	base := "/api/v1/sandboxes/owned-original/cold-starts"
	if w := coldAPIRequest(h, "GET", base, "platform-key", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := coldAPIRequest(h, "GET", base+"/other-request", "platform-key", nil); w.Code != 404 || p.reads != 0 {
		t.Fatal("foreign nonce reached provider", w.Code)
	}
	opts := coldAPIOptions()
	body, _ := json.Marshal(opts)
	for range 2 {
		w := coldAPIRequest(h, "POST", base, "platform-key", body)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"unknown"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := coldAPIRequest(h, "GET", base+"/original-request", "platform-key", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := coldAPIRequest(h, "GET", base, "platform-key", nil); w.Code != 409 {
		t.Fatal("unknown resource was offered as a new target", w.Code)
	}
	opts.ColdConfig.Env["MODEL_TOKEN"] = "synthetic-change"
	opts.ColdConfig.PayloadSHA256 = driver.ColdStartPayloadSHA256(opts)
	changed, _ := json.Marshal(opts)
	w := coldAPIRequest(h, "POST", base, "platform-key", changed)
	if w.Code != 409 || strings.Contains(w.Body.String(), "synthetic-change") || p.starts != 1 {
		t.Fatalf("changed input reached provider/leaked credentials: %d starts=%d", w.Code, p.starts)
	}
}

func TestColdConfigurationErrorsUseSafeStatusMetadata(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{store.ErrNotFound, 404}, {driver.ErrColdStartUnsupported, 501}, {driver.ErrColdStartUnknown, 409}, {driver.ErrColdStartConflict, 409}, {errors.New("synthetic-private-provider-details"), 500}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		(&Handler{}).coldStartError(c, tc.err)
		if w.Code != tc.code || strings.Contains(w.Body.String(), "synthetic-private") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
