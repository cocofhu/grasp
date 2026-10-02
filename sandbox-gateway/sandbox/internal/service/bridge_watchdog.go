package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/provider"
)

const (
	defaultTurnIdle      = 10 * time.Minute
	defaultTurnMax       = 60 * time.Minute
	defaultTurnQuietIdle = 3 * time.Minute

	envTurnIdle      = "SANDBOX_TURN_IDLE_TIMEOUT"
	envTurnMax       = "SANDBOX_TURN_MAX_DURATION"
	envTurnQuietIdle = "SANDBOX_TURN_QUIET_IDLE_TIMEOUT"
)

func turnLimitsFromEnv() (idle, max, quiet time.Duration) {
	return parseTurnLimit(os.Getenv(envTurnIdle), defaultTurnIdle),
		parseTurnLimit(os.Getenv(envTurnMax), defaultTurnMax),
		parseTurnLimit(os.Getenv(envTurnQuietIdle), defaultTurnQuietIdle)
}

// parseTurnLimit accepts a Go duration ("15m") or plain seconds ("900"); "0"
// disables the limit; empty or malformed falls back to def.
func parseTurnLimit(v string, def time.Duration) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n <= 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		log.Printf("bridge: %q 不是合法的时长，使用默认 %s", v, def)
		return def
	}
	return d
}

// touchActiveTurn marks provider activity on the in-flight turn and returns its opId.
func (b *Bridge) touchActiveTurn() string { return b.noteTurnEvent(nil) }

// noteTurnEvent is touchActiveTurn for a provider event: it also tracks the
// turn's open tool calls for the quiet-idle limit.
func (b *Bridge) noteTurnEvent(ev json.RawMessage) string {
	b.turnMu.Lock()
	th := b.activeTurn
	b.turnMu.Unlock()
	if th == nil {
		return ""
	}
	th.lastActivity.Store(time.Now().UnixNano())
	th.tools.note(ev)
	return th.opID
}

// turnTools tracks the tool calls a provider reported as started but not yet
// finished within one turn.
type turnTools struct {
	mu   sync.Mutex
	open map[string]struct{}
	seen bool // the provider reported at least one tool call with an id
}

func (t *turnTools) note(ev json.RawMessage) {
	if !bytes.Contains(ev, []byte(`"tool_call`)) {
		return
	}
	var f struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			ToolCallID    string `json:"toolCallId"`
			Status        string `json:"status"`
		} `json:"update"`
	}
	if json.Unmarshal(ev, &f) != nil || f.Update.ToolCallID == "" {
		return
	}
	u := f.Update
	t.mu.Lock()
	defer t.mu.Unlock()
	switch u.SessionUpdate {
	case "tool_call":
		t.seen = true
		if t.open == nil {
			t.open = map[string]struct{}{}
		}
		if !toolStatusDone(u.Status) {
			t.open[u.ToolCallID] = struct{}{}
		}
	case "tool_call_update":
		t.seen = true
		if toolStatusDone(u.Status) {
			delete(t.open, u.ToolCallID)
		}
	}
}

// quiet reports whether no tool is known to be running. A provider that never
// reports tools is never quiet: a long silent command could be one of them.
func (t *turnTools) quiet() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seen && len(t.open) == 0
}

// reset forgets open calls after the process group was killed.
func (t *turnTools) reset() {
	t.mu.Lock()
	t.open = nil
	t.mu.Unlock()
}

func toolStatusDone(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

func (b *Bridge) turnLimits(item queuedPrompt) (idle, max time.Duration) {
	idle, max = b.turnIdle, b.turnMax
	if item.MaxDuration > 0 {
		max = item.MaxDuration
	}
	return idle, max
}

func watchdogTick(idle, max time.Duration) time.Duration {
	shortest := idle
	if shortest == 0 || (max > 0 && max < shortest) {
		shortest = max
	}
	tick := shortest / 5
	if tick < 5*time.Millisecond {
		tick = 5 * time.Millisecond
	}
	if tick > 5*time.Second {
		tick = 5 * time.Second
	}
	return tick
}

// watchTurn aborts th once it has had no provider event for idle, or has run
// longer than max. While no reported tool call is open, the shorter quiet
// limit replaces idle: a CLI that went silent between tools is usually stuck
// waiting on a process it should have detached. The first idle cancels with
// provider.ErrTurnRecover so the bridge can resume once. A second idle, or
// the total-duration limit, cancels with provider.ErrTurnTimeout.
func (b *Bridge) watchTurn(p provider.Session, th *promptTurn, idle, max time.Duration) func() {
	if idle <= 0 && max <= 0 {
		return func() {}
	}
	quiet := b.turnQuietIdle
	if idle <= 0 || quiet >= idle {
		quiet = 0
	}
	stop := make(chan struct{})
	start := th.started
	if start.IsZero() {
		start = time.Now()
	}
	tickBase := idle
	if quiet > 0 {
		tickBase = quiet
	}
	go func() {
		t := time.NewTicker(watchdogTick(tickBase, max))
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				limit := idle
				if quiet > 0 && th.tools.quiet() {
					limit = quiet
				}
				var why string
				if max > 0 && now.Sub(start) >= max {
					why = fmt.Sprintf("回合总时长超过 %s", max)
				} else if limit > 0 && now.Sub(time.Unix(0, th.lastActivity.Load())) >= limit {
					why = fmt.Sprintf("连续 %s 没有任何输出", limit)
				}
				if why == "" {
					continue
				}
				// 总时长是硬停止。空闲第一次只续跑，不结束这一轮用户消息。
				hard := max > 0 && now.Sub(start) >= max
				b.abortTimedOutTurn(p, th, why, hard || th.continued.Load())
				return
			}
		}
	}()
	return func() { close(stop) }
}

func (b *Bridge) abortTimedOutTurn(p provider.Session, th *promptTurn, why string, hard bool) {
	var cause error
	if hard {
		cause = fmt.Errorf("%w: %s，回合已停止", provider.ErrTurnTimeout, why)
		th.timedOut.Store(true)
		log.Printf("prompt %s oid=%s: 看门狗终止回合: %s", b.AgentLogPrefix(), th.opID, why)
	} else {
		cause = fmt.Errorf("%w: %s", provider.ErrTurnRecover, why)
		th.recover.Store(true)
		log.Printf("prompt %s oid=%s: 看门狗准备续跑: %s", b.AgentLogPrefix(), th.opID, why)
	}
	th.lastCause = cause.Error()
	// Do not emit error_text here: Prompt may still return end_turn if the CLI
	// already finished and only hung on exit. Real timeouts explain themselves
	// in executePrompt (or the provider) before prompt_done.
	th.cancelCause(cause)
	if err := p.Cancel(); err != nil {
		log.Printf("prompt %s oid=%s: 看门狗通知 Agent 取消失败: %v", b.AgentLogPrefix(), th.opID, err)
	}
}

func timeoutCauseText(ctx context.Context) string {
	if cause := context.Cause(ctx); cause != nil {
		return cause.Error()
	}
	return "回合超时被终止"
}

// finishedBeforeExit is true when the agent already reported a successful
// end_turn and only failed to exit (foreground service still holding the pipe).
func finishedBeforeExit(stopReason string, err error) bool {
	return err == nil && strings.EqualFold(strings.TrimSpace(stopReason), "end_turn")
}
