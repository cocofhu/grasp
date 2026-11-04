package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/codexoauth"
	"github.com/cocofhu/grasp/internal/runtime"
)

func testJWT(claims map[string]any) string {
	enc := base64.RawURLEncoding
	head := enc.EncodeToString([]byte(`{"alg":"none"}`))
	body, _ := json.Marshal(claims)
	return head + "." + enc.EncodeToString(body) + ".sig"
}

func testAccess(n int, exp time.Time) string {
	return testJWT(map[string]any{"exp": exp.Unix(), "n": n})
}

func testIDToken() string {
	return testJWT(map[string]any{"email": "dev@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-1"}})
}

func testCodexLogin(t *testing.T, access, refresh string) string {
	t.Helper()
	body, err := codexoauth.NewAuthFile(codexoauth.Tokens{IDToken: testIDToken(), AccessToken: access, RefreshToken: refresh}, time.Now()).Encode()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// fakeOpenAI is the OpenAI token endpoint. Each successful refresh mints
// access-N (valid for ten days) and rt-N.
type fakeOpenAI struct {
	srv      *httptest.Server
	calls    atomic.Int32
	mu       sync.Mutex
	status   int
	body     string
	noRotate bool
	delay    time.Duration
	during   func()
	seenRT   []string
}

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	f := &fakeOpenAI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		n := int(f.calls.Add(1))
		f.mu.Lock()
		status, body, noRotate, delay, during := f.status, f.body, f.noRotate, f.delay, f.during
		f.seenRT = append(f.seenRT, r.Form.Get("refresh_token"))
		f.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		if during != nil {
			during()
		}
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		out := map[string]string{"id_token": testIDToken(), "access_token": testAccess(n, time.Now().Add(240*time.Hour))}
		if !noRotate {
			out["refresh_token"] = fmt.Sprintf("rt-%d", n)
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOpenAI) set(fn func(f *fakeOpenAI)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

type memCodexFile struct {
	mu     sync.Mutex
	body   string
	writes int
}

func (f *memCodexFile) WriteFileAtomic(_ context.Context, _ string, content []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body, f.writes = string(content), f.writes+1
	return nil
}

func (f *memCodexFile) ReadFile(context.Context, string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []byte(f.body), nil
}

func (f *memCodexFile) get() (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body, f.writes
}

type codexMgrFixture struct {
	creds *CredentialService
	hub   *runtime.CodexLoginHub
	m     *CodexLoginManager
	api   *fakeOpenAI
	id    string
}

func newCodexMgrFixture(t *testing.T, access string) *codexMgrFixture {
	t.Helper()
	setCredentialKey(t)
	db := newTestDB(t)
	creds := NewCredentialService(db)
	api := newFakeOpenAI(t)
	hub := runtime.NewCodexLoginHub(nil)
	m := newCodexLoginManager(creds, hub, codexoauth.NewClient(api.srv.URL), CodexLoginManagerConfig{CheckInterval: time.Hour})
	t.Cleanup(m.Stop)
	view, err := creds.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "Codex", Value: testCodexLogin(t, access, "rt-0")})
	if err != nil {
		t.Fatal(err)
	}
	m.bg.Wait()
	return &codexMgrFixture{creds: creds, hub: hub, m: m, api: api, id: view.ID}
}

