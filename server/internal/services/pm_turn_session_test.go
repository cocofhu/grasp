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

	"github.com/google/uuid"
)

type fakePmChat struct {
	mu      sync.Mutex
	frames  []json.RawMessage
	err     error
	release chan struct{}
	started chan uint
	prompts []string
	cancels []uint
}

func (f *fakePmChat) ChatWithTimeout(ctx context.Context, id uint, text string, _ []models.PromptImage, _ time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, text)
	frames, err, release, started := f.frames, f.err, f.release, f.started
	f.mu.Unlock()
	if started != nil {
		started <- id
	}
	for _, fr := range frames {
		onEvent(fr)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return nil, nil, err
}

func (f *fakePmChat) Cancel(id uint) {
	f.mu.Lock()
	f.cancels = append(f.cancels, id)
	f.mu.Unlock()
}

func pmChunk(text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"type": "session_update",
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": text},
		},
	})
	return raw
}

type pmTurnFixture struct {
	pm     *PmService
	runner *PmTurnRunner
	chat   *fakePmChat
	thread string
}

func newPmTurnFixture(t *testing.T) *pmTurnFixture {
	t.Helper()
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	p, err := NewProjectService(db).Create("TurnProj-"+uuid.NewString()[:8], "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	chat := &fakePmChat{}
	r := NewPmTurnRunner(pm, nil)
	r.SetChatterForTest(chat)
	return &pmTurnFixture{pm: pm, runner: r, chat: chat, thread: th.ID}
}

func (f *pmTurnFixture) userMsg(t *testing.T, text string) models.ChatMessage {
	t.Helper()
	m, err := f.pm.AppendMessage(f.thread, "user", text, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// collect reads frames until stop matches one (inclusive).
func collect(t *testing.T, ch <-chan PmTurnEvent, stop func(PmTurnEvent) bool) []PmTurnEvent {
	t.Helper()
	var out []PmTurnEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed after %d frames", len(out))
			}
			out = append(out, ev)
			if stop(ev) {
				return out
			}
		case <-timeout:
			t.Fatalf("timed out after %d frames: %+v", len(out), out)
		}
	}
}

func isEvent(name string) func(PmTurnEvent) bool {
	return func(ev PmTurnEvent) bool { return ev.Type == "session" && ev.Event == name }
}

func events(evs []PmTurnEvent) []string {
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		if ev.Type == "acp" {
			out = append(out, "acp")
			continue
		}
		out = append(out, ev.Event)
	}
	return out
}

