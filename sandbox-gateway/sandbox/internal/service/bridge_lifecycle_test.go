package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"

	"github.com/gorilla/websocket"
)

// lifeAgent is a provider whose sessions resume by id, like a CLI that keeps
// its conversations on disk.
type lifeAgent struct {
	transport provider.TransportKind
	noResume  bool // sessions do not report Capabilities.Resume
	forget    bool // a resume pointer is not found: a new conversation starts

	mu    sync.Mutex
	opens []provider.OpenOptions
	sess  []*lifeSess
}

func (a *lifeAgent) Name() provider.Name                                  { return "life" }
func (a *lifeAgent) Runtime() string                                      { return "life" }
func (a *lifeAgent) DefaultConfigRoot() string                            { return "" }
func (a *lifeAgent) Transport() provider.TransportKind                    { return a.transport }
func (a *lifeAgent) ListModels(context.Context) ([]provider.Model, error) { return nil, nil }
func (a *lifeAgent) AuthEnv(env []string) []string                        { return env }

func (a *lifeAgent) Open(_, _ context.Context, opts provider.OpenOptions, onEvent func(json.RawMessage), _ provider.PermissionChooser) (provider.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.opens = append(a.opens, opts)
	id := fmt.Sprintf("native-%d", len(a.opens))
	if opts.ResumeSessionID != "" && !a.forget {
		id = opts.ResumeSessionID
	}
	s := &lifeSess{stubSess: stubSess{id: id}, resume: !a.noResume, onEvent: onEvent,
		done: make(chan struct{}), started: make(chan string, 8), release: make(chan struct{})}
	a.sess = append(a.sess, s)
	return s, nil
}

func (a *lifeAgent) opened() ([]provider.OpenOptions, []*lifeSess) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]provider.OpenOptions(nil), a.opens...), append([]*lifeSess(nil), a.sess...)
}

// lifeSess runs a turn until released or until its process "dies".
type lifeSess struct {
	stubSess
	resume  bool
	onEvent func(json.RawMessage)

	done      chan struct{}
	closeOnce sync.Once
	started   chan string
	release   chan struct{}
}

func (s *lifeSess) Capabilities() provider.Capabilities {
	return provider.Capabilities{Resume: s.resume}
}
func (s *lifeSess) Done() <-chan struct{} { return s.done }
func (s *lifeSess) Close() error          { s.die(); return nil }
func (s *lifeSess) die()                  { s.closeOnce.Do(func() { close(s.done) }) }

func (s *lifeSess) Prompt(ctx context.Context, text string, _ []provider.PromptImage) (provider.TurnResult, error) {
	s.started <- text
	select {
	case <-s.release:
		s.onEvent(json.RawMessage(`{"type":"prompt_done","stopReason":"end_turn"}`))
		return provider.TurnResult{StopReason: "end_turn"}, nil
	case <-s.done:
		return provider.TurnResult{StopReason: "failed"}, errors.New("agent exited during the turn")
	case <-ctx.Done():
		return provider.TurnResult{StopReason: "cancelled"}, ctx.Err()
	}
}

func lifeBridge(t *testing.T, agent *lifeAgent) (*Bridge, *lifeSess) {
	t.Helper()
	b := NewBridge()
	b.agentProvider = agent
	b.turnIdle, b.turnMax = 0, 0
	auto := true
	if _, err := b.Connect(t.TempDir(), "", nil, &auto); err != nil {
		t.Fatal(err)
	}
	_, sessions := agent.opened()
	t.Cleanup(func() {
		for _, s := range sessions {
			s.die()
		}
	})
	return b, sessions[0]
}

func awaitPrompt(t *testing.T, s *lifeSess) string {
	t.Helper()
	select {
	case text := <-s.started:
		return text
	case <-time.After(3 * time.Second):
		t.Fatalf("session %s never ran a turn", s.id)
	}
	return ""
}

