package api_test

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
	"time"

	"sandbox-gateway/internal/api"
	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/driver"
	driverfake "sandbox-gateway/internal/driver/fake"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"
)

type releaseAPIProvider struct {
	*driverfake.Driver
	plan                           driver.EnvironmentReleasePlan
	previewErr, deleteErr, goneErr error
	gone                           bool
	deletes                        int
}

func (p *releaseAPIProvider) PreviewEnvironmentRelease(context.Context, string) (driver.EnvironmentReleasePlan, error) {
	return p.plan, p.previewErr
}
func (p *releaseAPIProvider) CheckEnvironmentRelease(context.Context, driver.EnvironmentReleasePlan) error {
	return p.previewErr
}
func (p *releaseAPIProvider) DeleteEnvironmentRelease(ctx context.Context, _ driver.EnvironmentReleasePlan, guard func(context.Context) error) error {
	if err := guard(ctx); err != nil {
		return err
	}
	p.deletes++
	if p.deleteErr != nil {
		return p.deleteErr
	}
	p.gone = true
	return nil
}
func (p *releaseAPIProvider) EnvironmentReleaseGone(context.Context, driver.EnvironmentReleasePlan) (bool, error) {
	return p.gone, p.goneErr
}
func releaseAPIFixture(t *testing.T) (http.Handler, *service.EnvironmentReleases, *releaseAPIProvider) {
	t.Helper()
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "release.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	p := &releaseAPIProvider{Driver: driverfake.New(), plan: driver.EnvironmentReleasePlan{Version: driver.EnvironmentReleaseVersion, Resource: "api-owned", Namespace: "owned", Objects: []driver.EnvironmentReleaseObject{{Kind: "Deployment", Namespace: "owned", Name: "d", UID: "dep-original"}, {Kind: "PersistentVolumeClaim", Namespace: "owned", Name: "c", UID: "pvc-original"}, {Kind: "PersistentVolume", Name: "physical", UID: "pv-original", ObserveOnly: true}}}}
	p.plan.SHA256 = p.plan.Digest()
	sb := models.Sandbox{ID: p.plan.Resource, Status: models.StatusStopped}
	sb.SetEnv(map[string]string{"GRASP_EXECUTION_LINEAGE": "api-lineage"})
	if err := db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.New(p, store.New(db), service.Config{})
	releases := service.NewEnvironmentReleases(db, svc, service.EnvironmentReleaseOptions{})
	cfg := config.Default()
	cfg.Auth.APIKeys = []string{"synthetic-release-key"}
	return api.NewRouter(api.NewHandler(svc, config.PortsConfig{}), cfg), releases, p
}
func releaseAPIRequest(t *testing.T, h http.Handler, method, path string, input any, authorized bool) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if raw, ok := input.(string); ok {
		body = []byte(raw)
	} else if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authorized {
		req.Header.Set("Authorization", "Bearer synthetic-release-key")
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	return out
}

const releaseAPIBase = "/api/v1/sandboxes/api-owned/releases"

