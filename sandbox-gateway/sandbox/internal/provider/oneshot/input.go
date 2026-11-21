package oneshot

import (
	"errors"
	"io"
	"log"
	"os"
	"sync"

	"backend/internal/provider"
)

// TurnInputCodec is an optional Codec extension for CLIs that keep reading
// user messages from stdin while a turn runs and acknowledge each one on
// stdout once it entered the conversation (ParseResult.UserMessagesConsumed).
// The engine then keeps stdin open for the whole turn so Steer can add input,
// and closes it only after a result that left no message unconsumed.
type TurnInputCodec interface {
	StdinEncoder
	TurnInput() bool
}

func turnInputCodec(c Codec) (TurnInputCodec, bool) {
	tc, ok := c.(TurnInputCodec)
	return tc, ok && tc.TurnInput()
}

// turnInput is a running turn's stdin for a TurnInputCodec. writeMu orders
// the writes, so a steer never interleaves with the prompt envelope; mu
// guards the counts only, never a pipe write, so settle and close can always
// close stdin, which also ends a write the CLI stopped reading.
type turnInput struct {
	writeMu  sync.Mutex
	mu       sync.Mutex
	w        io.WriteCloser
	sent     int
	consumed int
	closed   bool
}

// openTurnInput writes the prompt in the background (see feedStdin) and holds
// the input until it is written, so a steer can never reach the CLI first.
// wait reports the prompt write.
func openTurnInput(w io.WriteCloser, prompt []byte) (t *turnInput, wait func() error) {
	t = &turnInput{w: w}
	t.writeMu.Lock()
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer t.writeMu.Unlock()
		err = t.send(prompt)
	}()
	return t, func() error { <-done; return err }
}

// write sends one user message. It fails with provider.ErrNoActiveTurn once
// the turn settled, so a late steer is never written to a finished CLI.
func (t *turnInput) write(msg []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.send(msg)
}

// send writes msg under writeMu. The message counts as sent from the start,
// so a result that arrives meanwhile still waits for its answer.
func (t *turnInput) send(msg []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return provider.ErrNoActiveTurn
	}
	t.sent++
	t.mu.Unlock()
	if _, err := t.w.Write(msg); err != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.sent--
		if t.closed {
			return provider.ErrNoActiveTurn
		}
		return err
	}
	return nil
}

func (t *turnInput) consume(n int) {
	t.mu.Lock()
	t.consumed += n
	t.mu.Unlock()
}

// settle runs on a reported result. When every sent message was consumed it
// closes stdin, which lets the CLI exit, and reports true. Otherwise the CLI
// answers the remaining input before it reports its next result. A CLI that
// acknowledged nothing at all does not acknowledge input; waiting for it
// would keep the turn open forever.
func (t *turnInput) settle() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return true
	}
	if t.consumed > 0 && t.consumed < t.sent {
		return false
	}
	t.closeLocked()
	return true
}

func (t *turnInput) close() {
	t.mu.Lock()
	t.closeLocked()
	t.mu.Unlock()
}

func (t *turnInput) closeLocked() {
	if t.closed {
		return
	}
	t.closed = true
	// exec.Cmd.Wait closes the pipe itself once the CLI exited.
	if err := t.w.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		log.Printf("oneshot: close stdin: %v", err)
	}
}

// Capabilities: every one-shot codec continues its conversation through a
// resume pointer; steering needs a codec that keeps the turn's stdin open.
func (e *engine) Capabilities() provider.Capabilities {
	_, steer := turnInputCodec(e.c)
	return provider.Capabilities{Steer: steer, Resume: true}
}

// Steer writes text to the running turn's stdin.
func (e *engine) Steer(text string) error {
	tc, ok := turnInputCodec(e.c)
	if !ok {
		return provider.ErrNoActiveTurn
	}
	in := e.input.Load()
	if in == nil {
		return provider.ErrNoActiveTurn
	}
	return in.write(tc.StdinBytes(text, nil))
}
