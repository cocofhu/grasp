package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"

	"github.com/gorilla/websocket"
)

// lifeAgent is a provider whose sessions resume by id, like a CLI that keeps
// its conversations on disk.
type lifeAgent struct {
	name      provider.Name
	transport provider.TransportKind
	noResume  bool // sessions do not report Capabilities.Resume
	forget    bool // a resume pointer is not found: a new conversation starts
	// hold, when set, blocks resuming opens until closed; failResume then
	// makes them fail.
	hold       chan struct{}
	failResume bool
	// failOpens fails that many opens first.
	failOpens int
	// config is what sessions report as provider.ConfigReader files.
	config       []string
	openedSignal chan struct{}

	mu    sync.Mutex
	opens []provider.OpenOptions
	sess  []*lifeSess
}

func (a *lifeAgent) Name() provider.Name {
	if a.name != "" {
		return a.name
	}
	return "life"
}
func (a *lifeAgent) Runtime() string                                      { return "life" }
func (a *lifeAgent) DefaultConfigRoot() string                            { return "" }
func (a *lifeAgent) Transport() provider.TransportKind                    { return a.transport }
func (a *lifeAgent) ListModels(context.Context) ([]provider.Model, error) { return nil, nil }
func (a *lifeAgent) AuthEnv(env []string) []string                        { return env }

