package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/codexoauth"
)

type fakeDeviceClient struct {
	mu        sync.Mutex
	startErr  error
	verify    string
	polls     int
	results   []deviceResult
	lastPoll  codexoauth.DeviceStart
	pollBlock chan struct{}
}

type deviceResult struct {
	tokens     codexoauth.Tokens
	authorized bool
	err        error
}

func (f *fakeDeviceClient) StartDevice(context.Context) (codexoauth.DeviceStart, error) {
	if f.startErr != nil {
		return codexoauth.DeviceStart{}, f.startErr
	}
	return codexoauth.DeviceStart{DeviceAuthID: "dev-secret", UserCode: "ABCD-1234", VerificationURL: f.verify, Interval: time.Second, ExpiresIn: 15 * time.Minute}, nil
}

func (f *fakeDeviceClient) PollDevice(_ context.Context, d codexoauth.DeviceStart) (codexoauth.Tokens, bool, error) {
	if f.pollBlock != nil {
		<-f.pollBlock
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPoll = d
	f.polls++
	if len(f.results) == 0 {
		return codexoauth.Tokens{}, false, nil
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r.tokens, r.authorized, r.err
}

type deviceFixture struct {
	svc    *CodexDeviceFlowService
	client *fakeDeviceClient
	creds  *CredentialService
	clock  time.Time
}

func newDeviceFixture(t *testing.T) *deviceFixture {
	t.Helper()
	setCredentialKey(t)
	creds := NewCredentialService(newTestDB(t))
	client := &fakeDeviceClient{}
	fx := &deviceFixture{client: client, creds: creds, clock: time.Now()}
	fx.svc = newCodexDeviceFlowService(creds, client, "https://auth.example/codex/device")
	fx.svc.now = func() time.Time { return fx.clock }
	return fx
}

func (fx *deviceFixture) advance(d time.Duration) { fx.clock = fx.clock.Add(d) }

func deviceTokens(t *testing.T) codexoauth.Tokens {
	return codexoauth.Tokens{IDToken: testIDToken(), AccessToken: testAccess(1, time.Now().Add(240*time.Hour)), RefreshToken: "rt-device"}
}

func TestCodexDeviceFlowCreatesCredential(t *testing.T) {
	fx := newDeviceFixture(t)
	start, err := fx.svc.Start(context.Background(), "alice", "ChatGPT", "")
	if err != nil {
		t.Fatal(err)
	}
	if start.UserCode != "ABCD-1234" || start.VerificationURI != "https://auth.example/codex/device" || start.Interval != 3 || start.ExpiresIn != 900 {
		t.Fatalf("start %+v", start)
	}
	// Too early: pending without contacting OpenAI.
	if out, err := fx.svc.Poll(context.Background(), "alice", start.SessionID); err != nil || out.Status != CodexDevicePending || fx.client.polls != 0 {
		t.Fatalf("early poll %+v %v", out, err)
	}
	// Another user cannot poll or cancel the session.
	if _, err := fx.svc.Poll(context.Background(), "mallory", start.SessionID); !errors.Is(err, ErrCodexDeviceSessionNotFound) {
		t.Fatalf("foreign poll: %v", err)
	}
	fx.svc.Cancel("mallory", start.SessionID)
	fx.advance(3 * time.Second)
	if out, _ := fx.svc.Poll(context.Background(), "alice", start.SessionID); out.Status != CodexDevicePending || fx.client.polls != 1 {
		t.Fatalf("pending poll %+v", out)
	}
	if fx.client.lastPoll.DeviceAuthID != "dev-secret" {
		t.Fatal("device auth id not used")
	}
	fx.client.results = []deviceResult{{tokens: deviceTokens(t), authorized: true}}
	fx.advance(3 * time.Second)
	out, err := fx.svc.Poll(context.Background(), "alice", start.SessionID)
	if err != nil || out.Status != CodexDeviceSuccess || out.Credential == nil || out.Email != "dev@example.com" || out.Updated {
		t.Fatalf("success %+v %v", out, err)
	}
	login, err := fx.creds.codexLogin(out.Credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := codexoauth.ParseAuthFile(login.Body)
	if err != nil || auth.RefreshToken() != "rt-device" || auth.AuthMode() != "chatgpt" {
		t.Fatalf("stored login: %v", err)
	}
	if _, err := fx.svc.Poll(context.Background(), "alice", start.SessionID); !errors.Is(err, ErrCodexDeviceSessionNotFound) {
		t.Fatal("finished session must be gone")
	}
	// Same alias again is refused before asking OpenAI.
	if _, err := fx.svc.Start(context.Background(), "alice", "chatgpt", ""); !errors.Is(err, ErrCredentialAliasTaken) {
		t.Fatalf("alias: %v", err)
	}
	if _, err := fx.svc.Start(context.Background(), "alice", " ", ""); !errors.Is(err, ErrCredentialName) {
		t.Fatalf("name: %v", err)
	}
}

func TestCodexDeviceFlowReloginUpdatesCredential(t *testing.T) {
	fx := newDeviceFixture(t)
	old, err := fx.creds.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "Mine", Value: testCodexLogin(t, "old", "rt-old")})
	if err != nil {
		t.Fatal(err)
	}
	gh, err := fx.creds.Create(CredentialInput{Purpose: "custom", Type: "custom", Name: "Other", EnvKey: "OTHER_TOKEN", Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.Start(context.Background(), "alice", "", gh.ID); !errors.Is(err, ErrCodexDeviceTarget) {
		t.Fatalf("non-codex target: %v", err)
	}
	if _, err := fx.svc.Start(context.Background(), "alice", "", "missing"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	start, err := fx.svc.Start(context.Background(), "alice", "ignored", old.ID)
	if err != nil {
		t.Fatal(err)
	}
	fx.client.results = []deviceResult{{tokens: deviceTokens(t), authorized: true}}
	fx.advance(3 * time.Second)
	out, err := fx.svc.Poll(context.Background(), "alice", start.SessionID)
	if err != nil || out.Status != CodexDeviceSuccess || !out.Updated || out.Credential.ID != old.ID || out.Credential.Name != "Mine" {
		t.Fatalf("relogin %+v %v", out, err)
	}
	login, _ := fx.creds.codexLogin(old.ID)
	if auth, _ := codexoauth.ParseAuthFile(login.Body); auth.RefreshToken() != "rt-device" {
		t.Fatal("relogin did not replace the login")
	}
	// The target removed meanwhile: credential_unavailable.
	start, _ = fx.svc.Start(context.Background(), "alice", "", old.ID)
	if err := fx.creds.Remove(old.ID); err != nil {
		t.Fatal(err)
	}
	fx.client.results = []deviceResult{{tokens: deviceTokens(t), authorized: true}}
	fx.advance(3 * time.Second)
	if out, _ := fx.svc.Poll(context.Background(), "alice", start.SessionID); out.Status != CodexDeviceError || out.Error != "credential_unavailable" {
		t.Fatalf("removed target %+v", out)
	}
}

func TestCodexDeviceFlowOutcomes(t *testing.T) {
	fx := newDeviceFixture(t)
	fx.client.startErr = codexoauth.ErrDeviceNotEnabled
	if _, err := fx.svc.Start(context.Background(), "alice", "A", ""); !errors.Is(err, ErrCodexDeviceNotEnabled) {
		t.Fatalf("not enabled: %v", err)
	}
	fx.client.startErr = errors.New("dial tcp: refused")
	if _, err := fx.svc.Start(context.Background(), "alice", "A", ""); err == nil {
		t.Fatal("network error must surface")
	}
	fx.client.startErr = nil

	cases := []struct {
		name   string
		result deviceResult
		status string
		code   string
	}{
		{"denied", deviceResult{err: &codexoauth.Error{Status: 400, Code: "access_denied"}}, CodexDeviceDenied, ""},
		{"expired", deviceResult{err: &codexoauth.Error{Status: 400, Code: "expired_token"}}, CodexDeviceExpired, ""},
		{"exchange", deviceResult{err: &codexoauth.Error{Status: 500}}, CodexDeviceError, "exchange_failed"},
		{"coded", deviceResult{err: &codexoauth.Error{Status: 400, Code: "invalid_request"}}, CodexDeviceError, "invalid_request"},
		{"no refresh", deviceResult{tokens: codexoauth.Tokens{AccessToken: "a"}, authorized: true}, CodexDeviceError, "invalid_response"},
	}
	for _, tc := range cases {
		start, err := fx.svc.Start(context.Background(), "alice", "A "+tc.name, "")
		if err != nil {
			t.Fatal(err)
		}
		fx.client.results = []deviceResult{tc.result}
		fx.advance(3 * time.Second)
		out, err := fx.svc.Poll(context.Background(), "alice", start.SessionID)
		if err != nil || out.Status != tc.status || out.Error != tc.code {
			t.Fatalf("%s: %+v %v", tc.name, out, err)
		}
	}
	// A network failure keeps the session pending.
	start, _ := fx.svc.Start(context.Background(), "alice", "net", "")
	fx.client.results = []deviceResult{{err: errors.New("timeout")}}
	fx.advance(3 * time.Second)
	if out, _ := fx.svc.Poll(context.Background(), "alice", start.SessionID); out.Status != CodexDevicePending {
		t.Fatalf("network failure %+v", out)
	}
	// Expiry.
	fx.advance(16 * time.Minute)
	if out, _ := fx.svc.Poll(context.Background(), "alice", start.SessionID); out.Status != CodexDeviceExpired {
		t.Fatalf("expired %+v", out)
	}
	// Cancel.
	start, _ = fx.svc.Start(context.Background(), "alice", "cancel", "")
	fx.svc.Cancel("alice", start.SessionID)
	if _, err := fx.svc.Poll(context.Background(), "alice", start.SessionID); !errors.Is(err, ErrCodexDeviceSessionNotFound) {
		t.Fatal("cancelled session must be gone")
	}
}

func TestCodexDeviceFlowCancelDuringPollAndPrune(t *testing.T) {
	fx := newDeviceFixture(t)
	start, _ := fx.svc.Start(context.Background(), "alice", "A", "")
	fx.client.pollBlock = make(chan struct{})
	fx.client.results = []deviceResult{{tokens: deviceTokens(t), authorized: true}}
	fx.advance(3 * time.Second)
	done := make(chan error, 1)
	go func() {
		_, err := fx.svc.Poll(context.Background(), "alice", start.SessionID)
		done <- err
	}()
	for {
		fx.svc.mu.Lock()
		busy := fx.svc.sessions[start.SessionID].busy
		fx.svc.mu.Unlock()
		if busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if out, err := fx.svc.Poll(context.Background(), "alice", start.SessionID); err != nil || out.Status != CodexDevicePending {
		t.Fatalf("concurrent poll %+v %v", out, err)
	}
	fx.svc.Cancel("alice", start.SessionID)
	close(fx.client.pollBlock)
	if err := <-done; !errors.Is(err, ErrCodexDeviceSessionNotFound) {
		t.Fatalf("cancelled during poll: %v", err)
	}
	if ids, _ := fx.creds.codexLoginIDs(); len(ids) != 0 {
		t.Fatal("cancelled sign-in saved a credential")
	}
	fx.client.pollBlock = nil

	fx2 := newDeviceFixture(t)
	for i := range codexDeviceMaxPerOwner + 2 {
		fx2.advance(time.Second)
		if _, err := fx2.svc.Start(context.Background(), "bob", "n"+string(rune('a'+i)), ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(fx2.svc.sessions); n != codexDeviceMaxPerOwner {
		t.Fatalf("sessions = %d", n)
	}
	fx2.advance(time.Hour)
	_, _ = fx2.svc.Start(context.Background(), "carol", "c", "")
	if n := len(fx2.svc.sessions); n != 1 {
		t.Fatalf("expired sessions not pruned: %d", n)
	}
}

func TestNewCodexDeviceFlowService(t *testing.T) {
	svc := NewCodexDeviceFlowService(nil, "https://auth.example/")
	if svc.verify != "https://auth.example/codex/device" {
		t.Fatalf("verify %q", svc.verify)
	}
}
