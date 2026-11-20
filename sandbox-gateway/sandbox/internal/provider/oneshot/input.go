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

// turnInput is a running turn's stdin for a TurnInputCodec. Writes are
// serialized so a steer never interleaves with the prompt envelope.
type turnInput struct {
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
	t.mu.Lock()
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer t.mu.Unlock()
		err = t.writeLocked(prompt)
	}()
	return t, func() error { <-done; return err }
}

// write sends one user message. It fails with provider.ErrNoActiveTurn once
// the turn settled, so a late steer is never written to a finished CLI.
func (t *turnInput) write(msg []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writeLocked(msg)
}

func (t *turnInput) writeLocked(msg []byte) error {
	if t.closed {
		return provider.ErrNoActiveTurn
	}
	if _, err := t.w.Write(msg); err != nil {
		return err
	}
	t.sent++
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
