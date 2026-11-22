package pi

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/provider"
)

// replay feeds the events of a real `pi --mode rpc` run (testdata,
// DeepSeek; streaming snapshots trimmed) into a session with one turn into
// which posted inputs went.
func replay(t *testing.T, name string, posted int) (*session, *activeTurn, *recorder) {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	rec := &recorder{}
	s := newSession(provider.OpenOptions{}, nil, rec.add, nil)
	turn := &activeTurn{done: make(chan struct{}), posted: posted, usage: map[string]provider.TokenUsage{}}
	s.turn = turn
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		s.onEvent(append([]byte(nil), scanner.Bytes()...))
	}
	select {
	case <-turn.done:
	default:
		t.Fatalf("%s: turn never settled", name)
	}
	return s, turn, rec
}

func TestReplaySteeredRun(t *testing.T) {
	s, turn, rec := replay(t, "steer_run.jsonl", 2)
	if turn.users != 2 || turn.aborted || turn.errText != "" {
		t.Fatalf("turn %+v", turn)
	}
	if got := rec.text("agent_message_chunk"); got != `The command slept for four seconds and then printed "first" banana.` {
		t.Fatalf("reply %q", got)
	}
	if rec.text("agent_thought_chunk") == "" {
		t.Fatal("reasoning not streamed")
	}
	calls, updates := rec.updates("tool_call"), rec.updates("tool_call_update")
	if len(calls) != 1 || calls[0]["title"] != "bash" || len(updates) != 1 || updates[0]["status"] != "completed" {
		t.Fatalf("tool: %v %v", calls, updates)
	}
	if u := turn.usage["deepseek-v4-flash"]; u.InputTokens == 0 || u.OutputTokens == 0 {
		t.Fatalf("usage %+v", turn.usage)
	}
	if len(s.openTools) != 0 {
		t.Fatalf("open tools %v", s.openTools)
	}
}

func TestUnansweredSteerKeepsTheTurnOpen(t *testing.T) {
	s := newSession(provider.OpenOptions{}, nil, nil, nil)
	turn := &activeTurn{done: make(chan struct{}), posted: 2, inflight: 1, ended: true}
	s.turn = turn
	s.mu.Lock()
	s.evaluateLocked(turn)
	s.mu.Unlock()
	select {
	case <-turn.done:
		t.Fatal("settled while a steer was unanswered")
	default:
	}
}

func TestReplayAbortedRun(t *testing.T) {
	_, turn, rec := replay(t, "abort_run.jsonl", 1)
	if !turn.aborted {
		t.Fatal("abort not seen")
	}
	if u := rec.updates("tool_call_update"); len(u) != 1 {
		t.Fatalf("tool result: %v", u)
	}
}
