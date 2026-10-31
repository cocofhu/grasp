package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHubOAuth serves the device-flow endpoints. tokenReplies is consumed one
// per access_token request; the last entry repeats.
type fakeGitHubOAuth struct {
	mu           sync.Mutex
	deviceReply  map[string]any
	tokenReplies []map[string]any
	tokenCalls   int
	deviceCalls  int
	scopesHeader string
	noScopes     bool
	login        string
	forms        []map[string]string
	server       *httptest.Server
}

func newFakeGitHubOAuth(t *testing.T) *fakeGitHubOAuth {
	t.Helper()
	f := &fakeGitHubOAuth{
		deviceReply:  map[string]any{"device_code": "secret-device-code", "user_code": "WDJB-MJHT", "verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 5},
		tokenReplies: []map[string]any{{"error": "authorization_pending"}},
		scopesHeader: "repo, read:org, workflow",
		login:        "octocat",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.deviceCalls++
		f.forms = append(f.forms, map[string]string{"client_id": r.PostForm.Get("client_id"), "scope": r.PostForm.Get("scope")})
		reply := f.deviceReply
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(reply)
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.forms = append(f.forms, map[string]string{"device_code": r.PostForm.Get("device_code"), "grant_type": r.PostForm.Get("grant_type")})
		i := f.tokenCalls
		if i >= len(f.tokenReplies) {
			i = len(f.tokenReplies) - 1
		}
		f.tokenCalls++
		reply := f.tokenReplies[i]
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(reply)
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gho_synthetic_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		if !f.noScopes {
			w.Header().Set("X-OAuth-Scopes", f.scopesHeader)
		}
		login := f.login
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"login": login})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHubOAuth) setToken(replies ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenReplies, f.tokenCalls = replies, 0
}

func (f *fakeGitHubOAuth) calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deviceCalls, f.tokenCalls
}

type deviceClock struct{ t time.Time }