func awaitOpens(t *testing.T, agent *lifeAgent, n int) ([]provider.OpenOptions, []*lifeSess) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		opens, sessions := agent.opened()
		if len(opens) >= n {
			return opens, sessions
		}
		if time.Now().After(deadline) {
			t.Fatalf("opened %d sessions, want %d", len(opens), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestModelSwitchResumesTheNativeSession(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	b, first := lifeBridge(t, agent)
	b.evLog.append(json.RawMessage(`{"type":"prompt_begin","promptText":"earlier"}`))

	sess, resumed, err := b.SwitchModelSettings("next-model", "", nil)
	if err != nil || !resumed || sess.SessionID() != first.id {
		t.Fatalf("switch = %v resumed=%v err=%v", sess, resumed, err)
	}
	opens, _ := agent.opened()
	if got := opens[1]; got.ResumeSessionID != first.id || got.Model != "next-model" {
		t.Fatalf("reopen options = %+v", got)
	}
	if len(b.evLog.snapshot()) != 1 {
		t.Fatal("a model switch must keep the visible conversation")
	}
	select {
	case <-first.done:
	default:
		t.Fatal("the replaced process was not closed")
	}
}

func TestModelSwitchWithoutResumeStartsANewConversation(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived, noResume: true}
	b, _ := lifeBridge(t, agent)
	if _, resumed, err := b.SwitchModelSettings("next-model", "", nil); err != nil || resumed {
		t.Fatalf("resumed=%v err=%v", resumed, err)
	}
	if opens, _ := agent.opened(); opens[1].ResumeSessionID != "" {
		t.Fatalf("an agent that cannot resume got pointer %q", opens[1].ResumeSessionID)
	}
}

func TestRestartAgentAndNewContextNeverResume(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	b, _ := lifeBridge(t, agent)
	if _, err := b.RestartChatContext(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RestartAgent(); err != nil {
		t.Fatal(err)
	}
	opens, _ := agent.opened()
	for _, o := range opens[1:] {
		if o.ResumeSessionID != "" {
			t.Fatalf("a new conversation resumed %q", o.ResumeSessionID)
		}
	}
}

func TestAgentExitRevivesOnTheSameSessionAndKeepsTheQueue(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	b, first := lifeBridge(t, agent)
	c := dialBridge(t, b)
	if err := b.ChatWithLimits("running", "t1", "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	awaitPrompt(t, first)
	if err := b.ChatWithLimits("queued", "t2", "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	first.die()

	opens, sessions := awaitOpens(t, agent, 2)
	if opens[1].ResumeSessionID != first.id {
		t.Fatalf("revival resumed %q, want %q", opens[1].ResumeSessionID, first.id)
	}
	if got := awaitPrompt(t, sessions[1]); got != "queued" {
		t.Fatalf("revived session ran %q, want the queued prompt", got)
	}
	close(sessions[1].release)
	readFrame(t, c, func(f map[string]any) bool { return f["op"] == "connected" && f["sessionId"] == first.id })
}

func TestRevivalThatLosesTheSessionReportsSessionReset(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived, forget: true}
	b, first := lifeBridge(t, agent)
	c := dialBridge(t, b)
	first.die()
	_, sessions := awaitOpens(t, agent, 2)
	if err := waitConnected(b); err != nil {
		t.Fatal(err)
	}
	if err := b.ChatWithLimits("next", "t1", "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	awaitPrompt(t, sessions[1])
	close(sessions[1].release)
	done := readFrame(t, c, func(f map[string]any) bool {
		data, _ := f["data"].(map[string]any)
		return data["type"] == "prompt_done"
	})["data"].(map[string]any)
	if done["sessionReset"] != true || done["previousSessionId"] != first.id {
		t.Fatalf("prompt_done after a lost session = %v", done)
	}
	if lost, _ := b.lostSession.Load().(string); lost != "" {
		t.Fatalf("session reset reported more than once: %q", lost)
	}
}

func TestAgentWithoutResumeIsNotRevived(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived, noResume: true}
	b, first := lifeBridge(t, agent)
	c := dialBridge(t, b)
	first.die()
	readFrame(t, c, func(f map[string]any) bool { return f["agentExited"] == true })
	if opens, _ := agent.opened(); len(opens) != 1 {
		t.Fatalf("an agent that cannot resume was restarted %d times", len(opens)-1)
	}
}

func TestRevivalBudget(t *testing.T) {
	var r revivalBudget
	now := time.Now()
	for i := range revivalLimit {
		if !r.allow(now.Add(time.Duration(i) * time.Second)) {
			t.Fatalf("revival %d refused", i+1)
		}
	}
	if r.allow(now.Add(time.Minute)) {
		t.Fatal("revivals past the limit within the window")
	}
	if !r.allow(now.Add(revivalWindow + time.Minute)) {
		t.Fatal("the window never reopened")
	}
}

func TestStaleAgentIsRecycledBeforeTheNextTurn(t *testing.T) {
	t.Run("thaw", func(t *testing.T) {
		agent := &lifeAgent{transport: provider.LongLived}
		b, first := lifeBridge(t, agent)
		thaws.Add(1)
		runOnReplacement(t, b, agent, first)
	})
	t.Run("age", func(t *testing.T) {
		agent := &lifeAgent{transport: provider.LongLived}
		b, first := lifeBridge(t, agent)
		b.maxAgentAge = time.Minute
		b.mu.Lock()
		b.sessOpened = time.Now().Add(-2 * time.Minute)
		b.mu.Unlock()
		runOnReplacement(t, b, agent, first)
	})
}

func runOnReplacement(t *testing.T, b *Bridge, agent *lifeAgent, first *lifeSess) {
	t.Helper()
	if err := b.ChatWithLimits("after", "t1", "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	opens, sessions := awaitOpens(t, agent, 2)
	if opens[1].ResumeSessionID != first.id {
		t.Fatalf("recycle resumed %q, want %q", opens[1].ResumeSessionID, first.id)
	}
	if got := awaitPrompt(t, sessions[1]); got != "after" {
		t.Fatalf("turn ran %q on the replacement", got)
	}
	select {
	case <-first.started:
		t.Fatal("the stale process ran the turn")
	default:
	}
	close(sessions[1].release)
}

func TestFreshAgentsAndOneShotAgentsAreNotRecycled(t *testing.T) {
	for _, agent := range []*lifeAgent{{transport: provider.OneShot}, {transport: provider.LongLived, noResume: true}} {
		b, first := lifeBridge(t, agent)
		thaws.Add(1)
		if err := b.ChatWithLimits("turn", "t1", "chat", nil, 0, 0); err != nil {
			t.Fatal(err)
		}
		awaitPrompt(t, first)
		close(first.release)
		if opens, _ := agent.opened(); len(opens) != 1 {
			t.Fatalf("transport %v noResume=%v was recycled", agent.transport, agent.noResume)
		}
	}
}

func TestFroze(t *testing.T) {
	if froze(5*time.Second, 6*time.Second) {
		t.Fatal("clock adjustment counted as a freeze")
	}
	if !froze(5*time.Second, 10*time.Minute) {
		t.Fatal("a ten minute gap is a freeze")
	}
}

// readFrame reads frames until match returns true.
func readFrame(t *testing.T, c *websocket.Conn, match func(map[string]any) bool) map[string]any {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var f map[string]any
		if err := c.ReadJSON(&f); err != nil {
			t.Fatalf("read: %v", err)
		}
		if match(f) {
			return f
		}
	}
}

func waitConnected(b *Bridge) error {
	deadline := time.Now().Add(3 * time.Second)
	for b.Session() == nil {
		if time.Now().After(deadline) {
			return errors.New("bridge never reconnected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}
