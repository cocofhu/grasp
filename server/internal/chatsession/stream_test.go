package chatsession

import (
	"encoding/json"
	"testing"
	"time"
)

func drain(t *testing.T, ch <-chan Event, n int) []Event {
	t.Helper()
	out := make([]Event, 0, n)
	for len(out) < n {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d of %d frames", len(out), n)
			}
			out = append(out, ev)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d of %d frames", len(out), n)
		}
	}
	return out
}

func TestEventFrame(t *testing.T) {
	acp := Event{Seq: 3, Type: FrameAcp, Data: json.RawMessage(`{"x":1}`)}.Frame()
	if acp["type"] != FrameAcp || acp["seq"] != 3 || string(acp["data"].(json.RawMessage)) != `{"x":1}` {
		t.Fatalf("acp frame: %#v", acp)
	}
	ctl := Event{Seq: 4, Type: FrameSession, Event: EventTurnDone, Payload: map[string]any{"interrupted": true, "event": "spoof"}}.Frame()
	if ctl["event"] != EventTurnDone || ctl["interrupted"] != true || ctl["seq"] != 4 {
		t.Fatalf("session frame: %#v", ctl)
	}
}

func TestStreamSnapshotReplayAndLive(t *testing.T) {
	s := NewStream(0)
	if s.LastSeq() != -1 {
		t.Fatalf("fresh LastSeq = %d", s.LastSeq())
	}
	s.Session(EventQueueState, map[string]any{"busy": false}, KeepNone)
	s.Session(EventTurnBegin, map[string]any{}, KeepBegin)
	s.Acp(json.RawMessage(`{"a":1}`))
	s.Acp(json.RawMessage(`{"a":2}`))

	ch, unsub := s.Subscribe(func() (map[string]any, bool) { return map[string]any{"busy": true}, true }, -1)
	got := drain(t, ch, 4)
	if got[0].Event != EventQueueState || got[0].Seq != 3 || got[0].Payload["busy"] != true {
		t.Fatalf("snapshot: %#v", got[0])
	}
	if got[1].Event != EventTurnBegin || got[2].Seq != 2 || got[3].Seq != 3 {
		t.Fatalf("replay: %#v", got[1:])
	}

	s.Session(EventTurnDone, map[string]any{"interrupted": false}, KeepEnd)
	if live := drain(t, ch, 1)[0]; live.Event != EventTurnDone || live.Seq != 4 {
		t.Fatalf("live: %#v", live)
	}
	unsub()
	unsub()
	if _, ok := <-ch; ok {
		t.Fatal("channel should close on unsubscribe")
	}
	s.Acp(json.RawMessage(`{}`))

	idle, unsubIdle := s.Subscribe(func() (map[string]any, bool) { return map[string]any{}, false }, -1)
	defer unsubIdle()
	if first := drain(t, idle, 1)[0]; first.Event != EventQueueState {
		t.Fatalf("idle snapshot: %#v", first)
	}
	select {
	case ev := <-idle:
		t.Fatalf("idle stream replayed %#v", ev)
	default:
	}
}

func TestStreamReplayAfterSeqAndCap(t *testing.T) {
	s := NewStream(3)
	s.Session(EventTurnBegin, map[string]any{}, KeepBegin)
	for i := 0; i < 5; i++ {
		s.Acp(json.RawMessage(`{}`))
	}
	ch, unsub := s.Subscribe(func() (map[string]any, bool) { return map[string]any{}, true }, 4)
	defer unsub()
	got := drain(t, ch, 2)
	if got[1].Seq != 5 {
		t.Fatalf("replay after seq 4 = %#v", got)
	}

	ch2, unsub2 := s.Subscribe(func() (map[string]any, bool) { return map[string]any{}, true }, -1)
	defer unsub2()
	got = drain(t, ch2, 4)
	if got[1].Event != EventTurnBegin || got[2].Seq != 4 || got[3].Seq != 5 {
		t.Fatalf("capped replay keeps turn_begin: %#v", got)
	}
}

func TestStreamSlowSubscriberDoesNotBlockForever(t *testing.T) {
	s := NewStream(1)
	ch, unsub := s.Subscribe(func() (map[string]any, bool) { return map[string]any{}, false }, -1)
	defer unsub()
	for i := 0; i < cap(ch); i++ {
		s.Session(EventQueueState, map[string]any{}, KeepNone)
	}
	done := make(chan struct{})
	go func() {
		s.Session(EventQueueState, map[string]any{}, KeepNone)
		close(done)
	}()
	<-ch
	select {
	case <-done:
	case <-time.After(subscriberGrace + time.Second):
		t.Fatal("emit stayed blocked after the subscriber drained")
	}
}
