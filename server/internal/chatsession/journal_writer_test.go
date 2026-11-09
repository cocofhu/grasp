package chatsession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestJournalWriterFlushesAndFreezesAcceptedOutputBeforeRead(t *testing.T) {
	_, store, replica, scope := durableFixture(t)
	ctx := context.Background()
	intent := admitQueue(t, store, scope, "native output", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	writer := NewJournalWriter(ctx, func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		return store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, entries, nil)
	}, nil)
	for i := range 281 {
		entry := journalEntry(strings.Repeat("x", i%23))
		if err := writer.Add(ctx, entry); err != nil {
			t.Fatal(err)
		}
		// Callers may recycle their read buffer after Add returns.
		for j := range entry.Data {
			entry.Data[j] = '!'
		}
	}
	if err := writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := replica.JournalPage(ctx, scope, JournalQuery{After: -1, Limit: JournalPageEvents})
	if err != nil || len(page.Items) != 281 || page.HasMore {
		t.Fatalf("flush exposed partial output: count=%d err=%v", len(page.Items), err)
	}
	for _, entry := range page.Items {
		if !json.Valid(entry.Data) {
			t.Fatal("caller recycled uncommitted output")
		}
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatal("close is not idempotent", err)
	}
	if got := writer.Written(); got.First != 0 || got.Last != 280 || got.Count != 281 {
		t.Fatalf("wrong durable span: %+v", got)
	}
	if err := writer.Add(ctx, journalEntry("late")); !errors.Is(err, ErrJournalClosed) {
		t.Fatalf("closed output was accepted: %v", err)
	}
}

func TestJournalWriterStorageFailureIsStickyAndStopsProducer(t *testing.T) {
	failure := errors.New("object storage unavailable")
	var notified atomic.Int32
	writer := NewJournalWriter(context.Background(), func(context.Context, []JournalEntry) (JournalRange, error) { return JournalRange{}, failure }, func(err error) {
		if errors.Is(err, failure) {
			notified.Add(1)
		}
	})
	if err := writer.Add(context.Background(), journalEntry("unpublished")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("failed persistence was acknowledged: %v", err)
	}
	if err := writer.Close(context.Background()); !errors.Is(err, failure) {
		t.Fatal("close forgot failed output", err)
	}
	if got := writer.Written(); got.Count != 0 || notified.Load() != 1 {
		t.Fatalf("failed output was marked committed: %+v notices=%d", got, notified.Load())
	}
	if err := writer.Add(context.Background(), journalEntry("after failure")); !errors.Is(err, failure) {
		t.Fatalf("failed writer accepted more output: %v", err)
	}
}

func TestJournalWriterCancellationStillFlushesButOwnerLossDoesNot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var committed atomic.Int32
	writer := NewJournalWriter(ctx, func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		if ctx.Err() != nil {
			t.Error("cancelled native request prevented final durable flush")
		}
		first := committed.Add(int32(len(entries))) - int32(len(entries))
		return JournalRange{First: uint64(first), Last: uint64(first) + uint64(len(entries)) - 1, Count: len(entries)}, nil
	}, nil)
	if err := writer.Add(ctx, journalEntry("last output before Stop")); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := writer.Close(context.Background()); err != nil || committed.Load() != 1 {
		t.Fatalf("stop dropped final output: %v", err)
	}
	_, store, _, scope := durableFixture(t)
	intent := admitQueue(t, store, scope, "owned native work", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	ownerCtx, lose := context.WithCancelCause(context.Background())
	ownerCtx = executionContext(ownerCtx, Execution{Store: store, Lease: lease, TurnID: intent.Turn.ID})
	owned := NewJournalWriter(ownerCtx, func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		return store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, entries, nil)
	}, nil)
	lose(ErrQueueOwnerLost)
	if err := owned.Add(context.Background(), journalEntry("late native callback")); err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(context.Background()); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("lost owner published final buffered output: %v", err)
	}
	if head, err := store.JournalHead(context.Background(), scope); err != nil || head != -1 {
		t.Fatalf("late output became visible: %d %v", head, err)
	}
}

func TestJournalWriterBackpressureAndConcurrentCloseAreBounded(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var next uint64
	writer := NewJournalWriter(context.Background(), func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
			return JournalRange{}, ctx.Err()
		}
		first := next
		next += uint64(len(entries))
		return JournalRange{First: first, Last: next - 1, Count: len(entries)}, nil
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	large := journalEntry(strings.Repeat("x", 4<<20))
	if err := writer.Add(ctx, large); err != nil {
		t.Fatal(err)
	}
	<-entered
	var blocked error
	for range 12 {
		if err := writer.Add(ctx, large); err != nil {
			blocked = err
			break
		}
	}
	if !errors.Is(blocked, context.DeadlineExceeded) {
		t.Fatalf("slow storage did not backpressure producer: %v", blocked)
	}
	writer.mu.Lock()
	buffered := writer.buffered
	writer.mu.Unlock()
	if buffered > journalWriterBytes {
		t.Fatal("event buffer exceeded its byte bound")
	}
	close(release)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writer.Close(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestJournalWriterProbeTimeoutDoesNotAbortAcceptedOutput(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	writer := NewJournalWriter(context.Background(), func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
			return JournalRange{}, ctx.Err()
		}
		return JournalRange{First: 0, Last: uint64(len(entries)) - 1, Count: len(entries)}, nil
	}, nil)
	if err := writer.Add(context.Background(), journalEntry("retained while a reader disconnects")); err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	if err := writer.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("flush timeout ignored: %v", err)
	}
	cancel()
	if err := writer.Error(); err != nil {
		t.Fatal("reader's wait cancellation aborted accepted output", err)
	}
	close(release)
	if err := writer.Close(context.Background()); err != nil || writer.Written().Count != 1 {
		t.Fatalf("accepted output was not eventually confirmed: %v", err)
	}
}

func TestJournalWriterRejectsUnserializableAndOversizedOutputWithoutCommit(t *testing.T) {
	for _, entry := range []JournalEntry{{Type: FrameSession, Event: "status", Payload: map[string]any{"invalid": make(chan struct{})}}, journalEntry(strings.Repeat("x", JournalChunkBytes))} {
		var called atomic.Int32
		writer := NewJournalWriter(context.Background(), func(context.Context, []JournalEntry) (JournalRange, error) { called.Add(1); return JournalRange{}, nil }, nil)
		if err := writer.Add(context.Background(), entry); err == nil {
			t.Fatal("invalid output was buffered as successful")
		}
		if err := writer.Close(context.Background()); err == nil || called.Load() != 0 {
			t.Fatalf("invalid output reached commit: calls=%d err=%v", called.Load(), err)
		}
	}
}

func TestJournalWriterCommitPanicAndBadRangeRemainExplicitFailures(t *testing.T) {
	for _, commit := range []func(context.Context, []JournalEntry) (JournalRange, error){
		func(context.Context, []JournalEntry) (JournalRange, error) { panic("storage adapter panicked") },
		func(context.Context, []JournalEntry) (JournalRange, error) {
			return JournalRange{First: 1, Last: 1, Count: 0}, nil
		},
	} {
		writer := NewJournalWriter(context.Background(), commit, nil)
		if err := writer.Add(context.Background(), journalEntry("must not be claimed committed")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(context.Background()); err == nil || writer.Written().Count != 0 {
			t.Fatalf("broken adapter claimed output committed: %+v %v", writer.Written(), err)
		}
	}
}
