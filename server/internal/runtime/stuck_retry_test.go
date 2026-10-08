package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// A turn the bridge stopped as stuck restarts the node once in a fresh
// sandbox, telling the Agent why, without using the sandbox-fault attempts.
func TestRunAgentStuckRetriesOnce(t *testing.T) {
	p, _, store, mgr, _, req := setupProvider(t, func(attempt int) chatFunc {
		return func(int) turnAction {
			if attempt == 0 {
				return turnAction{narration: "watching", errorText: "Agent 连续 20 分钟没有任何活动", stopReason: "stuck"}
			}
			return turnAction{narration: "second try", produces: map[string]string{"report.md": "ok"}}
		}
	})
	p.opts.SandboxMaxAttempts = 1
	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("stuck agent should be retried once: %v", err)
	}
	if res.OutputMd != "second try" {
		t.Fatalf("output = %q", res.OutputMd)
	}
	if _, ok := store.Get("run-1", "report.md"); !ok {
		t.Error("produces missing after stuck retry")
	}
	if mgr.createCount() != 2 {
		t.Fatalf("create count = %d, want 2", mgr.createCount())
	}
	if first := mgr.bridge(0).promptAt(0); strings.Contains(first, "上一次执行被判定为卡住") {
		t.Fatal("first attempt must not carry the stuck note")
	}
	if retry := mgr.bridge(1).promptAt(0); !strings.Contains(retry, "上一次执行被判定为卡住") || !strings.Contains(retry, "没有任何活动") {
		t.Fatalf("retry prompt lacks the stuck note:\n%s", retry)
	}
	if _, ok := p.stuckNotes.Load(reactKey(req)); ok {
		t.Fatal("stuck note must be cleared when the node ends")
	}
}

func TestRunAgentStuckTwiceFails(t *testing.T) {
	p, _, _, mgr, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{narration: "x", stopReason: "stuck"} }
	})
	p.opts.SandboxMaxAttempts = 3
	_, err := p.RunAgent(context.Background(), req)
	if !errors.Is(err, sandbox.ErrAgentStuck) {
		t.Fatalf("err = %v, want ErrAgentStuck", err)
	}
	if mgr.createCount() != 2 {
		t.Fatalf("create count = %d, want 2 (one stuck retry only)", mgr.createCount())
	}
}

func TestTimeLimitsText(t *testing.T) {
	if got := timeLimitsText(NodeReq{Config: map[string]any{}}, 0); got != "" {
		t.Fatalf("no limits: %q", got)
	}
	got := timeLimitsText(NodeReq{Config: map[string]any{"timeout": 90}}, 20*time.Minute)
	if !strings.Contains(got, "连续 20 分钟") || !strings.Contains(got, "本节点总时限 90 分钟") {
		t.Fatalf("limits text: %q", got)
	}
}

func TestAgentIdlePrefersSettings(t *testing.T) {
	p := &acpProvider{opts: Options{ChatIdleTimeout: 7 * time.Minute}}
	if got := p.agentIdle(); got != 7*time.Minute {
		t.Fatalf("agentIdle = %s, want the boot value", got)
	}
	sandbox.SetAgentIdleTimeout(33 * time.Minute)
	t.Cleanup(func() { sandbox.SetAgentIdleTimeout(0) })
	if got := p.agentIdle(); got != 33*time.Minute {
		t.Fatalf("agentIdle = %s, want the settings value", got)
	}
}

// A sandbox heartbeat reaches the live event stream as kind=liveness.
func TestRunAgentEmitsLiveness(t *testing.T) {
	p, _, _, _, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{heartbeat: true, narration: "ok", produces: map[string]string{"report.md": "ok"}}
		}
	})
	var got *models.AcpLiveness
	p.emit = func(_, _ string, events []models.AcpEvent, _ bool) {
		for _, ev := range events {
			if ev.Kind == models.AcpKindLiveness {
				got = ev.Liveness
			}
		}
	}
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Active || got.CPUMs != 1200 || got.LimitSec != 1200 || got.LastTool != "go test" {
		t.Fatalf("liveness = %+v", got)
	}
}
