package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func shrinkExitProbe(t *testing.T) {
	t.Helper()
	tries, every := exitProbeTries, exitProbeEvery
	exitProbeTries, exitProbeEvery = 3, time.Millisecond
	t.Cleanup(func() { exitProbeTries, exitProbeEvery = tries, every })
}

func TestExitInfoDescribe(t *testing.T) {
	for _, tc := range []struct {
		e    ExitInfo
		want string
	}{
		{ExitInfo{Reason: "OOMKilled", OOMKilled: true, MemoryMB: 8192}, "沙箱 OOM 被杀(8192MiB)"},
		{ExitInfo{Reason: "OOMKilled", OOMKilled: true}, "沙箱 OOM 被杀"},
		{ExitInfo{Reason: "Evicted", Message: "low on memory"}, "沙箱被驱逐 low on memory"},
		{ExitInfo{Reason: "Error", ExitCode: 2}, "沙箱容器退出(Error,退出码 2)"},
	} {
		if got := tc.e.Describe(); got != tc.want {
			t.Errorf("Describe(%+v) = %q, want %q", tc.e, got, tc.want)
		}
	}
}

func TestExplainLoss(t *testing.T) {
	shrinkExitProbe(t)
	ctx := context.Background()
	gw, fg := newGatewayFake(t, "")
	m := NewManager(gw, ManagerOptions{})
	fg.recs["sb1"] = map[string]any{"id": "sb1"}
	sb := &Sandbox{ID: "sb1", mgr: m}
	lost := fmt.Errorf("agent chat: %w: not connected", ErrConnClosed)

	if got := sb.ExplainLoss(ctx, errors.New("other")); got.Error() != "other" {
		t.Fatalf("non-loss error changed: %v", got)
	}
	if got := sb.ExplainLoss(ctx, lost); got != lost {
		t.Fatalf("no exit recorded: %v", got)
	}

	calls := 0
	fg.mu.Lock()
	fg.recs["sb1"]["exitFn"] = func() any {
		calls++
		if calls < 2 {
			return nil
		}
		return map[string]any{"reason": "OOMKilled", "exitCode": 137, "oomKilled": true, "memoryMB": 8192}
	}
	fg.mu.Unlock()
	got := sb.ExplainLoss(ctx, lost)
	e, ok := AsExitError(got)
	if !ok || !e.OOMKilled || !errors.Is(got, ErrConnClosed) || !strings.HasPrefix(got.Error(), "沙箱 OOM 被杀(8192MiB): agent chat") {
		t.Fatalf("got %v (calls=%d)", got, calls)
	}
	if _, ok := AsExitError(lost); ok {
		t.Fatal("plain error carries no exit")
	}

	idle := fmt.Errorf("x: %w", ErrChatIdle)
	gone := &Sandbox{ID: "missing", mgr: m}
	if got := gone.ExplainLoss(ctx, idle); got != idle {
		t.Fatalf("unknown sandbox: %v", got)
	}
	if got := (&Sandbox{ID: "sb1"}).ExplainLoss(ctx, lost); got != lost {
		t.Fatalf("no manager: %v", got)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	fg.mu.Lock()
	fg.recs["sb1"]["exitFn"] = func() any { return nil }
	fg.mu.Unlock()
	if got := sb.ExplainLoss(cctx, lost); got != lost {
		t.Fatalf("cancelled: %v", got)
	}
}
