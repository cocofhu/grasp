package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
)

const codex401 = "failed to connect to websocket: HTTP error: 401 Unauthorized, url: wss://api.openai.com/v1/responses"

func (b *fakeBridge) turnCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turns
}

func TestRunAgentCodexLoginRejectedFailsFast(t *testing.T) {
	p, _, _, mgr, _, req := setupProviderBackend(t, BackendCodex, func(int) chatFunc {
		return func(int) turnAction { return turnAction{errorText: codex401, failed: true} }
	})
	p.opts.ProjectCredentialsForProject = func(string) map[string]string {
		return map[string]string{envauth.EnvCodexAuthFile: chatgptLogin}
	}
	_, err := p.RunAgent(context.Background(), req)
	if !errors.Is(err, ErrCodexLoginRejected) {
		t.Fatalf("err = %v, want ErrCodexLoginRejected", err)
	}
	if isRetryableSandboxErr(err) {
		t.Fatalf("login rejection must not be retryable: %v", err)
	}
	if mgr.createCount() != 1 {
		t.Fatalf("create count = %d, want 1 (no sandbox retry)", mgr.createCount())
	}
	if n := mgr.bridge(0).turnCount(); n != 1 {
		t.Fatalf("turns = %d, want 1 (no re-prompt after a refused login)", n)
	}
}

func TestRunAgentFailedTurnOnOtherBackendKeepsNudging(t *testing.T) {
	p, _, _, mgr, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{errorText: codex401, failed: true} }
	})
	_, err := p.RunAgent(context.Background(), req)
	if errors.Is(err, ErrCodexLoginRejected) {
		t.Fatalf("non-Codex backend must not report a Codex login failure: %v", err)
	}
	if n := mgr.bridge(0).turnCount(); n < 2 {
		t.Fatalf("turns = %d, want re-prompts to continue", n)
	}
}

func TestAsCodexLoginErr(t *testing.T) {
	if got := asCodexLoginErr(nil); got != ErrCodexLoginRejected {
		t.Fatalf("nil -> %v", got)
	}
	base := errors.New("agent chat: boom")
	wrapped := asCodexLoginErr(base)
	if !errors.Is(wrapped, ErrCodexLoginRejected) || !errors.Is(wrapped, base) {
		t.Fatalf("wrapped must match both: %v", wrapped)
	}
	if !strings.HasPrefix(wrapped.Error(), CodexLoginRepasteMessage) {
		t.Fatalf("message = %q", wrapped.Error())
	}
	carried := errors.New("agent chat: " + CodexLoginRepasteMessage)
	got := asCodexLoginErr(carried)
	if !errors.Is(got, ErrCodexLoginRejected) || got.Error() != carried.Error() {
		t.Fatalf("carried message must not repeat: %q", got.Error())
	}
	if asCodexLoginErr(wrapped) != wrapped {
		t.Fatal("already wrapped error must be returned as is")
	}
}

func TestSettleCodexLoginWrapsRejectedError(t *testing.T) {
	p := &acpProvider{backend: BackendCodex}
	req := NodeReq{RunID: "r", NodeID: "n"}
	err := p.settleCodexLogin(req, nil, errors.New("agent chat: "+codex401), nil)
	if !errors.Is(err, ErrCodexLoginRejected) {
		t.Fatalf("err = %v, want ErrCodexLoginRejected", err)
	}
	if err := p.settleCodexLogin(req, nil, nil, nil); err != nil {
		t.Fatalf("no error and no rejection must stay nil: %v", err)
	}
}
