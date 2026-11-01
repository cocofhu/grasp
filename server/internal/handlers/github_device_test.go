package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/services"
)

func newDeviceGitHub(t *testing.T, authorized *atomic.Bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc-secret", "user_code": "ABCD-EFGH", "verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 0})
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		if !authorized.Load() {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "gho_handler_token", "scope": "repo,read:org,workflow"})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, read:org, workflow")
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "octocat"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGitHubDeviceRoutesDisabled(t *testing.T) {
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	w := hn.do(http.MethodGet, "/api/workspace/credentials/github/device", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
	hn.h.GitHubDevice = services.NewGitHubDeviceFlowService(hn.h.Credentials, func() string { return "" })
	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/api/workspace/credentials/github/device"},
		{http.MethodPost, "/api/workspace/credentials/github/device/ghd-x/poll"},
		{http.MethodDelete, "/api/workspace/credentials/github/device/ghd-x"},
	} {
		if w := hn.do(req.method, req.path, map[string]any{"name": "x"}); w.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d", req.method, req.path, w.Code)
		}
	}
}

func TestGitHubDeviceRoutesSignInAndAudit(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	var authorized atomic.Bool
	gh := newDeviceGitHub(t, &authorized)
	svc := services.NewGitHubDeviceFlowService(hn.h.Credentials, func() string { return "cid" })
	svc.SetBaseURLs(gh.URL, gh.URL)
	hn.h.GitHubDevice = svc

	if w := hn.do(http.MethodGet, "/api/workspace/credentials/github/device", nil); !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatalf("status %s", w.Body.String())
	}
	if w := hn.do(http.MethodPost, "/api/workspace/credentials/github/device", map[string]any{"name": ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("blank name=%d %s", w.Code, w.Body.String())
	}
	if w := hn.do(http.MethodPost, "/api/workspace/credentials/github/device", map[string]any{"credentialId": "cred-missing"}); w.Code != http.StatusNotFound {
		t.Fatalf("missing target=%d %s", w.Code, w.Body.String())
	}

	w := hn.do(http.MethodPost, "/api/workspace/credentials/github/device", map[string]any{"name": "Device"})
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "dc-secret") {
		t.Fatalf("start=%d %s", w.Code, w.Body.String())
	}
	var start services.GitHubDeviceStart
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	if start.UserCode != "ABCD-EFGH" || start.SessionID == "" {
		t.Fatalf("start=%+v", start)
	}
	// Poll before the interval elapses: pending without saving.
	w = hn.do(http.MethodPost, "/api/workspace/credentials/github/device/"+start.SessionID+"/poll", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"pending"`) {
		t.Fatalf("poll=%d %s", w.Code, w.Body.String())
	}

	if conflict := hn.do(http.MethodPost, "/api/workspace/credentials/github/device", map[string]any{"name": "device"}); conflict.Code != http.StatusOK {
		t.Fatalf("pending sessions must not reserve an alias: %d", conflict.Code)
	}

	// A session started by someone else is invisible.
	foreign, err := svc.Start(context.Background(), "bob", "Bob", "")
	if err != nil {
		t.Fatal(err)
	}
	if w := hn.do(http.MethodPost, "/api/workspace/credentials/github/device/"+foreign.SessionID+"/poll", nil); w.Code != http.StatusNotFound {
		t.Fatalf("foreign poll=%d", w.Code)
	}
	if w := hn.do(http.MethodDelete, "/api/workspace/credentials/github/device/"+start.SessionID, nil); w.Code != http.StatusOK {
		t.Fatalf("cancel=%d", w.Code)
	}
	if w := hn.do(http.MethodPost, "/api/workspace/credentials/github/device/"+start.SessionID+"/poll", nil); w.Code != http.StatusNotFound {
		t.Fatalf("cancelled poll=%d", w.Code)
	}
}

func TestGitHubDeviceRoutesSavesCredentialWithoutLeakingToken(t *testing.T) {
	setSecretsKey(t)
	hn := newHarness(t)
	hn.h.Credentials = services.NewCredentialService(hn.db)
	var authorized atomic.Bool
	authorized.Store(true)
	gh := newDeviceGitHub(t, &authorized)
	svc := services.NewGitHubDeviceFlowService(hn.h.Credentials, func() string { return "cid" })
	svc.SetBaseURLs(gh.URL, gh.URL)
	now := time.Now()
	svc.SetClock(func() time.Time { return now })
	hn.h.GitHubDevice = svc

	w := hn.do(http.MethodPost, "/api/workspace/credentials/github/device", map[string]any{"name": "Device"})
	var start services.GitHubDeviceStart
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	if start.Interval != 5 {
		t.Fatalf("interval below the 5s minimum: %+v", start)
	}
	now = now.Add(5 * time.Second)

	w = hn.do(http.MethodPost, "/api/workspace/credentials/github/device/"+start.SessionID+"/poll", nil)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `"status":"authorized"`) || !strings.Contains(body, `"githubLogin":"octocat"`) {
		t.Fatalf("poll=%d %s", w.Code, body)
	}
	if strings.Contains(body, "gho_handler_token") || strings.Contains(body, "dc-secret") {
		t.Fatal("secret leaked in poll response")
	}
	list := hn.do(http.MethodGet, "/api/workspace/credentials", nil).Body.String()
	if !strings.Contains(list, `"githubLogin":"octocat"`) || strings.Contains(list, "gho_handler_token") {
		t.Fatalf("list=%s", list)
	}
	audit := hn.do(http.MethodGet, "/api/workspace/audit?time=all", nil).Body.String()
	if !strings.Contains(audit, "create credential (github device flow)") || !strings.Contains(audit, "octocat") {
		t.Fatalf("audit=%s", audit)
	}
	if strings.Contains(audit, "gho_handler_token") || strings.Contains(audit, "dc-secret") {
		t.Fatal("secret leaked in audit")
	}
}
