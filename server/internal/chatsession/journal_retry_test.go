package chatsession

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/go-sql-driver/mysql"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

type journalHTTPFailure int

func (e journalHTTPFailure) Error() string       { return "storage response failed" }
func (e journalHTTPFailure) HTTPStatusCode() int { return int(e) }

func TestJournalRetryOnlyClassifiesExplicitTransientFailures(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, driver.ErrBadConn, sql.ErrConnDone, io.EOF, io.ErrUnexpectedEOF, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE, &net.DNSError{IsTimeout: true}, journalHTTPFailure(408), journalHTTPFailure(429), journalHTTPFailure(503), &mysql.MySQLError{Number: 1213}, sqlite3.Error{Code: sqlite3.ErrBusy}, sqlite3.Error{Code: sqlite3.ErrLocked}} {
		if !transientJournalError(failure) {
			t.Fatalf("transient error not retried: %T %v", failure, failure)
		}
	}
	for _, failure := range []error{context.Canceled, ErrQueueRetired, ErrJournalGap, errors.Join(ErrQueueOwnerLost, io.EOF), journalHTTPFailure(403), &mysql.MySQLError{Number: 1142}, sqlite3.Error{Code: sqlite3.ErrCorrupt}, errors.New("unknown error")} {
		if transientJournalError(failure) {
			t.Fatalf("permanent error retried: %T %v", failure, failure)
		}
	}
}

func TestJournalRetryAttemptLimitPreservesLastError(t *testing.T) {
	_, store, _, scope := durableFixture(t)
	var attempts int
	failure := journalHTTPFailure(503)
	_, err := store.AppendJournalWith(t.Context(), scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "retry-budget"}}, func(context.Context, PreparedJournal) (JournalRange, error) {
		attempts++
		return JournalRange{}, failure
	})
	if !errors.Is(err, failure) || attempts != journalTransientAttempts {
		t.Fatalf("wrong retry budget or fabricated result: attempts=%d err=%v", attempts, err)
	}
}

func TestJournalTransientPrepareAndLostCommitReplyKeepOneFrozenBatch(t *testing.T) {
	db, store, replica, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, replica)
	var uploads, commits atomic.Int32
	objects.beforePut = func(context.Context) error {
		if uploads.Add(1) < 3 {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	var first PreparedJournal
	entries := []JournalEntry{{Type: FrameSession, Event: "first", Payload: map[string]any{"text": "original frozen body"}}}
	var deadlines []time.Time
	written, err := store.AppendJournalWith(t.Context(), scope, nil, "", entries, func(ctx context.Context, prepared PreparedJournal) (JournalRange, error) {
		deadline, _ := ctx.Deadline()
		deadlines = append(deadlines, deadline)
		if commits.Add(1) == 1 {
			first = prepared
			entries[0].Payload["text"] = "mutated after freeze"
		} else if prepared.chunk.ID != first.chunk.ID || prepared.body.Ref() != first.body.Ref() || prepared.chunk.FirstSequence != first.chunk.FirstSequence {
			t.Fatal("uncertain COMMIT re-prepared the batch at another head")
		}
		var result JournalRange
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			result, err = store.AppendPreparedJournalTx(ctx, tx, prepared, nil)
			return err
		})
		if err == nil && commits.Load() == 1 {
			return result, io.EOF // SQL committed; its response was lost.
		}
		return result, err
	})
	if err != nil || written.Count != 1 || commits.Load() != 2 || uploads.Load() != 3 || !deadlines[0].Equal(deadlines[1]) {
		t.Fatalf("transient failure aborted/duplicated output: %+v commits=%d uploads=%d deadlines=%v err=%v", written, commits.Load(), uploads.Load(), deadlines, err)
	}
	var chunks int64
	if err := db.Model(&models.ChatEventChunk{}).Count(&chunks).Error; err != nil || chunks != 1 {
		t.Fatalf("uncertain commit duplicated its journal row: %d %v", chunks, err)
	}
	page, err := replica.JournalPage(t.Context(), scope, JournalQuery{After: -1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Payload["text"] != "original frozen body" {
		t.Fatalf("frozen output changed during retry: %+v %v", page, err)
	}
}

func TestJournalRetryHonorsCallerDeadlineAndRejectsPermanentErrors(t *testing.T) {
	_, store, _, scope := durableFixture(t)
	want := &mysql.MySQLError{Number: 1205, Message: "lock wait timed out"}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var calls int
	started := time.Now()
	_, err := store.AppendJournalWith(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "bounded"}}, func(attempt context.Context, _ PreparedJournal) (JournalRange, error) {
		calls++
		actual, _ := attempt.Deadline()
		if !actual.Equal(deadline) {
			t.Fatal("retry extended the original caller deadline")
		}
		return JournalRange{}, want
	})
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, want) || calls != 1 || time.Since(started) > time.Second {
		t.Fatalf("retry deadline/error was hidden: calls=%d err=%v", calls, err)
	}
	for _, failure := range []error{ErrQueueOwnerLost, ErrJournalGap, errors.New("invalid body"), &mysql.MySQLError{Number: 1142, Message: "denied"}} {
		calls = 0
		_, err = store.AppendJournalWith(t.Context(), scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "terminal"}}, func(context.Context, PreparedJournal) (JournalRange, error) { calls++; return JournalRange{}, failure })
		if !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("permanent error retried/concealed: calls=%d err=%v", calls, err)
		}
	}
	if head, err := store.JournalHead(t.Context(), scope); err != nil || head != -1 {
		t.Fatalf("failed retries reserved visible output: %d %v", head, err)
	}
}

