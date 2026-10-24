package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const codex401 = "failed to connect to websocket: HTTP error: 401 Unauthorized, url: wss://api.openai.com/v1/responses"

func (b *fakeBridge) turnCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turns
}

func TestRunAgentCodexLoginRejectedFailsFast(t *testing.T) {
	p, mgr, req := codexSetup(t, func(int) turnAction { return turnAction{errorText: codex401, failed: true} })
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

func codexSetup(t *testing.T, turn func(int) turnAction) (*acpProvider, *fakeManager, NodeReq) {
	t.Helper()
	p, _, _, mgr, _, req := setupProviderBackend(t, BackendCodex, func(int) chatFunc { return turn })
	p.opts.ModelBinding = testModelBinding
	return p, mgr, req
}

func TestRunAgentCodexLoginRejectedOnNudgeTurn(t *testing.T) {
	p, mgr, req := codexSetup(t, func(turn int) turnAction {
		if turn == 0 {
			return turnAction{narration: "working"}
		}
		return turnAction{errorText: codex401, failed: true}
	})
	_, err := p.RunAgent(context.Background(), req)
	if !errors.Is(err, ErrCodexLoginRejected) {
		t.Fatalf("err = %v, want ErrCodexLoginRejected", err)
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("CLI detail dropped: %q", err.Error())
	}
	if n := mgr.bridge(0).turnCount(); n != 2 {
		t.Fatalf("turns = %d, want 2 (first turn + one failed re-prompt)", n)
	}
}

func TestRunAgentCodex401InNarrationIsNotLoginFailure(t *testing.T) {
	p, _, req := codexSetup(t, func(int) turnAction {
		return turnAction{narration: "curl said: " + codex401, produces: map[string]string{"report.md": "ok"}, outcome: true}
	})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("successful turn mentioning 401 must not fail: %v", err)
	}
}

func TestCodexLoginRepasteMarkInMessage(t *testing.T) {
	if !strings.Contains(CodexLoginRepasteMessage, CodexLoginRepasteMark) {
		t.Fatal("CodexLoginRepasteMessage must contain CodexLoginRepasteMark (engine no-retry and web banner key on it)")
	}
	if !IsCodexAuthRejectionText("gateway: " + CodexLoginRepasteMark) {
		t.Fatal("the mark alone must classify as a login rejection")
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