func (fx *codexMgrFixture) stored(t *testing.T) *codexoauth.AuthFile {
	t.Helper()
	login, err := fx.creds.codexLogin(fx.id)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := codexoauth.ParseAuthFile(login.Body)
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func (fx *codexMgrFixture) sandboxes(t *testing.T, n int) []*memCodexFile {
	t.Helper()
	copy0, version, err := fx.m.EnsureFresh(context.Background(), fx.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.hub.Prepare(context.Background(), fx.id, ""); err != nil {
		t.Fatal(err)
	}
	files := make([]*memCodexFile, n)
	for i := range files {
		files[i] = &memCodexFile{body: copy0}
		fx.hub.Register(fx.id, fmt.Sprintf("sb-%d", i), files[i], "/root/.codex/auth.json", version)
	}
	return files
}

func assertSandboxCopy(t *testing.T, body, wantAccess string) {
	t.Helper()
	auth, err := codexoauth.ParseAuthFile(body)
	if err != nil {
		t.Fatalf("sandbox copy: %v", err)
	}
	if auth.RefreshToken() != "" || strings.Contains(body, "rt-") {
		t.Fatal("refresh token reached a sandbox")
	}
	if wantAccess != "" && auth.AccessToken() != wantAccess {
		t.Fatalf("sandbox access token = %q", auth.AccessToken())
	}
}

func TestCodexLoginManagerScheduledRenewalRefreshesOnceAndPushesAll(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
	files := fx.sandboxes(t, 5)
	if fx.api.calls.Load() != 0 {
		t.Fatal("a fresh login must not be renewed")
	}
	// Ten days left: nothing is due.
	fx.m.Tick(context.Background())
	if fx.api.calls.Load() != 0 {
		t.Fatal("tick renewed a login that is not due")
	}
	// Forty hours left, inside the 96h lead.
	fx.m.now = func() time.Time { return time.Now().Add(200 * time.Hour) }
	fx.api.set(func(f *fakeOpenAI) { f.delay = 50 * time.Millisecond })
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(2)
		go func() { defer wg.Done(); fx.m.Tick(context.Background()) }()
		go func() {
			defer wg.Done()
			if _, _, err := fx.m.EnsureFresh(context.Background(), fx.id); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := fx.api.calls.Load(); n != 1 {
		t.Fatalf("refresh requests = %d, want 1", n)
	}
	auth := fx.stored(t)
	if auth.RefreshToken() != "rt-1" || fx.api.seenRT[0] != "rt-0" {
		t.Fatalf("stored refresh token %q, sent %v", auth.RefreshToken(), fx.api.seenRT)
	}
	for i, f := range files {
		body, writes := f.get()
		if writes != 1 {
			t.Fatalf("sandbox %d writes = %d", i, writes)
		}
		assertSandboxCopy(t, body, auth.AccessToken())
	}
	info := fx.m.Status(fx.id)
	if info == nil || info.Status != CodexLoginOK || info.Email != "dev@example.com" || info.LastRefresh == nil {
		t.Fatalf("status %+v", info)
	}
}

func TestCodexLoginManagerConcurrentRefusalsRefreshOnce(t *testing.T) {
	stale := testAccess(0, time.Now().Add(240*time.Hour))
	fx := newCodexMgrFixture(t, stale)
	files := fx.sandboxes(t, 5)
	fx.api.set(func(f *fakeOpenAI) { f.delay = 50 * time.Millisecond })
	var wg sync.WaitGroup
	retried := make([]bool, len(files))
	for i := range files {
		key := fmt.Sprintf("sb-%d", i)
		v, a := fx.hub.Snapshot(key)
		if a != stale {
			t.Fatalf("snapshot access %q", a)
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			retried[i] = fx.hub.Recover(context.Background(), key, v, a)
		}(i)
	}
	wg.Wait()
	if n := fx.api.calls.Load(); n != 1 {
		t.Fatalf("refresh requests = %d, want 1", n)
	}
	auth := fx.stored(t)
	for i, f := range files {
		body, _ := f.get()
		assertSandboxCopy(t, body, auth.AccessToken())
		if !retried[i] {
			t.Fatalf("sandbox %d was not retried", i)
		}
	}
	// A late refusal with the old token gets the stored copy, no new request.
	out, _, err := fx.m.ForceRefresh(context.Background(), fx.id, stale)
	if err != nil || fx.api.calls.Load() != 1 {
		t.Fatalf("late refusal: %v calls=%d", err, fx.api.calls.Load())
	}
	assertSandboxCopy(t, out, auth.AccessToken())
}

func TestCodexLoginManagerTerminalFailureNeedsRelogin(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
	files := fx.sandboxes(t, 2)
	fx.api.set(func(f *fakeOpenAI) {
		f.status, f.body = http.StatusBadRequest, `{"error":{"code":"refresh_token_reused","message":"Your refresh token has already been used"}}`
	})
	_, _, err := fx.m.ForceRefresh(context.Background(), fx.id, "")
	if !errors.Is(err, runtime.ErrCodexLoginRejected) || !strings.Contains(err.Error(), runtime.CodexLoginRepasteMark) {
		t.Fatalf("terminal failure must be a login rejection: %v", err)
	}
	if strings.Contains(err.Error(), "rt-0") {
		t.Fatal("error leaked the refresh token")
	}
	if st := fx.m.Status(fx.id).Status; st != CodexLoginNeedsRelogin {
		t.Fatalf("status %q", st)
	}
	// No loop: neither the scheduler nor another refusal calls OpenAI again.
	fx.m.now = func() time.Time { return time.Now().Add(200 * time.Hour) }
	fx.m.Tick(context.Background())
	if _, _, err := fx.m.ForceRefresh(context.Background(), fx.id, ""); !errors.Is(err, runtime.ErrCodexLoginRejected) {
		t.Fatal(err)
	}
	if fx.api.calls.Load() != 1 {
		t.Fatalf("calls = %d after a terminal failure", fx.api.calls.Load())
	}
	// The access token still works for a while: new sandboxes get it.
	if _, _, err := fx.m.EnsureFresh(context.Background(), fx.id); err != nil {
		t.Fatalf("unexpired login must still be served: %v", err)
	}
	fx.m.now = func() time.Time { return time.Now().Add(300 * time.Hour) }
	if _, _, err := fx.m.EnsureFresh(context.Background(), fx.id); !errors.Is(err, runtime.ErrCodexLoginRejected) {
		t.Fatalf("expired login after terminal failure: %v", err)
	}
	// A new paste resets the state and is pushed to running sandboxes.
	fx.m.now = time.Now
	fresh := testAccess(99, time.Now().Add(240*time.Hour))
	if _, err := fx.creds.Update(fx.id, CredentialInput{Value: testCodexLogin(t, fresh, "rt-new")}); err != nil {
		t.Fatal(err)
	}
	fx.m.bg.Wait()
	if st := fx.m.Status(fx.id).Status; st != CodexLoginOK {
		t.Fatalf("status after re-login %q", st)
	}
	for _, f := range files {
		body, _ := f.get()
		assertSandboxCopy(t, body, fresh)
	}
}

func TestCodexLoginManagerTransientFailureBacksOff(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
	base := time.Now().Add(200 * time.Hour)
	fx.m.now = func() time.Time { return base }
	fx.api.set(func(f *fakeOpenAI) { f.status, f.body = http.StatusBadGateway, "upstream down" })
	fx.m.Tick(context.Background())
	if st := fx.m.Status(fx.id).Status; st != CodexLoginNetworkError || fx.api.calls.Load() != 1 {
		t.Fatalf("status %q calls %d", st, fx.api.calls.Load())
	}
	fx.m.Tick(context.Background())
	if fx.api.calls.Load() != 1 {
		t.Fatal("tick ignored the backoff")
	}
	// Still valid: a new sandbox gets the current login without waiting.
	if _, _, err := fx.m.EnsureFresh(context.Background(), fx.id); err != nil || fx.api.calls.Load() != 1 {
		t.Fatalf("ensure during backoff: %v calls=%d", err, fx.api.calls.Load())
	}
	fx.api.set(func(f *fakeOpenAI) { f.status = 0 })
	fx.m.now = func() time.Time { return base.Add(2 * time.Minute) }
	fx.m.Tick(context.Background())
	if st := fx.m.Status(fx.id).Status; st != CodexLoginOK || fx.api.calls.Load() != 2 {
		t.Fatalf("after backoff: status %q calls %d", st, fx.api.calls.Load())
	}
	// An expired login whose renewal fails reports the failure.
	fx.api.set(func(f *fakeOpenAI) { f.status = http.StatusServiceUnavailable })
	fx.m.now = func() time.Time { return base.Add(1000 * time.Hour) }
	if _, _, err := fx.m.EnsureFresh(context.Background(), fx.id); err == nil || errors.Is(err, runtime.ErrCodexLoginRejected) {
		t.Fatalf("expired login with a transient failure: %v", err)
	}
}

func TestCodexLoginManagerKeepsRefreshTokenWhenNotRotated(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(time.Hour)))
	fx.api.set(func(f *fakeOpenAI) { f.noRotate = true })
	if _, _, err := fx.m.EnsureFresh(context.Background(), fx.id); err != nil {
		t.Fatal(err)
	}
	if rt := fx.stored(t).RefreshToken(); rt != "rt-0" {
		t.Fatalf("refresh token %q", rt)
	}
}

func TestCodexLoginManagerUserReloginWinsOverRenewal(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(time.Hour)))
	user := testAccess(42, time.Now().Add(240*time.Hour))
	fx.api.set(func(f *fakeOpenAI) {
		f.during = func() {
			if _, err := fx.creds.Update(fx.id, CredentialInput{Value: testCodexLogin(t, user, "rt-user")}); err != nil {
				t.Error(err)
			}
		}
	})
	out, _, err := fx.m.ForceRefresh(context.Background(), fx.id, "")
	if err != nil {
		t.Fatal(err)
	}
	assertSandboxCopy(t, out, user)
	if auth := fx.stored(t); auth.RefreshToken() != "rt-user" || auth.AccessToken() != user {
		t.Fatal("renewal overwrote the user's new login")
	}
}

