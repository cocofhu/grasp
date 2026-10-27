package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/acp"
	"backend/internal/provider"
	"backend/internal/provider/acpx"
)

type delayedExitSession struct {
	stubSess
	done        chan struct{}
	exitStarted chan struct{}
	exitRelease chan struct{}
	closed      chan struct{}
	closeOnce   sync.Once
}

func (s *delayedExitSession) Done() <-chan struct{} { return s.done }
func (s *delayedExitSession) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}
func (s *delayedExitSession) ExitInfo() (string, error) {
	close(s.exitStarted)
	<-s.exitRelease
	return "old Agent failed", nil
}

func awaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lifecycle transition")
	}
}

func TestAgentExitDetachesBeforeReapingAndDoesNotDisturbReconnect(t *testing.T) {
	old := &delayedExitSession{stubSess: stubSess{id: "old"}, done: make(chan struct{}), exitStarted: make(chan struct{}), exitRelease: make(chan struct{}), closed: make(chan struct{})}
	b := newTestBridge(old)
	oldCtx, oldCancel := context.WithCancel(context.Background())
	defer oldCancel()
	b.agentCtx, b.agentCancel = oldCtx, oldCancel
	b.evLog.append(json.RawMessage(`{"type":"prompt_begin","promptText":"failed turn"}`))
	b.userDoneBuf.appendDone(PromptQueueEntry{ID: "old-turn", Content: "failed turn"})
	c := dialBridge(t, b)
	finished := make(chan struct{})
	go func() { b.watchAgentConn(old); close(finished) }()
	close(old.done)
	awaitSignal(t, old.exitStarted)
	awaitSignal(t, oldCtx.Done())
	awaitSignal(t, old.closed)
	b.mu.Lock()
	retained := b.sess != nil
	b.mu.Unlock()
	if retained {
		t.Fatal("dead session was retained while ExitInfo blocked")
	}
	if len(b.evLog.snapshot()) != 1 || len(b.userDoneBuf.snapshotDone()) != 1 {
		t.Fatal("unexpected exit erased the conversation evidence")
	}
	if err := b.ChatWithOpID("must reject", "bad", "chat", nil); err == nil {
		t.Fatal("chat accepted an unavailable session")
	}
	newCtx, newCancel := context.WithCancel(context.Background())
	defer newCancel()
	newSession := &stubSess{id: "replacement"}
	b.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		b.mu.Lock()
		b.sess, b.agentCtx, b.agentCancel = newSession, newCtx, newCancel
		b.agentGeneration++
		b.mu.Unlock()
		return newSession, nil
	}
	if got, err := b.EnsureAgent("", "", nil, nil); err != nil || got != newSession {
		t.Fatalf("reconnect while old ExitInfo waits: %v, %v", got, err)
	}
	b.queueMu.Lock()
	b.promptQueue = []queuedPrompt{{Text: "replacement's queued turn", OpID: "new"}}
	b.queueMu.Unlock()
	close(old.exitRelease)
	awaitSignal(t, finished)
	select {
	case <-newCtx.Done():
		t.Fatal("old watcher cancelled the replacement's context")
	default:
	}
	if b.Session() != newSession {
		t.Fatal("old watcher detached the replacement")
	}
	if n, _, _ := b.PromptQueueInfo(); n != 1 {
		t.Fatal("old watcher cleared the replacement's queue")
	}
	b.Broadcast(map[string]any{"op": "test_complete"})
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var frame struct {
			Op          string `json:"op"`
			AgentExited bool   `json:"agentExited"`
		}
		if err := c.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.AgentExited {
			t.Fatal("old exit notification marked replacement offline")
		}
		if frame.Op == "test_complete" {
			break
		}
	}
}

func TestClosedSessionRejectedBeforeWatcherRuns(t *testing.T) {
	old := &delayedExitSession{done: make(chan struct{})}
	close(old.done)
	b := newTestBridge(old)
	if b.Session() != nil || b.info("").Connected {
		t.Fatal("closed session still advertised as connected")
	}
	if err := b.ChatWithOpID("hello", "closed", "chat", nil); err == nil {
		t.Fatal("closed session accepted another prompt")
	}
	if n, _, _ := b.PromptQueueInfo(); n != 0 {
		t.Fatal("closed session retained the rejected prompt")
	}
	replacement := &stubSess{id: "replacement"}
	b.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		b.mu.Lock()
		b.sess = replacement
		b.mu.Unlock()
		return replacement, nil
	}
	if got, err := b.EnsureAgent("", "", nil, nil); err != nil || got != replacement {
		t.Fatalf("EnsureAgent reused a dead session: %v, %v", got, err)
	}
}