func TestJournalCommittedReceiptStillRequiresOwnerAndFullFrozenIdentity(t *testing.T) {
	for _, mutation := range []string{"owner", "payload", "queue", "range", "epoch", "count", "bytes", "turn"} {
		t.Run(mutation, func(t *testing.T) {
			db, store, _, scope := durableFixture(t)
			intent := admitQueue(t, store, scope, "original work", "one")
			lease := acquireQueue(t, store, scope)
			if _, err := store.Claim(t.Context(), lease); err != nil {
				t.Fatal(err)
			}
			prepared, err := store.PrepareJournal(t.Context(), scope, &lease, intent.Turn.ID, []JournalEntry{journalEntry("native output")})
			if err != nil {
				t.Fatal(err)
			}
			commitPreparedJournal(t, store, prepared)
			if mutation == "owner" {
				if err := db.Table("chat_queues").Where("id = ?", scope.ID()).Update("owner", "new owner").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				field, value := map[string]string{"payload": "payload", "queue": "queue_id", "range": "last_sequence", "epoch": "execution_epoch", "count": "count", "bytes": "bytes", "turn": "turn_id"}[mutation], any("changed")
				if mutation == "range" || mutation == "epoch" || mutation == "count" || mutation == "bytes" {
					value = 999
				}
				if err := db.Table("chat_event_chunks").Where("id = ?", prepared.chunk.ID).Update(field, value).Error; err != nil {
					t.Fatal(err)
				}
			}
			err = db.Transaction(func(tx *gorm.DB) error {
				_, err := store.AppendPreparedJournalTx(t.Context(), tx, prepared, nil)
				return err
			})
			expected := ErrJournalGap
			if mutation == "owner" {
				expected = ErrQueueOwnerLost
			}
			if !errors.Is(err, expected) {
				t.Fatalf("unverified receipt acknowledged: %v", err)
			}
		})
	}
}

func TestJournalWriterRetryPreservesFIFOAndDoesNotStopNativeProducer(t *testing.T) {
	db, store, replica, scope := durableFixture(t)
	var attempts, failures atomic.Int32
	writer := NewJournalWriter(t.Context(), func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		return store.AppendJournalWith(ctx, scope, nil, "", entries, func(ctx context.Context, prepared PreparedJournal) (JournalRange, error) {
			if attempts.Add(1) == 1 {
				return JournalRange{}, &mysql.MySQLError{Number: 1213, Message: "deadlock"}
			}
			var written JournalRange
			err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var err error
				written, err = store.AppendPreparedJournalTx(ctx, tx, prepared, nil)
				return err
			})
			return written, err
		})
	}, func(error) { failures.Add(1) })
	for _, text := range []string{"one", "two", "three"} {
		if err := writer.Add(t.Context(), JournalEntry{Type: FrameSession, Event: "native", Payload: map[string]any{"text": text}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(t.Context()); err != nil || failures.Load() != 0 || attempts.Load() != 2 {
		t.Fatalf("transient outage stopped the producer: %v attempts=%d failures=%d", err, attempts.Load(), failures.Load())
	}
	page, err := replica.JournalPage(t.Context(), scope, JournalQuery{After: -1})
	if err != nil || len(page.Items) != 3 {
		t.Fatal(page, err)
	}
	var texts []string
	for _, entry := range page.Items {
		texts = append(texts, entry.Payload["text"].(string))
	}
	if strings.Join(texts, ",") != "one,two,three" {
		t.Fatal("retry reordered output", texts)
	}
}