func TestCodexLoginManagerGoneCredential(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
	files := fx.sandboxes(t, 1)
	if err := fx.creds.Revoke(fx.id); err != nil {
		t.Fatal(err)
	}
	if err := fx.hub.BeforeTurn(context.Background(), "sb-0"); !errors.Is(err, runtime.ErrCodexLoginRejected) {
		t.Fatalf("revoked login must not be re-installed: %v", err)
	}
	if _, _, err := fx.m.ForceRefresh(context.Background(), fx.id, ""); !errors.Is(err, runtime.ErrCodexLoginRejected) {
		t.Fatal(err)
	}
	if fx.m.Status(fx.id) != nil {
		t.Fatal("revoked login has no status")
	}
	if _, writes := files[0].get(); writes != 0 || fx.api.calls.Load() != 0 {
		t.Fatal("revoked login was renewed or pushed")
	}
}

func TestCodexLoginManagerListShowsStatusWithoutTokens(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
	list, err := fx.creds.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].CodexLogin == nil || list[0].CodexLogin.Email != "dev@example.com" || list[0].CodexLogin.Status != CodexLoginOK {
		t.Fatalf("list %+v", list)
	}
	blob, _ := json.Marshal(list)
	if strings.Contains(string(blob), "rt-0") || strings.Contains(string(blob), "access_token") {
		t.Fatalf("list leaked a token: %s", blob)
	}
}

