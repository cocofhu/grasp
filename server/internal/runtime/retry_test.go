package runtime

import (
	"context"
	"errors"
	"fmt"
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

// TestNodeChatTimeout: the node's timeout (minutes) wins, clarify Agents
// default to 30m, everything else uses the global chat timeout.
func TestNodeChatTimeout(t *testing.T) {
	c := &acpProvider{opts: Options{ChatTimeout: 90 * time.Second}}

	if d := c.nodeChatTimeout(NodeReq{}); d != 90*time.Second {
		t.Errorf("default timeout = %v, want 90s", d)
	}
	if d := c.nodeChatTimeout(NodeReq{Config: map[string]any{"timeout": 20}}); d != 20*time.Minute {
		t.Errorf("timeout(min) = %v, want 20m", d)
	}
	if d := c.nodeChatTimeout(NodeReq{Caps: testClarifyCaps, Config: map[string]any{"timeout": 20}}); d != 20*time.Minute {
		t.Errorf("clarify timeout(min) = %v, want 20m", d)
	}
	if d := c.nodeChatTimeout(NodeReq{Caps: testClarifyCaps}); d != 30*time.Minute {
		t.Errorf("clarify default = %v, want 30m", d)
	}
}

// TestNodeChatTimeoutNudgeInheritance documents that re-prompt (nudge) paths
// use nodeChatTimeout(req) — the same helper as the main turn — so a configured
// node timeout applies to each nudge round independently.
func TestNodeChatTimeoutNudgeInheritance(t *testing.T) {
	c := &acpProvider{opts: Options{ChatTimeout: 90 * time.Second}}
	req := NodeReq{Config: map[string]any{"timeout": 120}}
	if d := c.nodeChatTimeout(req); d != 120*time.Minute {
		t.Errorf("nudge rounds should inherit node timeout = %v, want 120m", d)
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
