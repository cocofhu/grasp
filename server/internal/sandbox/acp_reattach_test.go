package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func rawFrames(t *testing.T, frames ...string) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, len(frames))
	for i, f := range frames {
		out[i] = json.RawMessage(f)
	}
	return out
}

const (
	beginA = `{"type":"prompt_begin","opId":"a"}`
	msgA   = `{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"first"}}}`
	doneA  = `{"type":"prompt_done","opId":"a"}`
	beginB = `{"op":"event","data":{"type":"prompt_begin","opId":"b"}}`
	msgB   = `{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"nudge"}}}`
)

func TestFirstTurnFrames(t *testing.T) {
	if _, ok := FirstTurnFrames(nil); ok {
		t.Fatal("empty log has no turn")
	}
	if _, ok := FirstTurnFrames(rawFrames(t, msgA)); ok {
		t.Fatal("log without prompt_begin has no turn")
	}
	got, ok := FirstTurnFrames(rawFrames(t, `{"type":"available_commands"}`, beginA, msgA, doneA, beginB, msgB))
	if !ok || len(got) != 3 || string(got[0]) != beginA || string(got[2]) != doneA {
		t.Fatalf("first turn = %s ok=%v", got, ok)
	}
}

func TestReplayTurn(t *testing.T) {
	c := &ACPClient{}
	res, err := c.ReplayTurn(rawFrames(t, beginA, msgA, doneA))
	if err != nil || res.Narration != "first" {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	if _, err := c.ReplayTurn(rawFrames(t, beginA, msgA)); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("missing prompt_done: err = %v, want ErrConnClosed", err)
	}

	_, err = c.ReplayTurn(rawFrames(t, beginA, msgA, `{"type":"prompt_done","stopReason":"stuck"}`))
	if !errors.Is(err, ErrAgentStuck) || !strings.Contains(err.Error(), "已被终止") {
		t.Fatalf("stuck: err = %v", err)
	}

	if _, err := c.ReplayTurn(rawFrames(t, beginA, `{"type":"prompt_done","stopReason":"timeout"}`)); !errors.Is(err, ErrSandboxTurnTimeout) {
		t.Fatalf("empty timeout: err = %v", err)
	}
	res, err = c.ReplayTurn(rawFrames(t, beginA, msgA, `{"type":"prompt_done","stopReason":"timeout"}`))
	if err != nil || res.Narration != "first" {
		t.Fatalf("timeout with content keeps the result: res=%+v err=%v", res, err)
	}
}

func TestWaitBridgeKnownAndAwaitTurnWithoutConnection(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1)
	close(c.done)
	if _, ok := c.WaitBridgeKnown(context.Background(), time.Second); ok {
		t.Fatal("closed connection must not report a known bridge")
	}
	if err := c.AwaitTurn(context.Background(), "a"); !errors.Is(err, ErrConnClosed) {
		t.Fatalf("err = %v, want ErrConnClosed", err)
	}
}
