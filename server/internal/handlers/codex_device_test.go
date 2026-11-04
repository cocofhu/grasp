package handlers_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/services"
)

func codexTestJWT(claims map[string]any) string {
	body, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(body) + ".sig"
}

func newDeviceOpenAI(t *testing.T, authorized *atomic.Bool, disabled bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/usercode", func(w http.ResponseWriter, _ *http.Request) {
		if disabled {
			http.NotFound(w, nil)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"device_auth_id": "dev-secret", "user_code": "WXYZ-0000", "interval": "5"})
	})
	mux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, _ *http.Request) {
		if !authorized.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"authorization_code": "code", "code_verifier": "verifier"})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id_token":      codexTestJWT(map[string]any{"email": "dev@example.com"}),
			"access_token":  codexTestJWT(map[string]any{"exp": time.Now().Add(240 * time.Hour).Unix()}),
			"refresh_token": "rt-handler-secret",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCodexDeviceRoutesUnavailable(t *testing.T) {
	hn := newHarness(t)
	if w := hn.do(http.MethodGet, "/api/workspace/credentials/codex/device", nil); !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("status %s", w.Body.String())
	}
	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/api/workspace/credentials/codex/device"},
		{http.MethodPost, "/api/workspace/credentials/codex/device/cxd-x/poll"},
		{http.MethodDelete, "/api/workspace/credentials/codex/device/cxd-x"},
	} {
		if w := hn.do(req.method, req.path, map[string]any{"name": "x"}); w.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d", req.method, req.path, w.Code)
		}
	}
}

func TestCodexDeviceRoutesSignIn(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	var authorized atomic.Bool
	api := newDeviceOpenAI(t, &authorized, false)
	svc := services.NewCodexDeviceFlowService(hn.h.Credentials, api.URL)
	clock := time.Now()
	svc.SetClock(func() time.Time { return clock })
	hn.h.CodexDevice = svc

	if w := hn.do(http.MethodGet, "/api/workspace/credentials/codex/device", nil); !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatalf("status %s", w.Body.String())
	}
	if w := hn.do(http.MethodPost, "/api/workspace/credentials/codex/device", map[string]any{"name": ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("blank name=%d", w.Code)
	}
	w := hn.do(http.MethodPost, "/api/workspace/credentials/codex/device", map[string]any{"name": "ChatGPT"})
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "dev-secret") || !strings.Contains(w.Body.String(), api.URL+"/codex/device") {
		t.Fatalf("start=%d %s", w.Code, w.Body.String())
	}
	var start services.CodexDeviceStart
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	poll := "/api/workspace/credentials/codex/device/" + start.SessionID + "/poll"
	clock = clock.Add(6 * time.Second)
	if w := hn.do(http.MethodPost, poll, nil); !strings.Contains(w.Body.String(), `"status":"pending"`) {
		t.Fatalf("pending %s", w.Body.String())
	}
	authorized.Store(true)
	clock = clock.Add(6 * time.Second)
	w = hn.do(http.MethodPost, poll, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"success"`) || strings.Contains(w.Body.String(), "rt-handler-secret") {
		t.Fatalf("success=%d %s", w.Code, w.Body.String())
	}
	if w := hn.do(http.MethodPost, poll, nil); w.Code != http.StatusNotFound {
		t.Fatalf("finished session=%d", w.Code)
	}
	if w := hn.do(http.MethodDelete, "/api/workspace/credentials/codex/device/"+start.SessionID, nil); w.Code != http.StatusOK {
		t.Fatalf("cancel=%d", w.Code)
	}
	list := hn.do(http.MethodGet, "/api/workspace/credentials", nil)
	if strings.Contains(list.Body.String(), "rt-handler-secret") {
		t.Fatal("credential list leaked the refresh token")
	}
	if w := hn.do(http.MethodPost, "/api/workspace/credentials/codex/device", map[string]any{"credentialId": "cred-missing"}); w.Code != http.StatusNotFound {
		t.Fatalf("missing target=%d", w.Code)
	}
}

func TestCodexDeviceRoutesNotEnabledAndUpstream(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	var authorized atomic.Bool
	api := newDeviceOpenAI(t, &authorized, true)
	hn.h.CodexDevice = services.NewCodexDeviceFlowService(hn.h.Credentials, api.URL)
	w := hn.do(http.MethodPost, "/api/workspace/credentials/codex/device", map[string]any{"name": "x"})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"not_enabled"`) {
		t.Fatalf("not enabled=%d %s", w.Code, w.Body.String())
	}
	api.Close()
	w = hn.do(http.MethodPost, "/api/workspace/credentials/codex/device", map[string]any{"name": "x"})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("upstream=%d %s", w.Code, w.Body.String())
	}
}

func TestCodexLoginPasteValidationIs400(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	for _, value := range []string{"not json", `{"auth_mode":"chatgpt","tokens":{"access_token":"a"}}`} {
		w := hn.do(http.MethodPost, "/api/workspace/credentials", map[string]any{"purpose": "tool_auth", "toolId": "codex", "name": "c", "value": value})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%q = %d %s", value, w.Code, w.Body.String())
		}
	}
}

func TestCodexDeviceRoutesUpstreamDisconnect(t *testing.T) {
	for _, failure := range []string{"request EOF", "truncated response"} {
		t.Run(failure, func(t *testing.T) {
			setSecretsKey(t)
			hn := newHarness(t)
			hn.h.Credentials = services.NewCredentialService(hn.db)
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if failure == "truncated response" {
					w.Header().Set("Content-Length", "128")
					_, _ = w.Write([]byte("{"))
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			t.Cleanup(api.Close)
			hn.h.CodexDevice = services.NewCodexDeviceFlowService(hn.h.Credentials, api.URL)
			w := hn.do(http.MethodPost, "/api/workspace/credentials/codex/device", map[string]any{"name": "ChatGPT"})
			if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), `"code":"upstream_error"`) {
				t.Fatalf("%s: upstream=%d %s", failure, w.Code, w.Body.String())
			}
		})
	}
}
