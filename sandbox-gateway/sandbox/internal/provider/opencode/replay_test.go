package opencode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/provider"
)

// replay feeds an event stream recorded from a real `opencode serve` run
// (testdata, DeepSeek) into a session with one turn. It stops once the turn
// settles and returns the remaining events.
func replay(t *testing.T, name string, posted int) (*session, *activeTurn, *recorder, [][]byte) {
	t.Helper()
	lines := readLines(t, name)
	s, turn, rec := replaySession(t, lines, posted)
	for i, line := range lines {
		s.onEvent(line)
		select {
		case <-turn.done:
			return s, turn, rec, lines[i+1:]
		default:
		}
	}
	t.Fatalf("%s: turn never settled", name)
	return nil, nil, nil, nil
}

func TestReplayToolTurn(t *testing.T) {
	s, turn, rec, rest := replay(t, "tool_turn.jsonl", 1)
	if turn.aborted || turn.errText != "" || hasTranscript(rest) {
		t.Fatalf("settled early or failed: %+v", turn)
	}
	if got := rec.text("agent_message_chunk"); got != "e2b.local" {
		t.Fatalf("reply %q", got)
	}
	if rec.text("agent_thought_chunk") == "" {
		t.Fatal("reasoning not streamed")
	}
	calls, updates := rec.updates("tool_call"), rec.updates("tool_call_update")
	if len(calls) != 1 || calls[0]["title"] != "bash" || len(updates) != 1 || updates[0]["status"] != "completed" {
		t.Fatalf("tool call: %v %v", calls, updates)
	}
	// The recorded step-finish tokens: output excludes reasoning.
	want := provider.TokenUsage{InputTokens: 2895 + 238, OutputTokens: 42 + 101 + 5, CacheReadTokens: 4480 + 7296}
	if turn.usage != want {
		t.Fatalf("usage %+v want %+v", turn.usage, want)
	}
	if len(s.openTools) != 0 {
		t.Fatalf("open tools %v", s.openTools)
	}
}

func TestReplaySteeredTurnEndsAfterTheSteerIsAnswered(t *testing.T) {
	_, turn, rec, rest := replay(t, "steer_turn.jsonl", 2)
	if hasTranscript(rest) || len(turn.userOrder) != 2 {
		t.Fatalf("settled early: users=%v", turn.userOrder)
	}
	if got := rec.text("agent_message_chunk"); got != `The command ran and printed "first" banana.` {
		t.Fatalf("reply %q", got)
	}
}

func TestReplaySteerNotYetPickedUpKeepsTheTurnOpen(t *testing.T) {
	lines := readLines(t, "tool_turn.jsonl")
	s, turn, _ := replaySession(t, lines, 2)
	for _, line := range lines {
		s.onEvent(line)
	}
	select {
	case <-turn.done:
		t.Fatal("turn settled with a posted steer still unanswered")
	default:
	}
}

func TestReplayAbortedTurn(t *testing.T) {
	s, turn, rec, rest := replay(t, "abort_turn.jsonl", 1)
	if !turn.aborted {
		t.Fatal("abort not seen")
	}
	if len(rest) == 0 {
		t.Fatal("recording lost the events the server sends after an abort")
	}
	s.endTurn()
	before := len(rec.snapshot())
	for _, line := range rest {
		s.onEvent(line)
	}
	if after := rec.snapshot(); len(after) != before {
		t.Fatalf("late events reached the transcript: %v", after[before:])
	}
	updates := rec.updates("tool_call_update")
	if len(updates) != 1 || updates[0]["status"] != "failed" {
		t.Fatalf("interrupted tool not closed: %v", updates)
	}
}

// hasTranscript reports whether events carry assistant output.
func hasTranscript(lines [][]byte) bool {
	for _, line := range lines {
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &ev) == nil && strings.HasPrefix(ev.Type, "message.part.") {
			return true
		}
	}
	return false
}

func readLines(t *testing.T, name string) [][]byte {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var lines [][]byte
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		lines = append(lines, append([]byte(nil), scanner.Bytes()...))
	}
	return lines
}

// replaySession is a session of the recording's first event with one turn
// into which posted prompts went.
func replaySession(t *testing.T, lines [][]byte, posted int) (*session, *activeTurn, *recorder) {
	t.Helper()
	var first struct {
		Properties struct {
			SessionID string `json:"sessionID"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s := newSession(nil, provider.OpenOptions{}, nil, rec.add, nil)
	s.id = first.Properties.SessionID
	s.turn = &activeTurn{done: make(chan struct{}), posted: posted, users: map[string]bool{}, answered: map[string]bool{}}
	return s, s.turn, rec
}
