package handlers_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type doctorSessionResponse struct {
	ID, RunA, RunB, TokenA, TokenB, CleanupToken string
	Bootstrap                                    *sandbox.DoctorBootstrap
}

func doctorSessionRequest(h *harness, method, path, cleanup string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer doctor-test-secret")
	if cleanup != "" {
		req.Header.Set("X-Grasp-Doctor-Cleanup", cleanup)
	}
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w
}

func decodeDoctorSession(t *testing.T, w *httptest.ResponseRecorder) doctorSessionResponse {
	t.Helper()
	if w.Code != http.StatusCreated {
		t.Fatal("session was not accepted", w.Code, w.Body.String())
	}
	var response struct {
		ID           string                   `json:"id"`
		RunA         string                   `json:"run_a"`
		RunB         string                   `json:"run_b"`
		TokenA       string                   `json:"token_a"`
		TokenB       string                   `json:"token_b"`
		CleanupToken string                   `json:"cleanup_token"`
		Bootstrap    *sandbox.DoctorBootstrap `json:"bootstrap"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return doctorSessionResponse{response.ID, response.RunA, response.RunB, response.TokenA, response.TokenB, response.CleanupToken, response.Bootstrap}
}

func configureDoctorRuntime(t *testing.T, h *harness) (*sandbox.BundleStore, []byte) {
	t.Helper()
	var data bytes.Buffer
	zip := gzip.NewWriter(&data)
	tarball := tar.NewWriter(zip)
	for name, body := range map[string]string{
		"MANIFEST":                   "version=" + strings.Repeat("a", 64) + "\narch=amd64\nmin_image=1\n",
		"scripts/startup.sh":         "#!/bin/bash\n",
		"scripts/grasp-bootstrap.sh": "#!/bin/bash\n",
	} {
		if err := tarball.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarball.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarball.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "runtime.tgz")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	store := sandbox.NewBundleStore()
	manager := sandbox.NewManager(h.fg.Client(), sandbox.ManagerOptions{
		Runtime: sandbox.NewRuntimeBundle(path), InjectStore: store, InjectAdvertise: "http://diagnostic.example.test",
	})
	h.h.Sbx = services.NewSandboxService(h.db, manager, h.h.Agents, h.host, services.SandboxOptions{})
	return store, data.Bytes()
}

func doctorAuthority(t *testing.T, h *harness) *executionlineage.SQLAuthority {
	t.Helper()
	authority, err := executionlineage.OpenSQL(t.Context(), h.db)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(t.Context(), h.db, owner, nil); err != nil {
		t.Fatal(err)
	}
	ticket, err := authority.Initialize(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := executionlineage.EnsureRecoverySchema(t.Context(), h.db, owner); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error { _, err := authority.QuarantineTx(tx, owner, ticket); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Activate(t.Context(), h.db, owner, ticket); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(t.Context(), h.db, owner); err != nil {
		t.Fatal(err)
	}
	controller, err := executionlineage.NewLocalController(t.Context(), h.db)
	if err != nil {
		t.Fatal(err)
	}
	h.h.Arts.SetExecutionController(controller)
	return authority
}

func TestDoctorSandboxSessionUsesRealGuardedRunsAndScopedBundleCleanup(t *testing.T) {
	t.Setenv("GRASP_DOCTOR_TOKEN", "doctor-test-secret")
	h := newHarness(t)
	store, runtime := configureDoctorRuntime(t, h)
	authority := doctorAuthority(t, h)
	first := decodeDoctorSession(t, doctorSessionRequest(h, http.MethodPost, "/_internal/doctor/artifact-sessions?sandbox=true", ""))
	second := decodeDoctorSession(t, doctorSessionRequest(h, http.MethodPost, "/_internal/doctor/artifact-sessions?sandbox=true", ""))
	fetch := func(s doctorSessionResponse) bool {
		t.Helper()
		if s.Bootstrap == nil || s.Bootstrap.Validate() != nil {
			t.Fatal("response did not include a complete restricted bootstrap")
		}
		url := s.Bootstrap.Env["GRASP_RUNTIME_URL"]
		id := url[strings.LastIndex(url, "/")+1:]
		token := strings.TrimPrefix(s.Bootstrap.Env["SANDBOX_INJECT_HEADERS"], "Authorization: Bearer ")
		got, ok := store.Get(id, token)
		return ok && bytes.Equal(got, runtime)
	}
	if !fetch(first) || !fetch(second) {
		t.Fatal("session bundle did not contain the actual configured runtime")
	}
	var count int64
	if err := h.db.Model(&models.Run{}).Where("id IN ? AND status = ? AND created_by = ?", []string{first.RunA, first.RunB}, "completed", "grasp-doctor").Count(&count).Error; err != nil || count != 2 {
		t.Fatal("bootstrap lacked its real terminal diagnostic Runs", count, err)
	}
	call := func(runID, token, tool string, arguments map[string]string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp/runs/"+runID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.r.ServeHTTP(w, req)
		return w
	}
	if w := call(first.RunA, first.TokenA, "write_artifact", map[string]string{"name": "proof.txt", "kind": "text", "content": "owned proof"}); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatal("guarded diagnostic write failed", w.Code, w.Body.String())
	}
	if w := call(first.RunA, first.TokenA, "read_artifact", map[string]string{"name": "proof.txt"}); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "owned proof") {
		t.Fatal("same-run read failed", w.Code)
	}
	if w := call(first.RunB, first.TokenA, "read_artifact", map[string]string{"name": "proof.txt"}); w.Code != http.StatusUnauthorized {
		t.Fatal("another Run accepted the original token", w.Code)
	}
	if w := call(first.RunB, first.TokenB, "read_artifact", map[string]string{"name": "proof.txt"}); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatal("another Run could see the diagnostic artifact", w.Code)
	}
	if w := doctorSessionRequest(h, http.MethodDelete, "/_internal/doctor/artifact-sessions/"+first.ID, second.CleanupToken); w.Code != http.StatusNotFound || !fetch(first) {
		t.Fatal("foreign cleanup altered a diagnostic session", w.Code)
	}
	if w := doctorSessionRequest(h, http.MethodDelete, "/_internal/doctor/artifact-sessions/"+first.ID, first.CleanupToken); w.Code != http.StatusNoContent || fetch(first) || !fetch(second) {
		t.Fatal("cleanup did not revoke exactly its own bundle", w.Code)
	}
	if err := h.db.Model(&models.Run{}).Where("id IN ?", []string{first.RunA, first.RunB}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("cleanup retained its diagnostic Run pair", count, err)
	}
	if w := call(first.RunA, first.TokenA, "read_artifact", map[string]string{"name": "proof.txt"}); w.Code != http.StatusUnauthorized {
		t.Fatal("cleanup retained the old MCP token", w.Code)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(t.Context(), h.db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.BeginRestore(t.Context(), owner, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if w := doctorSessionRequest(h, http.MethodDelete, "/_internal/doctor/artifact-sessions/"+second.ID, second.CleanupToken); w.Code != http.StatusNotFound || fetch(second) {
		t.Fatal("revoked authority retained a download credential or bypassed SQL cleanup guard", w.Code)
	}
	if err := h.db.Model(&models.Run{}).Where("id IN ?", []string{second.RunA, second.RunB}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("revoked diagnostic metadata was removed", count, err)
	}
	if w := doctorSessionRequest(h, http.MethodPost, "/_internal/doctor/artifact-sessions?sandbox=true", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatal("revoked authority provisioned another sandbox bootstrap", w.Code)
	}
}

func TestDoctorSandboxSessionUnavailableRuntimeCleansOnlyItsNewRuns(t *testing.T) {
	t.Setenv("GRASP_DOCTOR_TOKEN", "doctor-test-secret")
	h := newHarness(t)
	ordinary := decodeDoctorSession(t, doctorSessionRequest(h, http.MethodPost, "/_internal/doctor/artifact-sessions", ""))
	t.Cleanup(func() {
		doctorSessionRequest(h, http.MethodDelete, "/_internal/doctor/artifact-sessions/"+ordinary.ID, ordinary.CleanupToken)
	})
	if ordinary.Bootstrap != nil {
		t.Fatal("ordinary MCP diagnostic unexpectedly received sandbox credentials")
	}
	for _, option := range []string{"true", "invalid"} {
		want := http.StatusServiceUnavailable
		if option == "invalid" {
			want = http.StatusBadRequest
		}
		if w := doctorSessionRequest(h, http.MethodPost, "/_internal/doctor/artifact-sessions?sandbox="+option, ""); w.Code != want {
			t.Fatal("invalid bootstrap request was accepted", w.Code)
		}
	}
	var count int64
	if err := h.db.Model(&models.Run{}).Where("launch_source = ?", "doctor").Count(&count).Error; err != nil || count != 2 {
		t.Fatal("bootstrap failure leaked its own Runs or removed the existing session", count, err)
	}
}
