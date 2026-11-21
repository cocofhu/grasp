package service

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/internal/agents"
	"backend/internal/provider"
)

const (
	// defaultAgentMaxAge bounds how long one long-lived Agent process serves a
	// chat before it is replaced between turns.
	defaultAgentMaxAge = 6 * time.Hour
	envAgentMaxAge     = "SANDBOX_AGENT_MAX_AGE"

	thawProbeEvery = 5 * time.Second
	// thawGap is how far the wall clock must run ahead of the monotonic clock
	// to count as a freeze; smaller steps are ordinary clock adjustment.
	thawGap = 30 * time.Second
)

func agentMaxAgeFromEnv() time.Duration {
	return parseTurnLimit(os.Getenv(envAgentMaxAge), defaultAgentMaxAge)
}

// agent is the provider this chat opens sessions with.
func (b *Bridge) agent() provider.Provider {
	if b.agentProvider != nil {
		return b.agentProvider
	}
	return agents.Current()
}

// recycleStaleAgent replaces a long-lived Agent process before the next turn
// when it may hold dead connections (the sandbox was frozen since it started)
// or has served longer than maxAgentAge. Only Agents that resume their native
// session are recycled: for the others a new process would lose the
// conversation.
func (b *Bridge) recycleStaleAgent() {
	b.mu.Lock()
	sess, opened, thaw, config := b.sess, b.sessOpened, b.sessThaw, b.sessConfig
	b.mu.Unlock()
	why := b.staleReason(sess, opened, thaw, config)
	if why == "" {
		return
	}
	b.ensureMu.Lock()
	defer b.ensureMu.Unlock()
	b.turnMu.Lock()
	b.mu.Lock()
	current := b.sess == sess
	b.mu.Unlock()
	if b.activeTurn != nil || b.recycling || !current {
		b.turnMu.Unlock()
		return
	}
	b.recycling = true
	b.turnMu.Unlock()
	defer func() {
		b.turnMu.Lock()
		b.recycling = false
		b.turnMu.Unlock()
	}()
	log.Printf("acp: %s，回合前重建 Agent 进程并接回会话 %s", why, sess.SessionID())
	next, err := b.replaceLocked(sess)
	if err != nil {
		log.Printf("acp: 重建 Agent 进程失败: %v", err)
		return
	}
	b.Broadcast(b.ConnectedPayload(next))
}

func (b *Bridge) staleReason(sess provider.Session, opened time.Time, thaw int64, config string) string {
	if !sessionUsable(sess) || b.agent().Transport() == provider.OneShot || resumePointer(sess) == "" {
		return ""
	}
	if thawCount() != thaw {
		return "沙箱从休眠中恢复"
	}
	if b.maxAgentAge > 0 && !opened.IsZero() && time.Since(opened) >= b.maxAgentAge {
		return fmt.Sprintf("Agent 进程已运行超过 %s", b.maxAgentAge)
	}
	if configStamp(sess) != config {
		return "Agent 的登录或配置文件已更新"
	}
	return ""
}

// configStamp identifies the content of the files a session read at start;
// it is empty for sessions that re-read their configuration on their own.
func configStamp(sess provider.Session) string {
	r, ok := sess.(provider.ConfigReader)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, path := range r.ConfigFiles() {
		b.WriteString(path)
		if st, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, ":%d:%d", st.Size(), st.ModTime().UnixNano())
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// thaws counts the times this sandbox was frozen and resumed (an E2B pause)
// while the bridge ran. A frozen guest does not advance its monotonic clock,
// and the wall clock is set forward on resume, so a gap between the two
// marks a freeze.
var (
	thaws         atomic.Int64
	thawWatchOnce sync.Once
)

func thawCount() int64 {
	thawWatchOnce.Do(func() { go watchThaws(thawProbeEvery) })
	return thaws.Load()
}

func watchThaws(every time.Duration) {
	last := time.Now()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for now := range tick.C {
		if froze(now.Sub(last), now.Round(0).Sub(last.Round(0))) {
			thaws.Add(1)
			log.Printf("acp: 检测到沙箱曾被冻结（墙上时间比单调时钟多走了 %s）", now.Round(0).Sub(last.Round(0))-now.Sub(last))
		}
		last = now
	}
}

// froze reports whether the wall clock ran ahead of the monotonic clock by
// more than thawGap over one probe.
func froze(monotonic, wall time.Duration) bool {
	return wall-monotonic > thawGap
}
