package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestLiveCodexRefused(t *testing.T) {
	if liveCodexRefused(&sandbox.ChatResult{ErrorText: "Rotated the refresh token and kept the session"}, nil) {
		t.Fatal("a successful turn mentioning tokens is not a refusal")
	}
	if !liveCodexRefused(&sandbox.ChatResult{Failed: true, ErrorText: "unexpected status 401 Unauthorized: Missing bearer, url: https://api.openai.com/v1/responses"}, nil) {
		t.Fatal("a 401 login failure is a refusal")
	}
	if !liveCodexRefused(nil, errors.New("failed to refresh token")) {
		t.Fatal("a refresh failure error is a refusal")
	}
}

func TestCodexLiveTurnUnregisteredRunsOnce(t *testing.T) {
	s := &SandboxService{codexLogins: runtime.NewCodexLoginHub(nil)}
	calls := 0
	_, _ = s.codexLiveTurn(context.Background(), 7, func() (*sandbox.ChatResult, error) {
		calls++
		return &sandbox.ChatResult{Failed: true, ErrorText: "401 Unauthorized"}, nil
	})
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestCodexLiveTurnRenewsAndRetriesOnce(t *testing.T) {
	fx := newCodexMgrFixture(t, testAccess(0, time.Now().Add(240*time.Hour)))
	copy0, version, err := fx.m.EnsureFresh(context.Background(), fx.id)
	if err != nil {
		t.Fatal(err)
	}
	file := &memCodexFile{body: copy0}
	fx.hub.Register(fx.id, liveCodexKey(7), file, "/root/.codex/auth.json", version)
	s := &SandboxService{codexLogins: fx.hub}
	calls := 0
	refused := &sandbox.ChatResult{Failed: true, ErrorText: "unexpected status 401 Unauthorized: Missing bearer, url: https://api.openai.com/v1/responses"}
	res, err := s.codexLiveTurn(context.Background(), 7, func() (*sandbox.ChatResult, error) {
		calls++
		if calls == 1 {
			return refused, nil
		}
		return &sandbox.ChatResult{}, nil
	})
	if err != nil || res.Failed || calls != 2 || fx.api.calls.Load() != 1 {
		t.Fatalf("calls=%d refreshes=%d err=%v", calls, fx.api.calls.Load(), err)
	}
	body, _ := file.get()
	assertSandboxCopy(t, body, fx.stored(t).AccessToken())

	// Still refused after the renewal: no second retry.
	calls = 0
	res, _ = s.codexLiveTurn(context.Background(), 7, func() (*sandbox.ChatResult, error) {
		calls++
		return refused, nil
	})
	if calls != 2 || !res.Failed || fx.api.calls.Load() != 2 {
		t.Fatalf("calls=%d refreshes=%d", calls, fx.api.calls.Load())
	}
	// A renewal that needs a new sign-in fails the turn without retrying.
	fx.api.set(func(f *fakeOpenAI) { f.status, f.body = http.StatusUnauthorized, `{"error":"invalid_grant"}` })
	calls = 0
	_, _ = s.codexLiveTurn(context.Background(), 7, func() (*sandbox.ChatResult, error) {
		calls++
		return refused, nil
	})
	if calls != 1 {
		t.Fatalf("terminal renewal retried: calls=%d", calls)
	}
}
