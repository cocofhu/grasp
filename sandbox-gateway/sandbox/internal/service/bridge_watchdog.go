package service

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"backend/internal/provider"
)

const (
	defaultTurnIdle = 10 * time.Minute
	defaultTurnMax  = 60 * time.Minute

	envTurnIdle = "SANDBOX_TURN_IDLE_TIMEOUT"
	envTurnMax  = "SANDBOX_TURN_MAX_DURATION"
)

func turnLimitsFromEnv() (idle, max time.Duration) {
	return parseTurnLimit(os.Getenv(envTurnIdle), defaultTurnIdle),
		parseTurnLimit(os.Getenv(envTurnMax), defaultTurnMax)
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
func (b *Bridge) touchActiveTurn() string {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	if b.activeTurn == nil {
		return ""
	}
	b.activeTurn.lastActivity.Store(time.Now().UnixNano())
	return b.activeTurn.opID
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
// longer than max. The turn ctx is cancelled with a provider.ErrTurnTimeout
// cause so transports that emit their own prompt_done can report "timeout".
// The returned func stops the watchdog.
func (b *Bridge) watchTurn(p provider.Session, th *promptTurn, idle, max time.Duration) func() {
	if idle <= 0 && max <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	start := time.Now()
	go func() {
		t := time.NewTicker(watchdogTick(idle, max))
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				var why string
				if max > 0 && now.Sub(start) >= max {
					why = fmt.Sprintf("回合总时长超过 %s", max)
				} else if idle > 0 && now.Sub(time.Unix(0, th.lastActivity.Load())) >= idle {
					why = fmt.Sprintf("连续 %s 没有任何输出", idle)
				}
				if why == "" {
					continue
				}
				b.abortTimedOutTurn(p, th, why)
				return
			}
		}
	}()
	return func() { close(stop) }
}

func (b *Bridge) abortTimedOutTurn(p provider.Session, th *promptTurn, why string) {
	cause := fmt.Errorf("%w: %s，回合已被沙箱终止。若本轮在前台启动了常驻服务（如 go run / npm start），它可能随后退出，请改用 setsid nohup <cmd> </dev/null >log 2>&1 & 在后台重新拉起", provider.ErrTurnTimeout, why)
	log.Printf("prompt %s oid=%s: 看门狗终止回合: %s", b.AgentLogPrefix(), th.opID, why)
	th.timedOut.Store(true)
	// Explain first: prompt_done is emitted inside Prompt, and clients stop reading at prompt_done.
	b.Broadcast(eventEnvelope(map[string]any{"op": "raw", "type": "error_text", "text": cause.Error(), "opId": th.opID}, th.opID))
	th.cancelCause(cause)
	if err := p.Cancel(); err != nil {
		log.Printf("prompt %s oid=%s: 看门狗通知 Agent 取消失败: %v", b.AgentLogPrefix(), th.opID, err)
	}
}
