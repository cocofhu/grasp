package chatsession

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const journalWriterBytes = 2 * JournalChunkBytes
const journalWriterTarget = 256 << 10
const journalWriterFlushInterval = time.Second

var ErrJournalClosed = errors.New("conversation event writer is closed")

type journalCommand struct {
	entry   *JournalEntry
	bytes   int
	barrier chan error
	stop    bool
}

// JournalWriter batches native output before it becomes visible in the SQL
// journal. Add accepts only an in-memory buffer; Flush/Close confirm durable
// commit. Readers consume the journal, never this uncommitted buffer.
type JournalWriter struct {
	ctx          context.Context
	cancel       context.CancelFunc
	commit       func(context.Context, []JournalEntry) (JournalRange, error)
	onFailure    func(error)
	commands     chan journalCommand
	done         chan struct{}
	mu           sync.Mutex
	changed      chan struct{}
	closed       bool
	err          error
	buffered     int
	rangeWritten JournalRange
}

func NewJournalWriter(ctx context.Context, commit func(context.Context, []JournalEntry) (JournalRange, error), onFailure func(error)) *JournalWriter {
	// Cancellation of a native request still permits a bounded flush of its
	// final output. The execution context retains its owner-loss fence.
	persistence, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w := &JournalWriter{ctx: persistence, cancel: cancel, commit: commit, onFailure: onFailure, commands: make(chan journalCommand, JournalBatchEvents), done: make(chan struct{}), changed: make(chan struct{})}
	go w.run()
	return w
}

func (w *JournalWriter) signalLocked() { close(w.changed); w.changed = make(chan struct{}) }

func (w *JournalWriter) fail(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	first := w.err == nil
	if first {
		w.err = err
		w.closed = true
		w.signalLocked()
	}
	w.mu.Unlock()
	if first {
		w.cancel()
		if w.onFailure != nil {
			_ = guardTurn(func() { w.onFailure(err) })
		}
	}
}

func (w *JournalWriter) enqueue(ctx context.Context, cmd journalCommand) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		w.mu.Lock()
		if w.err != nil {
			err := w.err
			w.mu.Unlock()
			return err
		}
		if w.closed && !cmd.stop {
			w.mu.Unlock()
			return ErrJournalClosed
		}
		if w.buffered+cmd.bytes <= journalWriterBytes {
			select {
			case w.commands <- cmd:
				w.buffered += cmd.bytes
				w.mu.Unlock()
				return nil
			default:
			}
		}
		changed := w.changed
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.done:
			if err := w.Error(); err != nil {
				return err
			}
			return ErrJournalClosed
		case <-changed:
		}
	}
}

func (w *JournalWriter) Add(ctx context.Context, entry JournalEntry) error {
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	encoded, err := json.Marshal(entry)
	if err == nil && len(encoded) > JournalChunkBytes-4096 {
		err = errors.New("conversation event exceeds storage bound")
	}
	if err != nil {
		w.fail(err)
		return err
	}
	// Freeze nested maps and raw data while the caller still owns them.
	var frozen JournalEntry
	if err = json.Unmarshal(encoded, &frozen); err != nil {
		w.fail(err)
		return err
	}
	return w.enqueue(ctx, journalCommand{entry: &frozen, bytes: len(encoded)})
}

func (w *JournalWriter) Flush(ctx context.Context) error {
	barrier := make(chan error, 1)
	if err := w.enqueue(ctx, journalCommand{barrier: barrier}); err != nil {
		return err
	}
	select {
	case err := <-barrier:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return w.Error()
	}
}

func (w *JournalWriter) Close(ctx context.Context) error {
	w.mu.Lock()
	first := !w.closed
	w.closed = true
	w.signalLocked()
	w.mu.Unlock()
	if first {
		if err := w.enqueue(ctx, journalCommand{stop: true}); err != nil {
			w.fail(err)
			return err
		}
	}
	select {
	case <-w.done:
		return w.Error()
	case <-ctx.Done():
		w.fail(ctx.Err())
		return ctx.Err()
	}
}

func (w *JournalWriter) Error() error { w.mu.Lock(); defer w.mu.Unlock(); return w.err }
func (w *JournalWriter) Written() JournalRange {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rangeWritten
}

func (w *JournalWriter) run() {
	defer func() { w.cancel(); close(w.done) }()
	// Low-volume output becomes visible within about one second. Size/count
	// thresholds and explicit barriers still commit immediately; readers never
	// see the uncommitted in-memory suffix.
	ticker := time.NewTicker(journalWriterFlushInterval)
	defer ticker.Stop()
	pending := []JournalEntry{}
	bytes := 0
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
		var written JournalRange
		var commitErr error
		err := guardTurn(func() { written, commitErr = w.commit(ctx, pending) })
		if err == nil {
			err = commitErr
		}
		cancel()
		if err != nil {
			return err
		}
		if written.Count != len(pending) || written.Last < written.First || written.Last-written.First+1 != uint64(len(pending)) {
			return ErrJournalGap
		}
		w.mu.Lock()
		if w.rangeWritten.Count > 0 && written.First <= w.rangeWritten.Last {
			w.mu.Unlock()
			return ErrJournalGap
		}
		if w.rangeWritten.Count == 0 {
			w.rangeWritten.First = written.First
		}
		w.rangeWritten.Last = written.Last
		w.rangeWritten.Count += written.Count
		w.buffered -= bytes
		w.signalLocked()
		w.mu.Unlock()
		pending = nil
		bytes = 0
		return nil
	}
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if err := flush(); err != nil {
				w.fail(err)
				return
			}
		case cmd := <-w.commands:
			w.mu.Lock()
			w.signalLocked()
			w.mu.Unlock()
			if cmd.entry != nil {
				if len(pending) > 0 && bytes+cmd.bytes > journalWriterTarget {
					if err := flush(); err != nil {
						w.fail(err)
						return
					}
				}
				pending = append(pending, *cmd.entry)
				bytes += cmd.bytes
				if len(pending) >= JournalBatchEvents || bytes >= journalWriterTarget {
					if err := flush(); err != nil {
						w.fail(err)
						return
					}
				}
				continue
			}
			err := flush()
			if cmd.barrier != nil {
				cmd.barrier <- err
			}
			if err != nil {
				w.fail(err)
				return
			}
			if cmd.stop {
				return
			}
		}
	}
}
