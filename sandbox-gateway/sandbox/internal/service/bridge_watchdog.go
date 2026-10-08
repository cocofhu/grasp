package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"backend/internal/provider"
)

const (
	defaultTurnIdle = 20 * time.Minute
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

// touchActiveTurn marks provider activity on the in-flight turn, feeds the
// tool-loop detector, and returns the turn's opId.
func (b *Bridge) touchActiveTurn(ev json.RawMessage) string {
	b.turnMu.Lock()
	th := b.activeTurn
	b.turnMu.Unlock()
	if th == nil {
		return ""
	}
	th.lastActivity.Store(time.Now().UnixNano())
	if th.loop.note(ev) && th.loopC != nil {
		select {
		case th.loopC <- struct{}{}:
		default:
		}
	}
	return th.opID
}

func (b *Bridge) turnLimits(item queuedPrompt) (idle, max time.Duration) {
	idle, max = b.turnIdle, b.turnMax
	if item.IdleTimeout > 0 {
		idle = item.IdleTimeout
	}
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

// livenessRoots are the process trees whose CPU / IO counts as the Agent's:
// the CLI processes of a session that reports them, else every child of the
// bridge (a negative root means "the children of").
func livenessRoots(p provider.Session) func() []int {
	if r, ok := p.(agentPIDReporter); ok {
		return r.AgentPIDs
	}
	return func() []int { return []int{-os.Getpid()} }
}

func (b *Bridge) newLivenessMonitor(p provider.Session) *livenessMonitor {
	m := &livenessMonitor{sampler: b.sampler, roots: livenessRoots(p), cpuMin: b.livenessCPU, ioMin: b.livenessIO}
	if r, ok := p.(backgroundWaitReporter); ok {
		m.paused = r.WaitingOnBackground
	}
	return m
}

// watchTurn stops th once it has gone idle (no provider event, and no CPU or
// IO in the Agent's process tree) for idle, kept repeating the same tool call,
// or has run longer than max. The first idle or loop cancels with
// provider.ErrTurnRecover so the bridge can resume once; the next one stops
// the turn with provider.ErrTurnStuck. Running past max stops it with
// provider.ErrTurnTimeout. While it runs, a liveness heartbeat goes out every
// livenessHeartbeatEvery.
func (b *Bridge) watchTurn(p provider.Session, th *promptTurn, idle, max time.Duration) func() {
	if idle <= 0 && max <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	start := th.started
	if start.IsZero() {
		start = time.Now()
	}
	mon := b.newLivenessMonitor(p)
	go func() {
		t := time.NewTicker(watchdogTick(idle, max))
		defer t.Stop()
		sample := time.NewTicker(livenessSampleEvery)
		defer sample.Stop()
		beat := time.NewTicker(livenessHeartbeatEvery)
		defer beat.Stop()
		lastBeat := time.Now()
		for {
			select {
			case <-stop:
				return
			case now := <-sample.C:
				if mon.check() {
					th.lastActivity.Store(now.UnixNano())
				}
			case now := <-beat.C:
				b.sendLiveness(th, mon, idle, lastBeat, now)
				lastBeat = now
			case <-th.loopC:
				why := fmt.Sprintf("连续 %d 次执行同一个工具调用（%s）", loopRepeatLimit, th.loop.tool())
				b.abortTimedOutTurn(p, th, why, stopStuckOrRecover(th, true))
				return
			case now := <-t.C:
				if max > 0 && now.Sub(start) >= max {
					b.abortTimedOutTurn(p, th, fmt.Sprintf("回合总时长超过 %s", max), stopTimeout)
					return
				}
				if idle > 0 && now.Sub(time.Unix(0, th.lastActivity.Load())) >= idle {
					why := fmt.Sprintf("连续 %s 没有任何输出、CPU 或磁盘活动", idle)
					b.abortTimedOutTurn(p, th, why, stopStuckOrRecover(th, false))
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

// sendLiveness broadcasts what the watchdog saw since the previous heartbeat.
func (b *Bridge) sendLiveness(th *promptTurn, mon *livenessMonitor, idle time.Duration, since, now time.Time) {
	active, cpu, io := mon.takeBeat()
	last := time.Unix(0, th.lastActivity.Load())
	if last.After(since) {
		active = true
	}
	b.Broadcast(map[string]any{"op": "liveness", "opId": th.opID, "data": map[string]any{
		"active":   active,
		"cpuMs":    cpu.Milliseconds(),
		"ioBytes":  io,
		"idleSec":  int(max(0, now.Sub(last)) / time.Second),
		"limitSec": int(idle / time.Second),
		"lastTool": th.loop.tool(),
	}})
}

type stopKind int

const (
	stopRecover stopKind = iota
	stopStuck
	stopTimeout
)

// stopStuckOrRecover resumes the first stall of a turn and stops the next.
func stopStuckOrRecover(th *promptTurn, loop bool) stopKind {
	if th.continued.Load() {
		return stopStuck
	}
	th.loopRecover.Store(loop)
	return stopRecover
}

func (b *Bridge) abortTimedOutTurn(p provider.Session, th *promptTurn, why string, kind stopKind) {
	var cause error
	switch kind {
	case stopRecover:
		cause = fmt.Errorf("%w: %s", provider.ErrTurnRecover, why)
		th.recover.Store(true)
		log.Printf("prompt %s oid=%s: 看门狗准备续跑: %s", b.AgentLogPrefix(), th.opID, why)
	case stopStuck:
		cause = fmt.Errorf("%w: %s，判定 Agent 卡住，回合已停止", provider.ErrTurnStuck, why)
		th.timedOut.Store(true)
		log.Printf("prompt %s oid=%s: 看门狗判定卡住: %s", b.AgentLogPrefix(), th.opID, why)
	default:
		cause = fmt.Errorf("%w: %s，回合已停止", provider.ErrTurnTimeout, why)
		th.timedOut.Store(true)
		log.Printf("prompt %s oid=%s: 看门狗终止回合: %s", b.AgentLogPrefix(), th.opID, why)
	}
	th.lastCause = cause.Error()
	th.lastErr = cause
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