func (c *deviceClock) now() time.Time      { return c.t }
func (c *deviceClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newDeviceFlowFixture(t *testing.T, clientID string) (*GitHubDeviceFlowService, *CredentialService, *fakeGitHubOAuth, *deviceClock) {
	t.Helper()
	setCredentialKey(t)
	creds := NewCredentialService(newTestDB(t))
	gh := newFakeGitHubOAuth(t)
	svc := NewGitHubDeviceFlowService(creds, func() string { return clientID })
	svc.SetBaseURLs(gh.server.URL, gh.server.URL)
	clock := &deviceClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	svc.now = clock.now
	return svc, creds, gh, clock
}

var successToken = map[string]any{"access_token": "gho_synthetic_token", "token_type": "bearer", "scope": "repo,read:org,workflow"}

func TestGitHubDeviceFlowDisabledDoesNotCallGitHub(t *testing.T) {
	svc, _, gh, _ := newDeviceFlowFixture(t, "  ")
	if svc.Enabled() {
		t.Fatal("blank client id must disable the flow")
	}
	if _, err := svc.Start(context.Background(), "alice", "mine", ""); !errors.Is(err, ErrGitHubDeviceFlowDisabled) {
		t.Fatalf("start err=%v", err)
	}
	if d, tk := gh.calls(); d != 0 || tk != 0 {
		t.Fatalf("GitHub was called: device=%d token=%d", d, tk)
	}
}

func TestGitHubDeviceFlowCreatesCredential(t *testing.T) {
	svc, creds, gh, clock := newDeviceFlowFixture(t, "Iv1.client")
	ctx := context.Background()
	start, err := svc.Start(ctx, "alice", " My GitHub ", "")
	if err != nil {
		t.Fatal(err)
	}
	if start.UserCode != "WDJB-MJHT" || start.Interval != 5 || start.ExpiresIn != 900 || start.VerificationURI != "https://github.com/login/device" {
		t.Fatalf("start=%+v", start)
	}
	raw, _ := json.Marshal(start)
	if strings.Contains(string(raw), "secret-device-code") {
		t.Fatal("device_code leaked to the browser")
	}
	if gh.forms[0]["client_id"] != "Iv1.client" || gh.forms[0]["scope"] != GitHubDeviceScopes {
		t.Fatalf("device request=%v", gh.forms[0])
	}

	// Before the interval elapses GitHub is not contacted.
	poll, err := svc.Poll(ctx, "alice", start.SessionID)
	if err != nil || poll.Status != GitHubDevicePending {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
	if _, tk := gh.calls(); tk != 0 {
		t.Fatalf("token polled before interval: %d", tk)
	}
	clock.add(5 * time.Second)
	if poll, _ = svc.Poll(ctx, "alice", start.SessionID); poll.Status != GitHubDevicePending {
		t.Fatalf("poll=%+v", poll)
	}
	if _, tk := gh.calls(); tk != 1 {
		t.Fatalf("token calls=%d", tk)
	}
	if gh.forms[1]["device_code"] != "secret-device-code" || gh.forms[1]["grant_type"] != githubDeviceGrantType {
		t.Fatalf("token request=%v", gh.forms[1])
	}

	gh.setToken(successToken)
	clock.add(5 * time.Second)
	poll, err = svc.Poll(ctx, "alice", start.SessionID)
	if err != nil || poll.Status != GitHubDeviceAuthorized || poll.Credential == nil {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
	if poll.GitHubLogin != "octocat" || poll.ScopeWarning || strings.Join(poll.Scopes, " ") != "repo read:org workflow" || poll.Updated {
		t.Fatalf("poll=%+v", poll)
	}
	raw, _ = json.Marshal(poll)
	if strings.Contains(string(raw), "gho_synthetic_token") {
		t.Fatal("token leaked in poll response")
	}
	v := poll.Credential
	if v.Name != "My GitHub" || v.Purpose != "git" || v.Provider != "github" || v.Host != GitHubDeviceHost || v.EnvKey != "GITHUB_TOKEN" || v.Metadata["authMethod"] != "device_flow" || v.Metadata["githubLogin"] != "octocat" {
		t.Fatalf("credential=%+v", v)
	}
	resolved, ok := creds.ResolveGitValue(v.ID)
	if !ok || resolved.Secret != "gho_synthetic_token" || resolved.Host != GitHubDeviceHost || resolved.EnvKey != "GITHUB_TOKEN" {
		t.Fatalf("resolved=%+v ok=%v", resolved, ok)
	}
	listed, _ := creds.Get(v.ID)
	if listed.Metadata["githubLogin"] != "octocat" {
		t.Fatalf("metadata not persisted: %+v", listed.Metadata)
	}
	// The session is single-use.
	if _, err := svc.Poll(ctx, "alice", start.SessionID); !errors.Is(err, ErrGitHubDeviceSessionNotFound) {
		t.Fatalf("finished session err=%v", err)
	}
}

func TestGitHubDeviceFlowSlowDownWidensInterval(t *testing.T) {
	svc, _, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	start, err := svc.Start(ctx, "alice", "a", "")
	if err != nil {
		t.Fatal(err)
	}
	gh.setToken(map[string]any{"error": "slow_down", "interval": 10}, map[string]any{"error": "authorization_pending"})
	clock.add(5 * time.Second)
	poll, _ := svc.Poll(ctx, "alice", start.SessionID)
	if poll.Status != GitHubDevicePending || poll.Interval != 10 {
		t.Fatalf("poll=%+v", poll)
	}
	clock.add(9 * time.Second)
	_, _ = svc.Poll(ctx, "alice", start.SessionID)
	if _, tk := gh.calls(); tk != 1 {
		t.Fatalf("polled GitHub before the widened interval: %d", tk)
	}
	clock.add(time.Second)
	_, _ = svc.Poll(ctx, "alice", start.SessionID)
	if _, tk := gh.calls(); tk != 2 {
		t.Fatalf("token calls=%d", tk)
	}
}

func TestGitHubDeviceFlowTerminalErrors(t *testing.T) {
	for _, tc := range []struct {
		reply  map[string]any
		status string
		code   string
	}{
		{map[string]any{"error": "access_denied"}, GitHubDeviceDenied, ""},
		{map[string]any{"error": "expired_token"}, GitHubDeviceExpired, ""},
		{map[string]any{"error": "device_flow_disabled"}, GitHubDeviceError, "device_flow_disabled"},
		{map[string]any{"error": "incorrect_client_credentials"}, GitHubDeviceError, "incorrect_client_credentials"},
	} {
		svc, creds, gh, clock := newDeviceFlowFixture(t, "cid")
		ctx := context.Background()
		start, err := svc.Start(ctx, "alice", "a", "")
		if err != nil {
			t.Fatal(err)
		}
		gh.setToken(tc.reply)
		clock.add(5 * time.Second)
		poll, err := svc.Poll(ctx, "alice", start.SessionID)
		if err != nil || poll.Status != tc.status || poll.Error != tc.code {
			t.Fatalf("%v: poll=%+v err=%v", tc.reply, poll, err)
		}
		if rows, _ := creds.List(); len(rows) != 0 {
			t.Fatalf("%v created a credential", tc.reply)
		}
		if _, err := svc.Poll(ctx, "alice", start.SessionID); !errors.Is(err, ErrGitHubDeviceSessionNotFound) {
			t.Fatalf("%v kept the session", tc.reply)
		}
	}
}

func TestGitHubDeviceFlowStartErrors(t *testing.T) {
	svc, creds, gh, _ := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	if _, err := svc.Start(ctx, "alice", " ", ""); !errors.Is(err, ErrCredentialName) {
		t.Fatalf("blank name err=%v", err)
	}
	if _, err := creds.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "Team", Value: "ghp_existing"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Start(ctx, "alice", "team", "")
	var taken *CredentialAliasTakenError
	if !errors.As(err, &taken) || taken.Alias != "Team" {
		t.Fatalf("alias err=%v", err)
	}
	if d, _ := gh.calls(); d != 0 {
		t.Fatal("GitHub was called before local validation")
	}
	gh.mu.Lock()
	gh.deviceReply = map[string]any{"error": "device_flow_disabled", "error_description": "Device Flow must be explicitly enabled"}
	gh.mu.Unlock()
	_, err = svc.Start(ctx, "alice", "new", "")
	var apiErr *GitHubAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != "device_flow_disabled" {
		t.Fatalf("device_flow_disabled err=%v", err)
	}
}

func TestGitHubDeviceFlowSessionsAreBoundToOwner(t *testing.T) {
	svc, _, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	start, err := svc.Start(ctx, "alice", "a", "")
	if err != nil {
		t.Fatal(err)
	}
	clock.add(5 * time.Second)
	if _, err := svc.Poll(ctx, "mallory", start.SessionID); !errors.Is(err, ErrGitHubDeviceSessionNotFound) {
		t.Fatalf("foreign poll err=%v", err)
	}
	svc.Cancel("mallory", start.SessionID)
	if poll, err := svc.Poll(ctx, "alice", start.SessionID); err != nil || poll.Status != GitHubDevicePending {
		t.Fatalf("foreign cancel removed the session: %+v %v", poll, err)
	}
	svc.Cancel("alice", start.SessionID)
	svc.Cancel("alice", start.SessionID)
	if _, err := svc.Poll(ctx, "alice", start.SessionID); !errors.Is(err, ErrGitHubDeviceSessionNotFound) {
		t.Fatalf("cancelled session err=%v", err)
	}
	if _, tk := gh.calls(); tk != 1 {
		t.Fatalf("token calls=%d", tk)
	}
}

func TestGitHubDeviceFlowExpiresWithoutGitHub(t *testing.T) {
	svc, _, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	start, _ := svc.Start(ctx, "alice", "a", "")
	clock.add(901 * time.Second)
	poll, err := svc.Poll(ctx, "alice", start.SessionID)
	if err != nil || poll.Status != GitHubDeviceExpired {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
	if _, tk := gh.calls(); tk != 0 {
		t.Fatal("expired session contacted GitHub")
	}
}

func TestGitHubDeviceFlowReloginRotatesExistingCredential(t *testing.T) {
	svc, creds, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	old, err := creds.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "Team", Value: "ghp_old"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := svc.Start(ctx, "alice", "ignored", old.ID)
	if err != nil {
		t.Fatal(err)
	}
	gh.setToken(successToken)
	gh.mu.Lock()
	gh.scopesHeader = "read:org"
	gh.mu.Unlock()
	clock.add(5 * time.Second)
	poll, err := svc.Poll(ctx, "alice", start.SessionID)
	if err != nil || poll.Status != GitHubDeviceAuthorized || !poll.Updated || !poll.ScopeWarning {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
	if poll.Credential.ID != old.ID || poll.Credential.Name != "Team" || poll.Credential.SecretRevision != old.SecretRevision+1 {
		t.Fatalf("credential=%+v", poll.Credential)
	}
	resolved, ok := creds.ResolveGitValue(old.ID)
	if !ok || resolved.Secret != "gho_synthetic_token" || resolved.Host != GitHubDeviceHost {
		t.Fatalf("resolved=%+v", resolved)
	}
	if rows, _ := creds.List(); len(rows) != 1 {
		t.Fatalf("relogin created a credential: %d", len(rows))
	}
	// A manual token rotation drops the device-flow account metadata.
	manual, err := creds.Update(old.ID, CredentialInput{Value: "ghp_manual"})
	if err != nil || manual.Metadata != nil {
		t.Fatalf("manual rotation kept metadata: %+v %v", manual.Metadata, err)
	}
}

func TestGitHubDeviceFlowReloginTargetRules(t *testing.T) {
	svc, creds, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	ghe, _ := creds.Create(CredentialInput{Purpose: "git", Provider: "github", Host: "github.example.com", Name: "GHE", Value: "ghp_x"})
	if _, err := svc.Start(ctx, "alice", "", ghe.ID); !errors.Is(err, ErrGitHubDeviceTarget) {
		t.Fatalf("GHE target err=%v", err)
	}
	gl, _ := creds.Create(CredentialInput{Purpose: "git", Provider: "gitlab", Name: "GL", Value: "glpat"})
	if _, err := svc.Start(ctx, "alice", "", gl.ID); !errors.Is(err, ErrGitHubDeviceTarget) {
		t.Fatalf("GitLab target err=%v", err)
	}
	if _, err := svc.Start(ctx, "alice", "", "cred-missing"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("missing target err=%v", err)
	}

	// The target is revoked while the user is authorizing.
	target, _ := creds.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "Team", Value: "ghp_old"})
	start, err := svc.Start(ctx, "alice", "", target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.Revoke(target.ID); err != nil {
		t.Fatal(err)
	}
	gh.setToken(successToken)
	clock.add(5 * time.Second)
	poll, _ := svc.Poll(ctx, "alice", start.SessionID)
	if poll.Status != GitHubDeviceError || poll.Error != "credential_unavailable" {
		t.Fatalf("poll=%+v", poll)
	}
	if got, _ := creds.Get(target.ID); got.RevokedAt == nil {
		t.Fatal("revoked credential was revived")
	}
	if rows, _ := creds.List(); len(rows) != 3 {
		t.Fatalf("a new credential was created: %d", len(rows))
	}
}

func TestGitHubDeviceFlowAliasTakenWhileWaiting(t *testing.T) {
	svc, creds, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	start, err := svc.Start(ctx, "alice", "Mine", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creds.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "mine", Value: "ghp_other"}); err != nil {
		t.Fatal(err)
	}
	gh.setToken(successToken)
	clock.add(5 * time.Second)
	poll, _ := svc.Poll(ctx, "alice", start.SessionID)
	if poll.Status != GitHubDeviceError || poll.Error != "alias_taken" || poll.Alias != "mine" {
		t.Fatalf("poll=%+v", poll)
	}
}

func TestGitHubDeviceFlowFallsBackToTokenScopes(t *testing.T) {
	svc, _, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	start, _ := svc.Start(ctx, "alice", "a", "")
	gh.setToken(map[string]any{"access_token": "gho_synthetic_token", "scope": "read:org,workflow"})
	gh.mu.Lock()
	gh.noScopes = true
	gh.mu.Unlock()
	clock.add(5 * time.Second)
	poll, _ := svc.Poll(ctx, "alice", start.SessionID)
	if poll.Status != GitHubDeviceAuthorized || !poll.ScopeWarning || strings.Join(poll.Scopes, ",") != "read:org,workflow" {
		t.Fatalf("poll=%+v", poll)
	}
}

func TestGitHubDeviceFlowNetworkErrorStaysPending(t *testing.T) {
	svc, _, gh, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	start, _ := svc.Start(ctx, "alice", "a", "")
	gh.server.Close()
	clock.add(5 * time.Second)
	poll, err := svc.Poll(ctx, "alice", start.SessionID)
	if err != nil || poll.Status != GitHubDevicePending {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
}

func TestGitHubDeviceFlowPrunesOwnerSessions(t *testing.T) {
	svc, _, _, clock := newDeviceFlowFixture(t, "cid")
	ctx := context.Background()
	first, _ := svc.Start(ctx, "alice", "a", "")
	for i := 0; i < githubDeviceMaxPerOwner; i++ {
		clock.add(time.Second)
		if _, err := svc.Start(ctx, "alice", "a", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Poll(ctx, "alice", first.SessionID); !errors.Is(err, ErrGitHubDeviceSessionNotFound) {
		t.Fatalf("oldest session kept: %v", err)
	}
	svc.mu.Lock()
	n := len(svc.sessions)
	svc.mu.Unlock()
	if n != githubDeviceMaxPerOwner {
		t.Fatalf("sessions=%d", n)
	}
}
