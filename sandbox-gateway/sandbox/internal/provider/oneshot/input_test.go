package oneshot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"
)

// inputFake reads one user message per stdin line, like a stream-json CLI
// with --input-format stream-json: it answers each message with its own
// result and exits on EOF. acks controls whether it confirms each message.
type inputFake struct {
	baseFake
	acks bool
}

func (inputFake) PromptViaStdin() bool { return true }
func (inputFake) TurnInput() bool      { return true }
func (inputFake) StdinBytes(prompt string, _ []provider.PromptImage) []byte {
	return []byte("msg:" + prompt + "\n")
}

func (f inputFake) Args(provider.OpenOptions, string, string) []string {
	ack := ""
	if f.acks {
		ack = "echo ack;"
	}
	return []string{"sh", "-c", `while IFS= read -r line; do ` + ack + ` echo "text:${line#msg:}";
case "$line" in *slow*) sleep 0.3;; esac; echo done; done`}
}

func (inputFake) ParseLine(line []byte) ParseResult {
	s := string(line)
	switch {
	case s == "ack":
		return ParseResult{UserMessagesConsumed: 1}
	case strings.HasPrefix(s, "text:"):
		return ParseResult{Msgs: []Msg{{Kind: KindText, Text: strings.TrimPrefix(s, "text:") + ";"}}}
	case s == "done":
		return ParseResult{StopReason: "end_turn"}
	}
	return ParseResult{}
}

// stdinOnlyFake takes its prompt on stdin but cannot take more input.
type stdinOnlyFake struct{ inputFake }

func (stdinOnlyFake) TurnInput() bool { return false }

func openSteerable(t *testing.T, c Codec) (provider.Session, func() string) {
	t.Helper()
	var mu sync.Mutex
	var text strings.Builder
	sess, err := NewProvider(c).Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir()},
		func(b json.RawMessage) {
			var m map[string]any
			if json.Unmarshal(b, &m) != nil {
				return
			}
			if u, ok := m["update"].(map[string]any); ok && u["sessionUpdate"] == "agent_message_chunk" {
				mu.Lock()
				text.WriteString(u["content"].(map[string]any)["text"].(string))
				mu.Unlock()
			}
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess, func() string { mu.Lock(); defer mu.Unlock(); return text.String() }
}

// steerDuring starts a turn and steers it once its stdin is live.
func steerDuring(t *testing.T, sess provider.Session, prompt, steer string) (provider.TurnResult, error) {
	t.Helper()
	e := sess.(*engine)
	type outcome struct {
		res provider.TurnResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := sess.Prompt(context.Background(), prompt, nil)
		done <- outcome{res, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for e.input.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("turn input never opened")
		}
		time.Sleep(time.Millisecond)
	}
	if err := sess.(provider.Steerer).Steer(steer); err != nil {
		t.Fatalf("steer: %v", err)
	}
	select {
	case o := <-done:
		return o.res, o.err
	case <-time.After(10 * time.Second):
		t.Fatal("steered turn did not finish")
	}
	return provider.TurnResult{}, nil
}

func TestSteerJoinsRunningTurn(t *testing.T) {
	sess, text := openSteerable(t, inputFake{acks: true})
	if !provider.CanSteer(sess) {
		t.Fatal("a turn-input codec must be steerable")
	}
	if err := sess.(provider.Steerer).Steer("early"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("steer before a turn: %v", err)
	}
	res, err := steerDuring(t, sess, "slow", "more")
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("turn = %+v %v", res, err)
	}
	if got := text(); got != "slow;more;" {
		t.Fatalf("the steered message must be answered within the turn: %q", got)
	}
	if err := sess.(provider.Steerer).Steer("late"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("steer after the turn: %v", err)
	}
	if res, err := sess.Prompt(context.Background(), "next", nil); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("next turn = %+v %v", res, err)
	}
}

func TestUnacknowledgedInputDoesNotHoldTheTurn(t *testing.T) {
	sess, text := openSteerable(t, inputFake{acks: false})
	res, err := steerDuring(t, sess, "slow", "more")
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("turn = %+v %v", res, err)
	}
	if !strings.HasPrefix(text(), "slow;") {
		t.Fatalf("text = %q", text())
	}
}

func TestStdinPromptWithoutTurnInputIsNotSteerable(t *testing.T) {
	sess, _ := openSteerable(t, stdinOnlyFake{})
	if provider.CanSteer(sess) {
		t.Fatal("stdin prompt alone does not make a codec steerable")
	}
	if err := sess.(provider.Steerer).Steer("x"); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("steer = %v", err)
	}
	if res, err := sess.Prompt(context.Background(), "hi", nil); err != nil || res.StopReason != "end_turn" {
		t.Fatalf("turn = %+v %v", res, err)
	}
}

type failingWriter struct{ closed int }

func (*failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
func (w *failingWriter) Close() error            { w.closed++; return errors.New("already closed") }

func TestTurnInputSettle(t *testing.T) {
	w := &failingWriter{}
	in, wait := openTurnInput(w, []byte("prompt"))
	if err := wait(); err == nil || in.sent != 0 {
		t.Fatalf("a failed prompt write is not sent: %v", err)
	}
	if err := in.write([]byte("x")); err == nil || in.sent != 0 {
		t.Fatalf("a failed write is not sent: %v sent=%d", err, in.sent)
	}
	in.sent = 2
	in.consume(1)
	if in.settle() {
		t.Fatal("a result with input still pending must not settle")
	}
	in.consume(1)
	if !in.settle() || !in.settle() || w.closed != 1 {
		t.Fatalf("settle closes stdin exactly once (closed=%d)", w.closed)
	}
	in.close()
	if w.closed != 1 {
		t.Fatal("close after settle is a no-op")
	}
	if err := in.write([]byte("x")); !errors.Is(err, provider.ErrNoActiveTurn) {
		t.Fatalf("write after settle: %v", err)
	}
}
