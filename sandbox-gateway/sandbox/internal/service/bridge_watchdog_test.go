package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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

	et := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && errorText(f) != "" })
	if et.OpID != "op-A" || !strings.Contains(errorText(et), "没有任何输出") {
		t.Fatalf("timeout explanation=%+v text=%q", et, errorText(et))
	}
	done := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_done" })
	if done.OpID != "op-A" || stopReasonOf(done) != "timeout" {
		t.Fatalf("prompt_done=%+v stop=%q", done, stopReasonOf(done))
	}
	readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_begin" && f.OpID == "op-B" })
	b.CancelPromptOp("op-B")
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
