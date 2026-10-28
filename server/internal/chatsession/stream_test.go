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
	for i := len(ch); i < cap(ch); i++ {
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

func terminalFrameCases() []struct {
	name  string
	emit  func(*Stream)
	check func(*testing.T, Event)
} {
	return []struct {
		name  string
		emit  func(*Stream)
		check func(*testing.T, Event)
	}{
		{
			name: "acp",
			emit: func(s *Stream) { s.Acp(json.RawMessage(`{"terminal":true}`)) },
			check: func(t *testing.T, ev Event) {
				t.Helper()
				if ev.Type != FrameAcp || string(ev.Data) != `{"terminal":true}` {
					t.Fatalf("terminal ACP frame = %#v", ev)
				}
			},
		},
		{
			name: EventTurnDone,
			emit: func(s *Stream) { s.Session(EventTurnDone, map[string]any{}, KeepEnd) },
			check: func(t *testing.T, ev Event) {
				t.Helper()
				if ev.Type != FrameSession || ev.Event != EventTurnDone {
					t.Fatalf("terminal frame = %#v", ev)
				}
			},
		},
		{
			name: EventError,
			emit: func(s *Stream) { s.Session(EventError, map[string]any{}, KeepEnd) },
			check: func(t *testing.T, ev Event) {
				t.Helper()
				if ev.Type != FrameSession || ev.Event != EventError {
					t.Fatalf("terminal frame = %#v", ev)
				}
			},
		},
	}
}

func setSubscriberGrace(t *testing.T, s *Stream, grace time.Duration) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.subs) != 1 {
		t.Fatalf("subscriber count = %d, want 1", len(s.subs))
	}
	for sub := range s.subs {
		sub.mu.Lock()
		sub.grace = grace
		sub.mu.Unlock()
	}
}

func fillSubscriber(t *testing.T, s *Stream, ch <-chan Event) {
	t.Helper()
	// 订阅快照已占一个槽位，剩余控制帧会把真实订阅 channel 填满。
	for i := len(ch); i < cap(ch); i++ {
		s.Session(EventQueueState, map[string]any{}, KeepNone)
	}
	if len(ch) != cap(ch) {
		t.Fatalf("subscriber buffer length = %d, capacity = %d", len(ch), cap(ch))
	}
}

func requireSubscriberOpen(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed after successful delivery")
		}
		t.Fatalf("unexpected frame after successful delivery: %#v", ev)
	default:
	}
}

func requireSubscriberClosed(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("subscriber channel has unexpected frame: %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel stayed open after an undeliverable frame")
	}
}

func TestStreamTerminalFrameDelivery(t *testing.T) {
	for _, tc := range terminalFrameCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStream(1)
			ch, unsub := s.Subscribe(func() (map[string]any, bool) { return map[string]any{}, false }, -1)
			drain(t, ch, 1)
			tc.emit(s)
			tc.check(t, drain(t, ch, 1)[0])
			requireSubscriberOpen(t, ch)
			unsub()
			unsub()
			requireSubscriberClosed(t, ch)
		})
	}
}

func TestStreamSlowSubscriberDeliversAfterDrain(t *testing.T) {
	for _, tc := range terminalFrameCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStream(1)
			ch, unsub := s.Subscribe(func() (map[string]any, bool) { return map[string]any{}, false }, -1)
			setSubscriberGrace(t, s, 100*time.Millisecond)
			defer unsub()
			fillSubscriber(t, s, ch)

			done := make(chan struct{})
			go func() {
				tc.emit(s)
				close(done)
			}()
			select {
			case <-done:
				t.Fatal("emit completed while the subscriber channel was full")
			case <-time.After(10 * time.Millisecond):
			}
			<-ch
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("emit stayed blocked after the subscriber drained")
			}

			got := drain(t, ch, cap(ch))
			tc.check(t, got[len(got)-1])
			requireSubscriberOpen(t, ch)
		})
	}
}

func TestStreamSlowSubscriberClosesOnUndeliverableFrame(t *testing.T) {
	for _, tc := range terminalFrameCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStream(1)
			ch, unsub := s.Subscribe(func() (map[string]any, bool) { return map[string]any{}, false }, -1)
			setSubscriberGrace(t, s, 20*time.Millisecond)
			fillSubscriber(t, s, ch)

			done := make(chan struct{})
			go func() {
				tc.emit(s)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("emit did not finish within the subscriber grace period")
			}

			drain(t, ch, cap(ch))
			requireSubscriberClosed(t, ch)
			unsub()
			unsub()
			tc.emit(s)
		})
	}
}
