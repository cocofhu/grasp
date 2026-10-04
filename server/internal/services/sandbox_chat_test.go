package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
)

type fakeSandboxChat struct {
	mu       sync.Mutex
	prompts  []string
	cancels  int
	release  chan struct{}
	started  chan string
	failWith error
}

func newFakeSandboxChat() *fakeSandboxChat {
	return &fakeSandboxChat{release: make(chan struct{}), started: make(chan string, 8)}
}

func (f *fakeSandboxChat) Chat(ctx context.Context, _ uint, text string, _ []models.PromptImage, onEvent func(json.RawMessage)) error {
	f.mu.Lock()
	f.prompts = append(f.prompts, text)
	f.mu.Unlock()
	f.started <- text
	onEvent(json.RawMessage(`{"chunk":"` + text + `"}`))
	select {
	case <-f.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return f.failWith
}

func (f *fakeSandboxChat) Cancel(uint) {
	f.mu.Lock()
	f.cancels++
	f.mu.Unlock()
}

func waitStarted(t *testing.T, f *fakeSandboxChat, want string) {
	t.Helper()
	select {
	case got := <-f.started:
		if got != want {
			t.Fatalf("started %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("turn %q never started", want)
	}
}

func nextEvent(t *testing.T, ch <-chan chatsession.Event, match func(chatsession.Event) bool) chatsession.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if match(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("expected frame never arrived")
		}
	}
}

func sessionEvent(name string) func(chatsession.Event) bool {
	return func(ev chatsession.Event) bool { return ev.Type == chatsession.FrameSession && ev.Event == name }
}

func TestSandboxChatsQueueAndReplay(t *testing.T) {
	f := newFakeSandboxChat()
	c := NewSandboxChats(nil)
	c.SetChatterForTest(f)

	if _, err := c.Enqueue(7, SandboxChatItem{}); err == nil {
		t.Fatal("empty message should be rejected")
	}
	if _, err := c.Enqueue(7, SandboxChatItem{Content: "one"}); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, f, "one")
	if n, err := c.Enqueue(7, SandboxChatItem{Content: "two"}); err != nil || n != 1 {
		t.Fatalf("second enqueue = %d %v", n, err)
	}
	if !c.Active(7) {
		t.Fatal("sandbox should be active")
	}

	// A viewer joining mid-turn sees the queue and the running turn.
	ch, unsub := c.Subscribe(7, -1)
	head := <-ch
	if head.Event != chatsession.EventQueueState || head.Payload["busy"] != true || head.Payload["waiting"] != 1 {
		t.Fatalf("snapshot: %#v", head)
	}
	if active, _ := head.Payload["activeItem"].(map[string]any); active["text"] != "one" {
		t.Fatalf("active item: %#v", head.Payload["activeItem"])
	}
	begin := nextEvent(t, ch, sessionEvent(chatsession.EventTurnBegin))
	if item, _ := begin.Payload["item"].(map[string]any); item["text"] != "one" || item["id"] == "" {
		t.Fatalf("replayed turn_begin: %#v", begin.Payload)
	}
	if acp := nextEvent(t, ch, func(ev chatsession.Event) bool { return ev.Type == chatsession.FrameAcp }); string(acp.Data) != `{"chunk":"one"}` {
		t.Fatalf("replayed acp: %s", acp.Data)
	}
	unsub()

	f.release <- struct{}{}
	waitStarted(t, f, "two")
	f.failWith = errors.New("boom")
	ch, unsub = c.Subscribe(7, -1)
	defer unsub()
	f.release <- struct{}{}
	if ev := nextEvent(t, ch, sessionEvent(chatsession.EventError)); ev.Payload["message"] != "boom" {
		t.Fatalf("error frame: %#v", ev.Payload)
	}
}

func TestSandboxChatsCancelClearsQueue(t *testing.T) {
	f := newFakeSandboxChat()
	c := NewSandboxChats(nil)
	c.SetChatterForTest(f)

	c.Cancel(9)
	if f.cancels != 1 {
		t.Fatalf("cancel without a session should still stop the runtime, got %d", f.cancels)
	}

	ch, unsub := c.Subscribe(9, -1)
	defer unsub()
	<-ch
	_, _ = c.Enqueue(9, SandboxChatItem{Content: "a"})
	waitStarted(t, f, "a")
	_, _ = c.Enqueue(9, SandboxChatItem{Content: "b"})
	c.Cancel(9)
	done := nextEvent(t, ch, sessionEvent(chatsession.EventTurnDone))
	if done.Payload["interrupted"] != true {
		t.Fatalf("cancelled turn should be interrupted: %#v", done.Payload)
	}
	deadline := time.Now().Add(2 * time.Second)
	for c.Active(9) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.Active(9) {
		t.Fatal("queue should be empty after cancel")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.prompts) != 1 || f.cancels != 2 {
		t.Fatalf("prompts=%v cancels=%d", f.prompts, f.cancels)
	}
}

func TestSandboxChatsUnavailableAndIdleDrop(t *testing.T) {
	if _, err := NewSandboxChats(nil).Enqueue(1, SandboxChatItem{Content: "x"}); err == nil {
		t.Fatal("expected unavailable error")
	}
	NewSandboxChats(nil).Cancel(1)

	f := newFakeSandboxChat()
	close(f.release)
	c := NewSandboxChats(nil)
	c.SetChatterForTest(f)
	_, _ = c.Enqueue(3, SandboxChatItem{Content: "x"})
	waitStarted(t, f, "x")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		n := len(c.chats)
		c.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("idle sandbox chat should be dropped")
}