func TestCodexLoginManagerLoopAndDefaults(t *testing.T) {
	setCredentialKey(t)
	creds := NewCredentialService(newTestDB(t))
	hub := runtime.NewCodexLoginHub(nil)
	m := NewCodexLoginManager(creds, hub, CodexLoginManagerConfig{})
	if m.lead != defaultCodexRefreshLead || m.interval != defaultCodexCheckInterval {
		t.Fatalf("defaults lead=%v interval=%v", m.lead, m.interval)
	}
	m.Start(context.Background())
	m.Stop()

	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(time.Hour)))
	fx.m.interval = 10 * time.Millisecond
	fx.m.Start(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for fx.api.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	fx.m.Stop()
	if fx.api.calls.Load() != 1 {
		t.Fatalf("loop renewals = %d", fx.api.calls.Load())
	}
}

func TestCodexLoginManagerUnsavedRenewalIsRetried(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(time.Hour)))
	prev, err := fx.creds.codexLogin(fx.id)
	if err != nil {
		t.Fatal(err)
	}
	renewed := testCodexLogin(t, testAccess(7, time.Now().Add(240*time.Hour)), "rt-7")
	fx.m.mu.Lock()
	fx.m.unsaved[fx.id] = codexUnsaved{prev: prev, content: renewed}
	fx.m.mu.Unlock()
	// The unsaved login is what gets used until it is stored.
	if _, auth, err := fx.m.load(fx.id); err != nil || auth.RefreshToken() != "rt-7" {
		t.Fatalf("load must prefer the unsaved renewal: %v", err)
	}
	fx.m.retryUnsaved()
	if rt := fx.stored(t).RefreshToken(); rt != "rt-7" {
		t.Fatalf("stored %q", rt)
	}
	fx.m.mu.Lock()
	left := len(fx.m.unsaved)
	fx.m.mu.Unlock()
	if left != 0 {
		t.Fatal("saved renewal kept pending")
	}
}

func TestCodexErrSummary(t *testing.T) {
	if got := codexErrSummary(&codexoauth.Error{Status: 502}); got != "http 502" {
		t.Fatal(got)
	}
	if got := codexErrSummary(&codexoauth.Error{Code: "invalid_grant"}); got != "invalid_grant" {
		t.Fatal(got)
	}
	if got := codexErrSummary(errors.New("x")); got != "x" {
		t.Fatal(got)
	}
}