func (a *lifeAgent) Open(_, _ context.Context, opts provider.OpenOptions, onEvent func(json.RawMessage), _ provider.PermissionChooser) (provider.Session, error) {
	if opts.ResumeSessionID != "" && a.hold != nil {
		<-a.hold
		if a.failResume {
			return nil, errors.New("agent failed to start")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failOpens > 0 {
		a.failOpens--
		return nil, errors.New("agent is not installed yet")
	}
	a.opens = append(a.opens, opts)
	if a.openedSignal != nil {
		select {
		case a.openedSignal <- struct{}{}:
		default:
		}
	}
	id := fmt.Sprintf("native-%d", len(a.opens))
	if opts.ResumeSessionID != "" && !a.forget {
		id = opts.ResumeSessionID
	}
	s := &lifeSess{stubSess: stubSess{id: id}, cwd: opts.Cwd, config: a.config, resume: !a.noResume, onEvent: onEvent,
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
	cwd     string
	config  []string
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
func (s *lifeSess) CWD() string           { return s.cwd }
func (s *lifeSess) ConfigFiles() []string { return s.config }
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
	if _, err := b.connect(openSpec{cwd: t.TempDir(), auto: &auto}); err != nil {
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

// takeTurn runs one chat turn on s and waits until the bridge settled it.
func takeTurn(t *testing.T, b *Bridge, s *lifeSess) {
	t.Helper()
	if err := b.ChatWithLimits("earlier", "turn-"+s.id, "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	awaitPrompt(t, s)
	close(s.release)
	for deadline := time.Now().Add(3 * time.Second); ; {
		b.mu.Lock()
		history := b.sessHistory
		b.mu.Unlock()
		b.turnMu.Lock()
		idle := b.activeTurn == nil
		b.turnMu.Unlock()
		if history && idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the turn never settled")
		}
		time.Sleep(5 * time.Millisecond)
	}
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

func TestStartDefaultAgentSkipsCursorPrewarm(t *testing.T) {
	opened := make(chan struct{}, 1)
	agent := &lifeAgent{name: provider.Cursor, transport: provider.LongLived, openedSignal: opened}
	b := NewBridge()
	b.agentProvider = agent

	b.StartDefaultAgent()

	select {
	case <-opened:
		t.Fatal("Cursor must wait for the first connect to receive MCP servers")
	case <-time.After(100 * time.Millisecond):
	}
	if opens, _ := agent.opened(); len(opens) != 0 {
		t.Fatalf("Cursor prewarm opened %d sessions", len(opens))
	}
}

func TestModelSwitchResumesTheNativeSession(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	b, first := lifeBridge(t, agent)
	takeTurn(t, b, first)
	visible := len(b.evLog.snapshot())

	sess, resumed, err := b.SwitchModelSettings("next-model", "", nil)
	if err != nil || !resumed || sess.SessionID() != first.id {
		t.Fatalf("switch = %v resumed=%v err=%v", sess, resumed, err)
	}
	opens, _ := agent.opened()
	if got := opens[1]; got.ResumeSessionID != first.id || got.Model != "next-model" {
		t.Fatalf("reopen options = %+v", got)
	}
	if visible == 0 || len(b.evLog.snapshot()) != visible {
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

func isReviving(b *Bridge) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reviving
}

func awaitReviving(t *testing.T, b *Bridge) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !isReviving(b) {
		if time.Now().After(deadline) {
			t.Fatal("the bridge never started reviving the agent")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPromptDuringRevivalWaitsForTheRevivedAgent(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived, hold: make(chan struct{})}
	b, first := lifeBridge(t, agent)
	first.die()
	awaitReviving(t, b)
	if err := b.ChatWithLimits("while reviving", "t1", "chat", nil, 0, 0); err != nil {
		t.Fatalf("a prompt during revival was rejected: %v", err)
	}
	close(agent.hold)
	_, sessions := awaitOpens(t, agent, 2)
	if got := awaitPrompt(t, sessions[1]); got != "while reviving" {
		t.Fatalf("revived agent ran %q", got)
	}
	if isReviving(b) {
		t.Fatal("still reviving after the agent came back")
	}
}

func TestFailedRevivalStopsAcceptingPrompts(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived, hold: make(chan struct{}), failResume: true}
	b, first := lifeBridge(t, agent)
	c := dialBridge(t, b)
	first.die()
	awaitReviving(t, b)
	close(agent.hold)
	readFrame(t, c, func(f map[string]any) bool { return f["agentExited"] == true })
	if isReviving(b) {
		t.Fatal("still reviving after the restart failed")
	}
	if err := b.ChatWithLimits("after", "t2", "chat", nil, 0, 0); err == nil {
		t.Fatal("a prompt was accepted with no agent")
	}
}

func TestNewContextAfterALostSessionDoesNotReportAReset(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived, forget: true}
	b, first := lifeBridge(t, agent)
	first.die()
	awaitOpens(t, agent, 2)
	if err := waitConnected(b); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RestartChatContext(); err != nil {
		t.Fatal(err)
	}
	if lost, _ := b.lostSession.Load().(string); lost != "" {
		t.Fatalf("a new context still reports the lost session %q", lost)
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
	t.Run("config", func(t *testing.T) {
		login := filepath.Join(t.TempDir(), "auth.json")
		if err := os.WriteFile(login, []byte(`{"v":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		agent := &lifeAgent{transport: provider.LongLived, config: []string{login}}
		b, first := lifeBridge(t, agent)
		if err := os.WriteFile(login, []byte(`{"v":22}`), 0o600); err != nil {
			t.Fatal(err)
		}
		runOnReplacement(t, b, agent, first)
	})
}

func TestFailedRecycleDropsTheQueueAndTellsTheClients(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived, hold: make(chan struct{}), failResume: true}
	close(agent.hold)
	b, _ := lifeBridge(t, agent)
	c := dialBridge(t, b)
	thaws.Add(1)
	if err := b.ChatWithLimits("after", "t1", "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	readFrame(t, c, func(f map[string]any) bool { return f["agentExited"] == true })
	if waiting, _, _ := b.PromptQueueInfo(); waiting != 0 {
		t.Fatalf("%d prompts stayed queued with no agent", waiting)
	}
}

func TestUnchangedConfigKeepsTheAgent(t *testing.T) {
	login := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(login, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := &lifeAgent{transport: provider.LongLived, config: []string{login, filepath.Join(t.TempDir(), "missing.toml")}}
	b, first := lifeBridge(t, agent)
	if err := b.ChatWithLimits("turn", "t1", "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	awaitPrompt(t, first)
	close(first.release)
	if opens, _ := agent.opened(); len(opens) != 1 {
		t.Fatal("an agent whose files did not change was recycled")
	}
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
