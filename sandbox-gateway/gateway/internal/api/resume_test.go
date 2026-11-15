package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"sandbox-gateway/internal/driver"
)

func TestStartRejectsMalformedOrOversizedPreparation(t *testing.T) {
	r, _, _ := testRouter(t, nil)
	for _, body := range []string{"{", `{"restoreEnv":{"TOKEN":1}}`, `{"replaceEnvKeys":"TOKEN"}`, `{"bootstrapScript":"` + strings.Repeat("x", 256<<10) + `"}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/resident/start", bytes.NewBufferString(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad preparation returned %d: %s", w.Code, w.Body.String())
		}
	}
}

func TestColdStartAPIRefreshesEnvironmentAndExposesOnlyKeysAndOriginalWorkspace(t *testing.T) {
	r, drv, svc := testRouter(t, nil)
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		if w.Code != status {
			t.Fatalf("%s %s returned %d: %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	decode := func(w *httptest.ResponseRecorder, keys []string) sandboxDTO {
		t.Helper()
		var dto sandboxDTO
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatal(err)
		}
		if dto.WorkspaceDir != "/original/workspace" || !reflect.DeepEqual(dto.EnvKeys, keys) {
			t.Fatalf("wrong restore metadata: %+v", dto)
		}
		for _, secret := range []string{"old-credential", "ssh-credential", "new-credential", "fresh-config-grant"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("sandbox metadata disclosed environment values")
			}
		}
		return dto
	}
	w := request(http.MethodPost, "/api/v1/sandboxes",
		`{"workspaceDir":"/original/workspace","env":{"OLD_TOKEN":"old-credential","SSH_KEY":"ssh-credential"}}`, http.StatusAccepted)
	created := decode(w, []string{"OLD_TOKEN", "SSH_KEY"})
	waitRunning(t, svc, created.ID)
	path := "/api/v1/sandboxes/" + created.ID
	decode(request(http.MethodGet, path, "", http.StatusOK), []string{"OLD_TOKEN", "SSH_KEY"})
	request(http.MethodPost, path+"/stop", `{"backupKeep":1}`, http.StatusOK)
	request(http.MethodPost, path+"/start", `{"replaceEnvKeys":["OLD_TOKEN"],"restoreEnv":{"NEW_TOKEN":"new-credential","SANDBOX_INJECT":"fresh-config-grant"}}`, http.StatusOK)
	waitRunning(t, svc, created.ID)
	decode(request(http.MethodGet, path, "", http.StatusOK), []string{"NEW_TOKEN", "SANDBOX_INJECT", "SSH_KEY"})
	spec := drv.LastCreate()
	if spec.Env["NEW_TOKEN"] != "new-credential" || spec.Env["SSH_KEY"] != "ssh-credential" || spec.Env["SANDBOX_INJECT"] != "fresh-config-grant" || spec.Env["OLD_TOKEN"] != "" || spec.WorkspaceDir != "/original/workspace" {
		t.Fatal("HTTP resume inputs did not reach the recreated workload")
	}
	// List uses the same safe restore metadata rather than returning Env itself.
	w = request(http.MethodGet, "/api/v1/sandboxes", "", http.StatusOK)
	var list struct {
		Sandboxes []json.RawMessage `json:"sandboxes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Sandboxes) != 1 {
		t.Fatalf("unexpected list: %s %v", w.Body.String(), err)
	}
	w.Body.Reset()
	_, _ = w.Body.Write(list.Sandboxes[0])
	decode(w, []string{"NEW_TOKEN", "SANDBOX_INJECT", "SSH_KEY"})
	if status, err := drv.Status(t.Context(), created.ID); err != nil || status != driver.StatusRunning {
		t.Fatalf("restored workload is unavailable: %s %v", status, err)
	}
}

func TestStopRejectsInvalidRetentionBeforeChangingSandbox(t *testing.T) {
	r, _, svc := testRouter(t, nil)
	id := createSandbox(t, r)
	waitRunning(t, svc, id)
	for _, body := range []string{"{", `{"backupKeep":0}`, `{"backupKeep":-1}`, `{"backupKeep":"five"}`, `{"padding":"` + strings.Repeat("x", 1024) + `"}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sandboxes/"+id+"/stop", bytes.NewBufferString(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid stop returned %d: %s", w.Code, w.Body.String())
		}
		if sb, err := svc.Get(id); err != nil || sb.Status != "running" {
			t.Fatalf("invalid request changed sandbox: %+v %v", sb, err)
		}
	}
}