func waitIdle(t *testing.T, r *PmTurnRunner, threadID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.Active(threadID) {
		if time.Now().After(deadline) {
			t.Fatal("turn still active")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPmTurnRunsAndPersistsReply(t *testing.T) {
	f := newPmTurnFixture(t)
	f.chat.frames = []json.RawMessage{pmChunk("进度"), pmChunk("正常")}
	ch, unsub, _ := f.runner.Subscribe(f.thread, -1)
	defer unsub()
	u := f.userMsg(t, "进度如何")

	if err := f.runner.Start(f.thread, u.ID, 7, "进度如何", nil); err != nil {
		t.Fatal(err)
	}
	got := collect(t, ch, isEvent(chatsession.EventTurnDone))
	want := []string{"queue_state", "queue_state", "queue_state", "turn_begin", "acp", "acp", "turn_done"}
	if len(got) != len(want) {
		t.Fatalf("frames=%v want %v", events(got), want)
	}
	if busy, _ := got[0].Payload["busy"].(bool); busy {
		t.Fatal("first snapshot must be idle")
	}
	begin := got[3]
	if begin.Payload["userMsgId"] != u.ID {
		t.Fatalf("turn_begin=%v", begin.Payload)
	}
	if got[6].Payload["userMsgId"] != u.ID || got[6].Payload["interrupted"] != false {
		t.Fatalf("turn_done=%v", got[6].Payload)
	}
	frame := got[4].Frame()
	if frame["type"] != "acp" || frame["data"] == nil {
		t.Fatalf("acp frame=%v", frame)
	}

	waitIdle(t, f.runner, f.thread)
	msgs, _ := f.pm.ListMessages(f.thread)
	if len(msgs) != 2 || msgs[1].Role != "assistant" || msgs[1].Content != "进度正常" {
		t.Fatalf("messages=%+v", msgs)
	}
	if d, _ := f.pm.GetDraft(f.thread); d != nil {
		t.Fatalf("draft must be cleared, got %+v", d)
	}
	if active, mid, partial, _, _ := f.runner.Status(f.thread); active || mid != u.ID || partial != "进度正常" {
		t.Fatalf("status active=%v mid=%q partial=%q", active, mid, partial)
	}
}

func TestPmTurnQueuesBehindRunningTurn(t *testing.T) {
	f := newPmTurnFixture(t)
	f.chat.release = make(chan struct{})
	f.chat.started = make(chan uint, 2)
	f.chat.frames = []json.RawMessage{pmChunk("好")}
	a, b := f.userMsg(t, "一"), f.userMsg(t, "二")

	if err := f.runner.Start(f.thread, a.ID, 1, "一", nil); err != nil {
		t.Fatal(err)
	}
	<-f.chat.started
	if _, err := f.runner.Enqueue(f.thread, PmTurnRequest{UserMsgID: a.ID, SandboxID: 1, Prompt: "一"}); !errors.Is(err, errPmTurnDuplicate) {
		t.Fatalf("duplicate active err=%v", err)
	}
	waiting, err := f.runner.Enqueue(f.thread, PmTurnRequest{UserMsgID: b.ID, SandboxID: 1, Prompt: "二"})
	if err != nil || waiting != 1 {
		t.Fatalf("enqueue waiting=%d err=%v", waiting, err)
	}
	if _, err := f.runner.Enqueue(f.thread, PmTurnRequest{UserMsgID: b.ID, SandboxID: 1, Prompt: "二"}); !errors.Is(err, errPmTurnDuplicate) {
		t.Fatalf("duplicate pending err=%v", err)
	}
	close(f.chat.release)
	<-f.chat.started
	waitIdle(t, f.runner, f.thread)
	msgs, _ := f.pm.ListMessages(f.thread)
	if len(msgs) != 4 {
		t.Fatalf("want two replies, messages=%d", len(msgs))
	}
}

func TestPmTurnReconnectReplaysActiveTurn(t *testing.T) {
	f := newPmTurnFixture(t)
	f.chat.release = make(chan struct{})
	f.chat.started = make(chan uint, 1)
	f.chat.frames = []json.RawMessage{pmChunk("半句")}
	u := f.userMsg(t, "问")
	if err := f.runner.Start(f.thread, u.ID, 3, "问", nil); err != nil {
		t.Fatal(err)
	}
	<-f.chat.started

	ch, unsub, _ := f.runner.Subscribe(f.thread, -1)
	got := collect(t, ch, func(ev PmTurnEvent) bool { return ev.Type == "acp" })
	if e := events(got); len(e) != 3 || e[0] != "queue_state" || e[1] != "turn_begin" {
		t.Fatalf("replay=%v", e)
	}
	snap := got[0].Payload
	if snap["busy"] != true || snap["phase"] != PmPhaseRunning || snap["userMsgId"] != u.ID {
		t.Fatalf("snapshot=%v", snap)
	}
	unsub()
	unsub()
	close(f.chat.release)
	waitIdle(t, f.runner, f.thread)
}

func TestPmTurnCancelStopsAndDropsQueue(t *testing.T) {
	f := newPmTurnFixture(t)
	f.chat.release = make(chan struct{})
	f.chat.started = make(chan uint, 1)
	a, b := f.userMsg(t, "一"), f.userMsg(t, "二")
	ch, unsub, _ := f.runner.Subscribe(f.thread, -1)
	defer unsub()
	if err := f.runner.Start(f.thread, a.ID, 5, "一", nil); err != nil {
		t.Fatal(err)
	}
	<-f.chat.started
	if err := f.runner.Start(f.thread, b.ID, 5, "二", nil); err != nil {
		t.Fatal(err)
	}
	f.runner.Cancel(f.thread)
	got := collect(t, ch, isEvent(chatsession.EventTurnDone))
	done := got[len(got)-1].Payload
	if done["interrupted"] != true || done["failKind"] != PmFailStopped || done["userMsgId"] != a.ID {
		t.Fatalf("turn_done=%v", done)
	}
	waitIdle(t, f.runner, f.thread)
	for _, id := range []string{a.ID, b.ID} {
		if m, _ := f.pm.GetMessage(f.thread, id); m.Status != "failed" || m.FailKind != PmFailStopped {
			t.Fatalf("msg %s=%+v want failed/stopped", id, m)
		}
	}
	f.chat.mu.Lock()
	cancels := append([]uint(nil), f.chat.cancels...)
	f.chat.mu.Unlock()
	if len(cancels) == 0 || cancels[0] != 5 {
		t.Fatalf("sandbox cancel=%v", cancels)
	}
}

func TestPmTurnPrepareReportsPhaseAndFailure(t *testing.T) {
	f := newPmTurnFixture(t)
	ch, unsub, _ := f.runner.Subscribe(f.thread, -1)
	defer unsub()
	u := f.userMsg(t, "问")
	_, err := f.runner.Enqueue(f.thread, PmTurnRequest{
		UserMsgID: u.ID,
		Text:      "问",
		Prepare: func(_ context.Context, setPhase func(string)) (uint, string, error) {
			setPhase("pulling")
			setPhase("pulling")
			return 0, "", errors.New("image pull failed")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collect(t, ch, isEvent(chatsession.EventError))
	var phases []any
	for _, ev := range got {
		if ev.Event == PmEventPhase {
			phases = append(phases, ev.Payload["phase"])
		}
		if ev.Event == chatsession.EventQueueState && ev.Payload["busy"] == true && ev.Payload["phase"] != nil && ev.Payload["phase"] != PmPhasePreparing {
			t.Fatalf("busy queue_state phase=%v", ev.Payload["phase"])
		}
	}
	if len(phases) != 1 || phases[0] != "pulling" {
		t.Fatalf("phases=%v", phases)
	}
	last := got[len(got)-1].Payload
	if last["failKind"] != PmFailSandbox || last["message"] != "image pull failed" {
		t.Fatalf("error=%v", last)
	}
	waitIdle(t, f.runner, f.thread)
	if m, _ := f.pm.GetMessage(f.thread, u.ID); m.FailKind != PmFailSandbox {
		t.Fatalf("msg=%+v", m)
	}
}

func TestPmTurnPrepareBuildsPrompt(t *testing.T) {
	f := newPmTurnFixture(t)
	f.chat.frames = []json.RawMessage{pmChunk("答")}
	u := f.userMsg(t, "问")
	_, err := f.runner.Enqueue(f.thread, PmTurnRequest{
		UserMsgID: u.ID,
		Prepare: func(context.Context, func(string)) (uint, string, error) {
			return 9, "带上下文的问", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, f.runner, f.thread)
	f.chat.mu.Lock()
	defer f.chat.mu.Unlock()
	if len(f.chat.prompts) != 1 || f.chat.prompts[0] != "带上下文的问" {
		t.Fatalf("prompts=%v", f.chat.prompts)
	}
}

func TestPmTurnFailureKinds(t *testing.T) {
	cases := []struct {
		name   string
		frames []json.RawMessage
		err    error
		want   string
	}{
		{name: "empty", want: PmFailEmpty},
		{name: "chat error", err: errors.New("acp closed"), want: PmFailUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPmTurnFixture(t)
			f.chat.frames, f.chat.err = tc.frames, tc.err
			ch, unsub, _ := f.runner.Subscribe(f.thread, -1)
			defer unsub()
			u := f.userMsg(t, "问")
			if err := f.runner.Start(f.thread, u.ID, 2, "问", nil); err != nil {
				t.Fatal(err)
			}
			got := collect(t, ch, isEvent(chatsession.EventError))
			if kind := got[len(got)-1].Payload["failKind"]; kind != tc.want {
				t.Fatalf("failKind=%v want %s", kind, tc.want)
			}
			waitIdle(t, f.runner, f.thread)
			if m, _ := f.pm.GetMessage(f.thread, u.ID); m.Status != "failed" || m.FailKind != tc.want {
				t.Fatalf("msg=%+v", m)
			}
		})
	}
}

func TestPmTurnRunnerUnavailable(t *testing.T) {
	r := NewPmTurnRunner(nil, nil)
	if err := r.Start("t", "m", 1, "p", nil); err == nil {
		t.Fatal("expected unavailable error")
	}
	if r.Active("t") {
		t.Fatal("unknown thread must not be active")
	}
	r.Cancel("t")
	if active, _, _, _, seq := r.Status("t"); active || seq != -1 {
		t.Fatalf("status active=%v seq=%d", active, seq)
	}
}

func TestPmTurnThreadDroppedWhenIdle(t *testing.T) {
	f := newPmTurnFixture(t)
	f.chat.frames = []json.RawMessage{pmChunk("答")}
	ch, unsub, _ := f.runner.Subscribe(f.thread, -1)
	<-ch
	u := f.userMsg(t, "问")
	if err := f.runner.Start(f.thread, u.ID, 1, "问", nil); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, f.runner, f.thread)
	if f.runner.lookup(f.thread) == nil {
		t.Fatal("thread with a subscriber must stay registered")
	}
	unsub()
	deadline := time.Now().Add(5 * time.Second)
	for f.runner.lookup(f.thread) != nil {
		if time.Now().After(deadline) {
			t.Fatal("idle thread without subscribers must be dropped")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
