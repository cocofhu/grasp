package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// TestIsRetryableSandboxErr covers the classifier that decides whether a node
// attempt should be transparently retried in a fresh sandbox. Only transient
// infrastructure faults (setup, dropped connection, idle stall) are retryable;
// agent errors, the hard deadline, and contract misses are not.
func TestIsRetryableSandboxErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"setup", errSandboxSetup, true},
		{"setup-wrapped", fmt.Errorf("create sandbox: %w", errSandboxSetup), true},
		{"conn-closed", sandbox.ErrConnClosed, true},
		{"conn-closed-wrapped", fmt.Errorf("agent chat: %w", sandbox.ErrConnClosed), true},
		{"idle", sandbox.ErrChatIdle, true},
		{"idle-wrapped", fmt.Errorf("agent chat: %w", sandbox.ErrChatIdle), true},
		{"deadline", context.DeadlineExceeded, false},
		{"canceled", context.Canceled, false},
		{"agent-error", errors.New("acp error: model refused"), false},
		{"cursor-api", errors.New("acp error: exit status 1; stderr: Failed to reach the Cursor API"), true},
		{"tls-abort", errors.New("acp error: exit status 1; stderr: Error: [aborted] Client network socket disconnected before secure TLS connection was established"), true},
		{"econnreset", errors.New("agent chat: read: connection reset by peer: ECONNRESET"), true},
	}
	for _, tc := range cases {
		if got := isRetryableSandboxErr(tc.err); got != tc.want {
			t.Errorf("%s: isRetryableSandboxErr = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSandboxAttempts clamps the configured cap to a sane minimum of one.
func TestSandboxAttempts(t *testing.T) {
	cases := []struct {
		max  int
		want int
	}{{0, 1}, {1, 1}, {3, 3}, {-2, 1}}
	for _, tc := range cases {
		c := &acpProvider{opts: Options{SandboxMaxAttempts: tc.max}}
		if got := c.sandboxAttempts(); got != tc.want {
			t.Errorf("SandboxMaxAttempts=%d: attempts=%d want %d", tc.max, got, tc.want)
		}
	}
}

// TestNodeBudget: turns draw on one budget per node execution; an exhausted
// budget cancels the next turn with a non-retryable *nodeBudgetError.
func TestNodeBudget(t *testing.T) {
	c := &acpProvider{opts: Options{NodeHardCap: time.Hour}}
	req := NodeReq{RunID: "r", NodeID: "n", Config: map[string]any{"timeout": 30}}

	ctx, cancel := c.turnCtx(context.Background(), req)
	dl, ok := ctx.Deadline()
	cancel()
	if !ok || time.Until(dl) > 30*time.Minute || time.Until(dl) < 29*time.Minute {
		t.Fatalf("fresh budget deadline = %v", time.Until(dl))
	}

	c.budgets.add(reactKey(req), 20*time.Minute)
	ctx, cancel = c.turnCtx(context.Background(), req)
	dl, _ = ctx.Deadline()
	cancel()
	if left := time.Until(dl); left > 10*time.Minute || left < 9*time.Minute {
		t.Fatalf("remaining budget deadline = %v, want ~10m", left)
	}

	c.budgets.add(reactKey(req), 10*time.Minute)
	ctx, cancel = c.turnCtx(context.Background(), req)
	defer cancel()
	err := budgetErr(ctx)
	if ctx.Err() == nil || !errors.Is(err, ErrNodeBudget) || isRetryableSandboxErr(err) {
		t.Fatalf("exhausted budget: ctxErr=%v cause=%v", ctx.Err(), err)
	}
	if !strings.Contains(err.Error(), "节点运行超过总时限 30 分钟") {
		t.Fatalf("message = %q", err.Error())
	}
	if _, serr := c.streamChat(ctx, nil, req, "never sent", nil); !errors.Is(serr, ErrNodeBudget) {
		t.Fatalf("streamChat on exhausted budget = %v", serr)
	}

	c.resetNodeBudget(req)
	if c.budgets.get(reactKey(req)) != 0 {
		t.Fatal("reset must clear the used time")
	}
	other := NodeReq{RunID: "r", NodeID: "other"}
	if d, hard := c.nodeLimit(other); d != time.Hour || !hard {
		t.Fatalf("no timeout falls back to the hard cap, got %v hard=%v", d, hard)
	}
	c.budgets.add(reactKey(other), 2*time.Hour)
	octx, ocancel := c.turnCtx(context.Background(), other)
	defer ocancel()
	if e := budgetErr(octx); e == nil || !strings.Contains(e.Error(), "平台兜底上限 1 小时") {
		t.Fatalf("hard cap message = %v", e)
	}
}

// TestIsChatTimeoutErr covers the classifier used to distinguish timeout
// truncation from contract misses in nudge re-prompt paths.
func TestIsChatTimeoutErr(t *testing.T) {
	if !isChatTimeoutErr(context.DeadlineExceeded) {
		t.Error("DeadlineExceeded should be a chat timeout")
	}
	if isChatTimeoutErr(sandbox.ErrConnClosed) {
		t.Error("connection drop should not be classified as chat timeout")
	}
}

// TestBackoffRespectsContext returns false promptly when the context is done,
// so the retry loop stops instead of sleeping out the full backoff.
func TestBackoffRespectsContext(t *testing.T) {
	c := &acpProvider{opts: Options{SandboxRetryBackoff: 10 * time.Second}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if c.backoff(ctx, 1) {
		t.Errorf("backoff should return false on a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("backoff waited %v despite cancelled context", elapsed)
	}
}
