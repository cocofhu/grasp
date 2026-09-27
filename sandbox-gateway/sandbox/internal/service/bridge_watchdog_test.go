package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backend/internal/provider"
)

func TestParseTurnLimit(t *testing.T) {
	def := 10 * time.Minute
	cases := map[string]time.Duration{
		"":      def,
		"0":     0,
		"-5":    0,
		"900":   900 * time.Second,
		"15m":   15 * time.Minute,
		"bogus": def,
	}
	for in, want := range cases {
		if got := parseTurnLimit(in, def); got != want {
			t.Errorf("parseTurnLimit(%q)=%s want %s", in, got, want)
		}
	}
}

func errorText(f wsFrame) string {
	var d struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(f.Data, &d)
	if d.Type != "error_text" {
		return ""
	}
	return d.Text
}

func stopReasonOf(f wsFrame) string {
	var d struct {
		StopReason string `json:"stopReason"`
	}
	_ = json.Unmarshal(f.Data, &d)
	return d.StopReason
}

func TestWatchdogIdleEndsTurnAndPumpsNext(t *testing.T) {
	sess := &blockingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	b.turnIdle, b.turnMax = 80*time.Millisecond, 0
	c := dialBridge(t, b)

	_ = b.ChatWithOpID("hangs", "op-A", "chat", nil)
	_ = b.ChatWithOpID("next", "op-B", "chat", nil)

	seg := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "turn_segment" })
	if seg.OpID != "op-A" {
		t.Fatalf("turn_segment=%+v", seg)
	}
	et := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && errorText(f) != "" })
	if et.OpID != "op-A" || !strings.Contains(errorText(et), "没有任何输出") || !strings.Contains(errorText(et), "回合已停止") {
		t.Fatalf("timeout explanation=%+v text=%q", et, errorText(et))
	}
	if strings.Contains(errorText(et), "setsid") || strings.Contains(errorText(et), "nohup") {
		t.Fatalf("user-facing timeout must not include the restart hint: %q", errorText(et))
	}
	done := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_done" })
	if done.OpID != "op-A" || stopReasonOf(done) != "timeout" {
		t.Fatalf("prompt_done=%+v stop=%q", done, stopReasonOf(done))
	}
	readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_begin" && f.OpID == "op-B" })
	if n := sess.prompts.Load(); n < 2 {
		t.Fatalf("first idle must resume once, prompts=%d", n)
	}
	b.CancelPromptOp("op-B")
	waitIdle(t, b)
}

func TestWatchdogContinueDoesNotResetMaxDuration(t *testing.T) {
	sess := &blockingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	b.turnIdle, b.turnMax = 40*time.Millisecond, 130*time.Millisecond
	c := dialBridge(t, b)

	start := time.Now()
	go func() {
		for time.Since(start) < time.Second {
			if sess.prompts.Load() >= 2 {
				b.touchActiveTurn()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	_ = b.ChatWithOpID("hangs", "op-A", "chat", nil)
	et := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && errorText(f) != "" })
	if !strings.Contains(errorText(et), "总时长") {
		t.Fatalf("continuation must still honor the original max, got %q", errorText(et))
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("max clock was reset across the resume: elapsed %s", elapsed)
	}
	waitIdle(t, b)
}

func TestWatchdogActivityKeepsTurnAlive(t *testing.T) {
	sess := &blockingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	b.turnIdle, b.turnMax = 100*time.Millisecond, 0

	_ = b.ChatWithOpID("busy", "op-A", "chat", nil)
	stop := time.After(400 * time.Millisecond)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
loop:
	for {
		select {
		case <-stop:
			break loop
		case <-tick.C:
			b.touchActiveTurn()
		}
	}
	if b.activeOpID() != "op-A" {
		t.Fatalf("turn with steady output was killed (active=%q)", b.activeOpID())
	}
	b.CancelPromptOp("op-A")
	waitIdle(t, b)
}

// lingerSess already finished (end_turn) and is only waiting on process exit.
type lingerSess struct{ stubSess }

func (s *lingerSess) Prompt(ctx context.Context, _ string, _ []provider.PromptImage) (provider.TurnResult, error) {
	<-ctx.Done()
	return provider.TurnResult{StopReason: "end_turn"}, nil
}

func TestWatchdogDoesNotErrorAfterEndTurn(t *testing.T) {
	b := newTestBridge(&lingerSess{stubSess: stubSess{id: "s1"}})
	b.turnIdle, b.turnMax = 80*time.Millisecond, 0
	c := dialBridge(t, b)

	_ = b.ChatWithOpID("already done", "op-A", "chat", nil)
	waitIdle(t, b)

	_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			break
		}
		var f wsFrame
		if json.Unmarshal(raw, &f) == nil && errorText(f) != "" {
			t.Fatalf("lingering end_turn must not emit error_text: %q", errorText(f))
		}
	}
}

func TestWatchdogPerChatMaxDuration(t *testing.T) {
	sess := &blockingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	b.turnIdle, b.turnMax = 0, time.Hour
	c := dialBridge(t, b)

	go func() {
		// Steady activity: only the total-duration limit can end this turn.
		for b.activeOpID() != "" || sess.prompts.Load() == 0 {
			b.touchActiveTurn()
			time.Sleep(10 * time.Millisecond)
		}
	}()
	_ = b.ChatWithDeadline("long", "op-A", "chat", nil, 120*time.Millisecond)
	et := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && errorText(f) != "" })
	if !strings.Contains(errorText(et), "总时长") {
		t.Fatalf("want total-duration reason, got %q", errorText(et))
	}
	waitIdle(t, b)
}