type pumpExitSession struct {
	stubSess
	done        chan struct{}
	checking    chan struct{}
	resume      chan struct{}
	doneCalls   atomic.Int32
	promptCalls atomic.Int32
}

func (s *pumpExitSession) Done() <-chan struct{} {
	if s.doneCalls.Add(1) == 1 {
		close(s.checking) // pump has removed the item, but has not restored it
		<-s.resume
	}
	return s.done
}

func (s *pumpExitSession) Prompt(context.Context, string, []provider.PromptImage) (provider.TurnResult, error) {
	s.promptCalls.Add(1)
	return provider.TurnResult{StopReason: "end_turn"}, nil
}

func TestPumpDoesNotRestoreQueueAfterAgentExit(t *testing.T) {
	sess := &pumpExitSession{stubSess: stubSess{id: "failed"}, done: make(chan struct{}), checking: make(chan struct{}), resume: make(chan struct{})}
	close(sess.done)
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(sess.resume) }) }
	t.Cleanup(resume)
	b := newTestBridge(sess)
	b.promptQueue = []queuedPrompt{{Text: "must not be retried", OpID: "old"}}
	cleared := make(chan struct{})
	b.agentCancel = func() { close(cleared) } // watcher calls this after clearing
	pumpDone := make(chan struct{})
	go func() { b.pumpPromptQueue(); close(pumpDone) }()
	awaitSignal(t, sess.checking)
	watcherDone := make(chan struct{})
	go func() { b.watchAgentConn(sess); close(watcherDone) }()
	if b.enqueueMu.TryLock() {
		b.enqueueMu.Unlock()
		// Without serialization, force teardown to clear the queue before
		// the pump restores its item. This deterministically reproduced a
		// permanently queued message on an already disconnected session.
		awaitSignal(t, cleared)
	}
	resume()
	awaitSignal(t, pumpDone)
	awaitSignal(t, watcherDone)
	if n, _, _ := b.PromptQueueInfo(); n != 0 {
		t.Fatalf("pump restored %d messages after Agent exit cleanup", n)
	}
	if sess.promptCalls.Load() != 0 || b.Session() != nil {
		t.Fatal("failed session was reused or its prompt was retried")
	}
}

func TestBridgeACPHelperProcess(t *testing.T) {
	mode := os.Getenv("GRASP_BRIDGE_ACP_HELPER")
	if mode == "" {
		return
	}
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(s.Bytes(), &req)
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "test-acp"}}
		case "session/new":
			result = map[string]string{"sessionId": "helper-" + mode}
		case "session/prompt":
			if mode == "invalid" {
				_, _ = fmt.Fprintln(os.Stdout, "Corrupt protocol output")
				continue // remain alive with stdin open, like the failing CLI
			}
			result = map[string]string{"stopReason": "end_turn"}
		default:
			continue
		}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		_, _ = fmt.Fprintln(os.Stdout, string(body))
	}
	os.Exit(0)
}

func TestBridgeRecoversAfterACPProtocolFailure(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b := NewBridge()
	mode := "invalid"
	var watcherDone chan struct{}
	b.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		ctx, cancel := context.WithCancel(context.Background())
		p, err := acp.NewPanel(ctx, ctx, []string{exe, "-test.run=^TestBridgeACPHelperProcess$"}, append(os.Environ(), "GRASP_BRIDGE_ACP_HELPER="+mode), t.TempDir(), "", nil, func(ev json.RawMessage) {
			b.Broadcast(eventEnvelope(ev, b.touchActiveTurn(ev)))
		}, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		sess := acpx.Wrap(p)
		b.mu.Lock()
		b.sess, b.agentCtx, b.agentCancel = sess, ctx, cancel
		b.agentGeneration++
		b.mu.Unlock()
		done := make(chan struct{})
		watcherDone = done
		go func() { b.watchAgentConn(sess); close(done) }()
		t.Cleanup(func() { cancel(); _ = sess.Close(); awaitSignal(t, done) })
		return sess, nil
	}
	if _, err := b.EnsureAgent("", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.ChatWithOpID("first turn", "bad", "chat", nil); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, watcherDone)
	waitIdle(t, b)
	if b.Session() != nil || len(b.evLog.snapshot()) == 0 {
		t.Fatal("failed Agent was not detached, or its history was erased")
	}
	mode = "healthy"
	if _, err := b.EnsureAgent("", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.ChatWithOpID("retry explicitly", "good", "chat", nil); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, b)
	for _, raw := range b.evLog.snapshot() {
		var ev struct {
			Type       string `json:"type"`
			StopReason string `json:"stopReason"`
		}
		if json.Unmarshal(raw, &ev) == nil && ev.Type == "prompt_done" && ev.StopReason == "end_turn" {
			return
		}
	}
	t.Fatal("replacement Agent did not finish its prompt")
}