func releaseAPIPreview(t *testing.T, h http.Handler) service.EnvironmentReleaseView {
	t.Helper()
	w := releaseAPIRequest(t, h, "POST", releaseAPIBase, service.EnvironmentReleasePrepare{ID: "manual-original", Lineage: "api-lineage"}, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out service.EnvironmentReleaseView
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func releaseAPIConfirm(view service.EnvironmentReleaseView) service.EnvironmentReleaseConfirm {
	return service.EnvironmentReleaseConfirm{PlanSHA256: view.Plan.SHA256, Nonce: "manual-loss-accepted", Confirmed: true, LossAccepted: true, Owner: "worker-one", Generation: 1, ExpiresAt: time.Now().Add(40 * time.Second), Deadline: time.Now().Add(4 * time.Minute)}
}
func TestEnvironmentReleaseAuthenticatedHTTPProtocolAndStableReceipt(t *testing.T) {
	h, worker, p := releaseAPIFixture(t)
	if w := releaseAPIRequest(t, h, "GET", releaseAPIBase+"/capabilities", nil, false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := releaseAPIRequest(t, h, "GET", releaseAPIBase+"/capabilities", nil, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"supported":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	view := releaseAPIPreview(t, h)
	confirm := releaseAPIConfirm(view)
	invalid := confirm
	invalid.LossAccepted = false
	if w := releaseAPIRequest(t, h, "POST", releaseAPIBase+"/manual-original/confirm", invalid, true); w.Code != 400 {
		t.Fatal("missing manual loss accepted", w.Code)
	}
	if w := releaseAPIRequest(t, h, "POST", releaseAPIBase+"/manual-original/confirm", confirm, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if p.deletes != 0 {
		t.Fatal("HTTP dispatched native deletion")
	}
	if w := releaseAPIRequest(t, h, "GET", releaseAPIBase+"/manual-original", nil, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"confirmed"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	renew := service.EnvironmentReleaseRenew{Owner: confirm.Owner, Generation: 1, ExpiresAt: time.Now().Add(44 * time.Second)}
	if w := releaseAPIRequest(t, h, "PATCH", releaseAPIBase+"/manual-original/fence", renew, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	renew.Generation = 2
	if w := releaseAPIRequest(t, h, "PATCH", releaseAPIBase+"/manual-original/fence", renew, true); w.Code != 409 {
		t.Fatal("wrong generation renewed", w.Code)
	}
	if err := worker.RunOne(t.Context(), view.ID); err != nil {
		t.Fatal(err)
	}
	if w := releaseAPIRequest(t, h, "GET", releaseAPIBase+"/manual-original", nil, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"released"`) || !strings.Contains(w.Body.String(), `"receipt"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := releaseAPIRequest(t, h, "POST", releaseAPIBase+"/manual-original/confirm", confirm, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if p.deletes != 1 {
		t.Fatal("original receipt replayed native mutation")
	}
	if w := releaseAPIRequest(t, h, "GET", releaseAPIBase+"/foreign-operation", nil, true); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestEnvironmentReleaseHTTPRejectsUntrustedIntentAndMapsNativeFailures(t *testing.T) {
	h, _, p := releaseAPIFixture(t)
	for _, suffix := range []string{"", "/manual-original/confirm", "/manual-original/fence"} {
		method := "POST"
		if strings.HasSuffix(suffix, "fence") {
			method = "PATCH"
		}
		for _, body := range []string{`{"arbitraryPath":"/host"}`, `{} {}`, strings.Repeat(" ", 8200) + `{}`} {
			if w := releaseAPIRequest(t, h, method, releaseAPIBase+suffix, body, true); w.Code != 400 {
				t.Fatal(suffix, w.Code)
			}
		}
	}
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{driver.ErrEnvironmentReleaseConflict, 409, "environment_release_scope_changed"}, {driver.ErrEnvironmentReleasePending, 409, "environment_release_pending"}, {driver.ErrEnvironmentReleaseUnsupported, 501, "environment_release_unsupported"}, {errors.New("native secret private-host"), 503, "environment_release_unavailable"}} {
		p.previewErr = test.err
		w := releaseAPIRequest(t, h, "POST", releaseAPIBase, service.EnvironmentReleasePrepare{ID: "manual-original", Lineage: "api-lineage"}, true)
		if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) || strings.Contains(w.Body.String(), "private-host") {
			t.Fatal(w.Code, w.Body.String())
		}
	}

}
func TestEnvironmentReleaseHTTPUnknownReceiptIsReadOnly(t *testing.T) {
	h, worker, p := releaseAPIFixture(t)
	view := releaseAPIPreview(t, h)
	if w := releaseAPIRequest(t, h, "POST", releaseAPIBase+"/manual-original/confirm", releaseAPIConfirm(view), true); w.Code != 202 {
		t.Fatal(w.Code)
	}
	p.deleteErr = errors.New("native response lost")
	if err := worker.RunOne(t.Context(), view.ID); err == nil {
		t.Fatal("lost response silently succeeded")
	}
	p.goneErr = errors.New("temporary API outage")
	if w := releaseAPIRequest(t, h, "GET", releaseAPIBase+"/manual-original", nil, true); w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	p.goneErr = nil
	if w := releaseAPIRequest(t, h, "GET", releaseAPIBase+"/manual-original", nil, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"unknown"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if p.deletes != 1 {
		t.Fatal("unknown read repeated deletion")
	}
}
