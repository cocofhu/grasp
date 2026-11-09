package chatsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func commitPreparedJournal(t *testing.T, store *DurableStore, prepared PreparedJournal) JournalRange {
	t.Helper()
	var result JournalRange
	if err := store.db.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = store.AppendPreparedJournalTx(context.Background(), tx, prepared, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestJournalBlockedPublicationReleasesQueueAndSQLConnection(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	ctx := context.Background()
	ready, err := store.PrepareJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "ready"}})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once, unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.beforePut = func(ctx context.Context) error {
		var err error
		once.Do(func() {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				err = ctx.Err()
			}
		})
		return err
	}
	done := make(chan error, 1)
	go func() {
		_, err := store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "slow"}}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("body preparation did not start")
	}
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if head, err := store.JournalHead(probe, scope); err != nil || head != -1 {
		t.Fatalf("uncommitted object advanced the journal or held SQL: %d %v", head, err)
	}
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error { return store.GuardIdle(tx, scope) }); err != nil {
		t.Fatalf("upload held the queue mutation lock: %v", err)
	}
	// A previously prepared body can commit while a different PUT holds the
	// filestore publication mutex; SQL proof checks must never wait on it.
	var first JournalRange
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error {
		var err error
		first, err = store.AppendPreparedJournalTx(probe, tx, ready, nil)
		return err
	}); err != nil || first.First != 0 {
		t.Fatalf("prepared append waited for an unrelated PUT: %+v %v", first, err)
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("head conflict did not retry after upload released")
	}
	if objects.puts.Load() != 3 || objects.opens.Load() != 0 {
		t.Fatalf("unexpected object I/O during CAS commit: puts=%d gets=%d", objects.puts.Load(), objects.opens.Load())
	}
	page, err := remote.JournalPage(ctx, scope, JournalQuery{After: -1})
	if err != nil || len(page.Items) != 2 || page.Head != 1 || page.Items[0].Event != "ready" || page.Items[1].Event != "slow" {
		t.Fatalf("failed CAS reserved a sequence or lost the retry: %+v %v", page, err)
	}
}

func TestJournalWaitingForAnotherUploadHonorsItsEarlierDeadline(t *testing.T) {
	_, store, _, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store)
	started, release := make(chan struct{}), make(chan struct{})
	var unblock, notify sync.Once
	var calls atomic.Int32
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.beforePut = func(ctx context.Context) error {
		if calls.Add(1) > 2 {
			return nil
		}
		notify.Do(func() { close(started) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	first, cancelFirst := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelFirst()
	done := make(chan error, 1)
	go func() {
		_, err := store.AppendJournal(first, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "first-upload"}}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first upload never started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	begin := time.Now()
	if _, err := store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "expired-waiter"}}, nil); !errors.Is(err, context.DeadlineExceeded) || time.Since(begin) > time.Second {
		t.Fatalf("journal deadline waited for another caller's PUT: %s %v", time.Since(begin), err)
	}
	if objects.puts.Load() != 2 {
		t.Fatal("independent callers did not reach object storage concurrently")
	}
	if head, err := store.JournalHead(context.Background(), scope); err != nil || head != -1 {
		t.Fatalf("uncommitted uploads reserved a sequence: %d %v", head, err)
	}
	// A third upload can publish without waiting for the slow first caller.
	parallel, parallelCancel := context.WithTimeout(context.Background(), time.Second)
	defer parallelCancel()
	if span, err := store.AppendJournal(parallel, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "parallel-upload"}}, nil); err != nil || span.First != 0 {
		t.Fatalf("slow upload blocked independent Journal publication: %+v %v", span, err)
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first upload did not finish after its gate released")
	}
	if head, err := store.JournalHead(context.Background(), scope); err != nil || head != 1 {
		t.Fatalf("timed-out waiter reserved or published a sequence: %d %v", head, err)
	}
}

func TestJournalCASRollsBackOuterTransactionAndFreezesRetryBytes(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	queuePayloadBackend(t, store, remote)
	ctx := context.Background()
	initial := time.Date(2026, 11, 9, 1, 2, 3, 4, time.UTC)
	store.now = func() time.Time { return initial }
	if err := db.Create(&models.Setting{Key: "journal-outer-cas", Value: "original"}).Error; err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{Type: FrameSession, Event: "frozen", Payload: map[string]any{"nested": map[string]any{"text": "original"}, "large": uint64(18446744073709551615)}}
	attempts := 0
	written, err := store.AppendJournalWith(ctx, scope, nil, "", []JournalEntry{entry}, func(ctx context.Context, prepared PreparedJournal) (JournalRange, error) {
		attempts++
		if attempts == 1 {
			if _, err := remote.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "competing"}}, nil); err != nil {
				return JournalRange{}, err
			}
			entry.Payload["nested"].(map[string]any)["text"] = "mutated after acceptance"
			store.now = func() time.Time { return initial.Add(time.Hour) }
		} else {
			var setting models.Setting
			if err := db.WithContext(ctx).First(&setting, "key = ?", "journal-outer-cas").Error; err != nil || setting.Value != "original" {
				t.Fatalf("retry began before the outer TX rolled back: %+v %v", setting, err)
			}
		}
		var span JournalRange
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.Setting{}).Where("key = ?", "journal-outer-cas").Update("value", "committed").Error; err != nil {
				return err
			}
			var err error
			span, err = store.AppendPreparedJournalTx(ctx, tx, prepared, nil)
			return err
		})
		return span, err
	})
	if err != nil || attempts != 2 || written.First != 1 || written.Last != 1 {
		t.Fatalf("head CAS did not retry one whole publication: %+v attempts=%d %v", written, attempts, err)
	}
	var chunk JournalChunkRecord
	if err := db.Table("chat_event_chunks").Where("queue_id = ? AND first_sequence = ?", scope.ID(), 1).Take(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	body, err := files.LoadBounded(ctx, chunk.Payload, filestore.Bytes, JournalChunkBytes)
	if err != nil || !bytes.Contains(body, []byte(`"large":18446744073709551615`)) || bytes.Contains(body, []byte("mutated")) {
		t.Fatalf("CAS retry changed frozen payload bytes: %s %v", body, err)
	}
	entries, err := validateJournalChunk(chunk.logical(body))
	if err != nil || !entries[0].At.Equal(initial) {
		t.Fatalf("CAS retry assigned a new event time: %+v %v", entries, err)
	}
	page, err := remote.JournalPage(ctx, scope, JournalQuery{After: -1})
	if err != nil || len(page.Items) != 2 || page.Items[0].Seq != 0 || page.Items[1].Seq != 1 {
		t.Fatalf("CAS introduced an event hole: %+v %v", page, err)
	}
}

func TestJournalPreparationRejectsExpiredOrSupersededExecution(t *testing.T) {
	for _, mode := range []string{"expired", "generation", "owner-context"} {
		t.Run(mode, func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			objects := queuePayloadBackend(t, store, remote)
			intent := admitQueue(t, store, scope, "native", "owned")
			lease := acquireQueue(t, store, scope)
			if _, err := store.Claim(context.Background(), lease); err != nil {
				t.Fatal(err)
			}
			ctx, lose := context.WithCancelCause(context.Background())
			defer lose(nil)
			ctx = executionContext(ctx, Execution{Store: store, Lease: lease, TurnID: intent.Turn.ID})
			objects.beforePut = func(context.Context) error {
				switch mode {
				case "expired":
					return db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error
				case "generation":
					if err := remote.Abandon(context.Background(), lease); err != nil {
						return err
					}
					_, _, err := remote.Acquire(context.Background(), scope, uuid.NewString())
					return err
				default:
					lose(ErrQueueOwnerLost)
					return nil
				}
			}
			if _, err := store.AppendJournal(context.WithoutCancel(ctx), scope, &lease, intent.Turn.ID, []JournalEntry{journalEntry("must stay invisible")}, nil); !errors.Is(err, ErrQueueOwnerLost) {
				t.Fatalf("stale execution published after preparing: %v", err)
			}
			if head, err := remote.JournalHead(context.Background(), scope); err != nil || head != -1 {
				t.Fatalf("lost execution reserved a journal range: %d %v", head, err)
			}
		})
	}
}

func TestJournalPageLoadsCapturedRefsAfterReleasingCursorAndTransaction(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	ctx := context.Background()
	for _, event := range []string{"captured-one", "captured-two"} {
		if _, err := store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: event}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var once sync.Once
	objects.beforeOpen = func(ctx context.Context) (failure error) {
		once.Do(func() {
			probe, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			// Same-handle SQL must succeed with MaxOpenConns(1), including an
			// append and a raw-reference edit after the page captured its head.
			_, failure = store.AppendJournal(probe, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "later-head"}}, nil)
			if failure == nil {
				failure = db.WithContext(probe).Table("chat_event_chunks").Where("queue_id = ? AND first_sequence = ?", scope.ID(), 0).
					Update("payload", filestore.Prefix+"invalid").Error
			}
		})
		return failure
	}
	page, err := store.JournalPage(ctx, scope, JournalQuery{After: -1})
	if err != nil || page.Head != 1 || page.Next != 1 || page.HasMore || len(page.Items) != 2 || page.Items[0].Event != "captured-one" || page.Items[1].Event != "captured-two" {
		t.Fatalf("GET retained SQL or mixed in later refs/head: %+v %v", page, err)
	}
	objects.refuse.Store(true)
	gets := objects.opens.Load()
	if head, err := store.JournalHead(ctx, scope); err != nil || head != 2 {
		t.Fatalf("metadata depends on journal payload availability: %d %v", head, err)
	}
	if _, err := store.Metadata(ctx, scope); err != nil || objects.opens.Load() != gets {
		t.Fatalf("metadata touched object storage: %v", err)
	}
	failed, err := store.JournalPage(ctx, scope, JournalQuery{After: -1})
	if !errors.Is(err, ErrJournalGap) || failed.Next != -1 || len(failed.Items) != 0 {
		t.Fatalf("bad reference advanced a cursor: %+v %v", failed, err)
	}
}

func TestJournalExplicitStorageKeepsV1BytesLegacyReadersAndBackupClosure(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	ctx := context.Background()
	at := time.Date(2026, 11, 9, 1, 2, 3, 4, time.UTC)
	entry := JournalEntry{Type: FrameSession, Event: "legacy", At: at, Payload: map[string]any{"text": "original 中文 bytes"}}
	prepared, err := store.PrepareJournal(ctx, scope, nil, "turn", []JournalEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	commitPreparedJournal(t, store, prepared)
	entry.Seq, entry.Generation, entry.TurnID = 0, prepared.base.generation, "turn"
	want, err := json.Marshal(journalEnvelope{JournalVersion, scope.ID(), 0, 0, []JournalEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	var legacy models.ChatEventChunk
	if err := db.First(&legacy, "queue_id = ?", scope.ID()).Error; err != nil || !bytes.Equal(legacy.Payload, want) {
		t.Fatalf("prepared storage changed legacy v1 bytes: %q %v", legacy.Payload, err)
	}
	report, err := filestore.Migrate(ctx, db, &models.ChatEventChunk{})
	if err != nil || report.Verified != 1 || report.Migrated != 0 || len(report.Failures) != 0 {
		t.Fatalf("legacy migration lost the explicit journal reference: %+v %v", report, err)
	}
	backup, err := payloadstore.VerifyReferences(ctx, db, objects)
	if err != nil || backup.Payloads != 1 {
		t.Fatalf("backup catalogue omitted the journal reference: %+v %v", backup, err)
	}
	if err := db.Table("chat_event_chunks").Where("id = ?", legacy.ID).Update("payload", string(want)).Error; err != nil {
		t.Fatal(err)
	}
	gets := objects.opens.Load()
	page, err := remote.JournalPage(ctx, scope, JournalQuery{After: -1})
	if err != nil || page.Next != 0 || len(page.Items) != 1 || objects.opens.Load() != gets {
		t.Fatalf("inline legacy bytes require an object or changed cursor: %+v %v", page, err)
	}
	report, err = filestore.Migrate(ctx, db, &models.ChatEventChunk{})
	if err != nil || report.Migrated != 1 || len(report.Failures) != 0 {
		t.Fatalf("inline journal migration failed: %+v %v", report, err)
	}
	var row JournalChunkRecord
	if err := db.Table("chat_event_chunks").Where("id = ?", legacy.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	_, digest, err := filestore.Parse(row.Payload)
	if err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	delete(objects.objects, "payloads/sha256/"+digest[:2]+"/"+digest)
	objects.mu.Unlock()
	if _, err := payloadstore.VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("backup accepted a journal with a missing body")
	}
	page, err = remote.JournalPage(ctx, scope, JournalQuery{After: -1})
	if !errors.Is(err, ErrJournalGap) || page.Next != -1 || len(page.Items) != 0 {
		t.Fatalf("missing object became an empty successful page: %+v %v", page, err)
	}
}

func TestJournalCASRetriesRespectEarlierDeadlineAndRejectTransactionHandles(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	var attempts atomic.Int32
	started := time.Now()
	_, err := store.AppendJournalWith(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "conflict"}},
		func(context.Context, PreparedJournal) (JournalRange, error) {
			attempts.Add(1)
			return JournalRange{}, ErrJournalPrepareConflict
		})
	if !errors.Is(err, context.DeadlineExceeded) || attempts.Load() < 2 || time.Since(started) > time.Second {
		t.Fatalf("CAS retry exceeded its caller deadline: attempts=%d elapsed=%s %v", attempts.Load(), time.Since(started), err)
	}
	if head, err := store.JournalHead(context.Background(), scope); err != nil || head != -1 {
		t.Fatalf("uncommitted retry advanced its range: %d %v", head, err)
	}
	puts, gets := objects.puts.Load(), objects.opens.Load()
	if err := db.Transaction(func(tx *gorm.DB) error {
		inside := NewDurableStore(tx)
		if _, err := inside.PrepareJournal(context.Background(), scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "bad"}}); !errors.Is(err, ErrPayloadInTransaction) {
			t.Fatalf("preparation silently kept an outer TX: %v", err)
		}
		if _, err := inside.JournalPage(context.Background(), scope, JournalQuery{After: -1}); !errors.Is(err, ErrPayloadInTransaction) {
			t.Fatalf("read silently kept an outer TX: %v", err)
		}
		if _, err := inside.ReadJournalBase(context.Background(), scope); !errors.Is(err, ErrPayloadInTransaction) {
			t.Fatalf("head preparation silently kept an outer TX: %v", err)
		}
		if _, err := inside.LoadJournalChunk(context.Background(), JournalChunkRecord{}); !errors.Is(err, ErrPayloadInTransaction) {
			t.Fatalf("chunk hydration silently kept an outer TX: %v", err)
		}
		if _, err := inside.AppendJournal(context.Background(), scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "bad"}}, nil); !errors.Is(err, ErrPayloadInTransaction) {
			t.Fatalf("publication silently kept an outer TX: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if objects.puts.Load() != puts || objects.opens.Load() != gets {
		t.Fatal("rejected transaction handle performed object I/O")
	}
}

func TestJournalPreparedPublicationRejectsAutocommitAndForeignTransactionBeforeSQL(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store)
	foreignObjects := queuePayloadBackend(t, remote)
	ctx := context.Background()
	prepared, err := store.PrepareJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "prepared-only"}})
	if err != nil {
		t.Fatal(err)
	}
	var sqlCalls atomic.Int32
	for _, handle := range []*gorm.DB{db, remote.db} {
		probe := func(*gorm.DB) { sqlCalls.Add(1) }
		if err := handle.Callback().Create().Before("gorm:create").Register("test:journal-proof-no-create", probe); err != nil {
			t.Fatal(err)
		}
		if err := handle.Callback().Update().Before("gorm:update").Register("test:journal-proof-no-update", probe); err != nil {
			t.Fatal(err)
		}
		if err := handle.Callback().Query().Before("gorm:query").Register("test:journal-proof-no-query", probe); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = handle.Callback().Create().Remove("test:journal-proof-no-create")
			_ = handle.Callback().Update().Remove("test:journal-proof-no-update")
			_ = handle.Callback().Query().Remove("test:journal-proof-no-query")
		})
	}
	if written, err := store.AppendPreparedJournalTx(ctx, db, prepared, nil); err == nil || written.Count != 0 {
		t.Fatalf("prepared journal allowed partial autocommit writes: %+v %v", written, err)
	}
	if err := remote.db.Transaction(func(tx *gorm.DB) error {
		// Deliberately use the preparing Store with a different DB's TX. The
		// proof must be checked against tx's storage binding, not s.db's.
		if _, err := store.AppendPreparedJournalTx(ctx, tx, prepared, nil); err == nil {
			return errors.New("another storage namespace accepted the prepared journal body")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := store.AppendPreparedJournalTx(ctx, tx, PreparedJournal{}, nil); err == nil {
			return errors.New("zero preparation proof accepted")
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := store.AppendPreparedJournalTx(cancelled, tx, prepared, nil); !errors.Is(err, context.Canceled) {
			return errors.New("already cancelled publication reached SQL")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sqlCalls.Load() != 0 || objects.puts.Load() != 1 || objects.opens.Load() != 0 || foreignObjects.puts.Load() != 0 || foreignObjects.opens.Load() != 0 {
		t.Fatalf("rejected proof touched SQL or object storage: sql=%d localPut/Get=%d/%d foreignPut/Get=%d/%d", sqlCalls.Load(), objects.puts.Load(), objects.opens.Load(), foreignObjects.puts.Load(), foreignObjects.opens.Load())
	}
	if head, err := store.JournalHead(ctx, scope); err != nil || head != -1 {
		t.Fatalf("rejected transaction advanced its event head: %d %v", head, err)
	}
	var count int64
	if err := db.Table("chat_event_chunks").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected transaction left a journal reference: %d %v", count, err)
	}
}

func TestJournalHeadUpdateFailureRollsBackChunkAndZeroRowsRetriesWithoutGap(t *testing.T) {
	for _, mode := range []string{"sql-error", "zero-rows"} {
		t.Run(mode, func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			objects := queuePayloadBackend(t, store, remote)
			want := errors.New("journal head update refused")
			var updates atomic.Int32
			if err := db.Callback().Update().Before("gorm:update").Register("test:journal-head-update", func(tx *gorm.DB) {
				values, ok := tx.Statement.Dest.(map[string]any)
				if tx.Statement.Table != "chat_queues" || !ok || values["next_event_sequence"] == nil || updates.Add(1) != 1 {
					return
				}
				if mode == "sql-error" {
					_ = tx.AddError(want)
				} else {
					tx.Where("1 = 0")
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Update().Remove("test:journal-head-update") })
			objects.beforePut = func(ctx context.Context) error {
				if objects.puts.Load() != 2 {
					return nil
				}
				var count int64
				if err := db.WithContext(ctx).Table("chat_event_chunks").Count(&count).Error; err != nil {
					return err
				}
				if count != 0 {
					return errors.New("CAS retry retained its uncommitted chunk")
				}
				return nil
			}
			written, err := store.AppendJournal(context.Background(), scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "head-failure"}}, nil)
			if mode == "sql-error" {
				if !errors.Is(err, want) || written.Count != 0 {
					t.Fatalf("head SQL failure was acknowledged: %+v %v", written, err)
				}
				if head, err := remote.JournalHead(context.Background(), scope); err != nil || head != -1 {
					t.Fatalf("failed head write left a visible range: %d %v", head, err)
				}
				var chunks int64
				if err := db.Table("chat_event_chunks").Count(&chunks).Error; err != nil || chunks != 0 {
					t.Fatalf("failed head write committed its earlier chunk: %d %v", chunks, err)
				}
			} else {
				if err != nil || written.First != 0 || written.Last != 0 || updates.Load() != 2 || objects.puts.Load() != 2 {
					t.Fatalf("zero-row CAS did not retry the original range: %+v updates=%d puts=%d %v", written, updates.Load(), objects.puts.Load(), err)
				}
				page, err := remote.JournalPage(context.Background(), scope, JournalQuery{After: -1})
				if err != nil || len(page.Items) != 1 || page.Next != 0 {
					t.Fatalf("zero-row CAS produced duplicate or missing events: %+v %v", page, err)
				}
			}
		})
	}
}

func TestJournalFinalGuardFailureAndCancelledCASNeverPublishPreparedFrames(t *testing.T) {
	for _, mode := range []string{"guard", "cancelled-final-check", "cancelled-backoff"} {
		t.Run(mode, func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			objects := queuePayloadBackend(t, store, remote)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := errors.New("parent authorization changed before commit")
			checks := 0
			var err error
			if mode == "cancelled-backoff" {
				_, err = store.AppendJournalWith(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "cancelled-conflict"}}, func(context.Context, PreparedJournal) (JournalRange, error) {
					cancel()
					return JournalRange{}, ErrJournalPrepareConflict
				})
			} else {
				_, err = store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "late-guard"}}, func(*gorm.DB) error {
					checks++
					if checks == 2 {
						if mode == "guard" {
							return want
						}
						cancel()
					}
					return nil
				})
			}
			if mode != "guard" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || objects.puts.Load() != 1 {
				t.Fatalf("a late guard/cancellation retried or committed: puts=%d checks=%d %v", objects.puts.Load(), checks, err)
			}
			if head, err := remote.JournalHead(context.Background(), scope); err != nil || head != -1 {
				t.Fatalf("failed final guard left visible frames: %d %v", head, err)
			}
			var chunks int64
			if err := db.Table("chat_event_chunks").Count(&chunks).Error; err != nil || chunks != 0 {
				t.Fatalf("failed final guard retained a chunk reference: %d %v", chunks, err)
			}
		})
	}
}

func TestJournalMissingOrCorruptHeadCannotInventHistoryOrAllocateUnsafeSequences(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store)
	ctx := context.Background()
	if page, err := store.JournalPage(ctx, scope, JournalQuery{After: -1}); err != nil || page.Head != -1 || page.Next != -1 || len(page.Items) != 0 {
		t.Fatalf("a new conversation invented a journal: %+v %v", page, err)
	}
	if page, err := store.JournalPage(ctx, scope, JournalQuery{After: 0}); !errors.Is(err, ErrJournalCursor) || page.Next != 0 {
		t.Fatalf("a missing queue accepted a nonempty cursor: %+v %v", page, err)
	}
	if head, err := store.JournalHead(ctx, scope); err != nil || head != -1 {
		t.Fatalf("a missing queue invented a head: %d %v", head, err)
	}
	if _, err := store.ReadJournalBase(ctx, scope); err != nil {
		t.Fatal(err)
	}
	for _, next := range []uint64{1, journalSafeSequence, journalSafeSequence + 1} {
		if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("next_event_sequence", next).Error; err != nil {
			t.Fatal(err)
		}
		page, err := store.JournalPage(ctx, scope, JournalQuery{After: -1})
		if !errors.Is(err, ErrJournalGap) || page.Next != -1 || len(page.Items) != 0 {
			t.Fatalf("missing/corrupt index advanced the cursor: next=%d page=%+v %v", next, page, err)
		}
		if next >= journalSafeSequence {
			if _, err := store.PrepareJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "unsafe"}}); err == nil {
				t.Fatalf("unsafe next sequence %d was allocated", next)
			}
		}
		if next > journalSafeSequence {
			if head, err := store.JournalHead(ctx, scope); !errors.Is(err, ErrJournalGap) || head != -1 {
				t.Fatalf("unsafe journal head was returned: %d %v", head, err)
			}
		}
	}
	if objects.puts.Load() != 0 || objects.opens.Load() != 0 {
		t.Fatal("invalid metadata caused object I/O")
	}
}

func TestJournalMissingStorageBindingAndSQLFailuresRemainExplicit(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store)
	ctx := context.Background()
	prepared, err := store.PrepareJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "existing"}})
	if err != nil {
		t.Fatal(err)
	}
	commitPreparedJournal(t, store, prepared)
	// Simulate a handle missing its payload component, without changing the
	// real DB handle or the successfully published immutable object.
	unbound := db.Session(&gorm.Session{NewDB: true})
	unbound.Config.Plugins = map[string]gorm.Plugin{}
	unboundStore := NewDurableStore(unbound)
	if _, err := unboundStore.PrepareJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "unbound"}}); err == nil {
		t.Fatal("missing payload storage was silently accepted during preparation")
	}
	if page, err := unboundStore.JournalPage(ctx, scope, JournalQuery{After: -1}); err == nil || page.Next != -1 || len(page.Items) != 0 {
		t.Fatalf("missing payload storage became empty successful history: %+v %v", page, err)
	}
	if err := unbound.Transaction(func(tx *gorm.DB) error {
		if _, err := store.AppendPreparedJournalTx(ctx, tx, prepared, nil); err == nil {
			return errors.New("TX without payload storage accepted a published reference")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := errors.New("journal metadata SQL unavailable")
	for _, table := range []string{"chat_queues", "chat_event_chunks"} {
		if err := db.Callback().Query().Before("gorm:query").Register("test:journal-read-sql-failure", func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				_ = tx.AddError(want)
			}
		}); err != nil {
			t.Fatal(err)
		}
		page, err := store.JournalPage(ctx, scope, JournalQuery{After: -1})
		if !errors.Is(err, want) || page.Next != -1 || len(page.Items) != 0 {
			t.Fatalf("SQL outage became a partial page: %+v %v", page, err)
		}
		if table == "chat_queues" {
			if _, err := store.PrepareJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "sql-unavailable"}}); !errors.Is(err, want) {
				t.Fatalf("preparation concealed its metadata SQL failure: %v", err)
			}
		}
		if err := db.Callback().Query().Remove("test:journal-read-sql-failure"); err != nil {
			t.Fatal(err)
		}
	}
	if objects.puts.Load() != 1 || objects.opens.Load() != 0 {
		t.Fatal("failed SQL/storage binding touched immutable bodies")
	}
}

func TestJournalNativeWriterProgressesDuringConcurrentControlPublications(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	queuePayloadBackend(t, store, remote)
	intent := admitQueue(t, store, scope, "native with concurrent controls", "native")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	writer := NewJournalWriter(ctx, func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		return store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, entries, nil)
	}, nil)
	const nativeCount, producers, each = 317, 3, 16
	failures := make(chan error, producers+1)
	var group sync.WaitGroup
	for producer := range producers {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := range each {
				_, err := remote.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "control", Payload: map[string]any{"producer": producer, "index": i}}}, nil)
				if err != nil {
					failures <- err
					return
				}
			}
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		for i := range nativeCount {
			if err := writer.Add(ctx, journalEntry(fmt.Sprintf("native-%d", i))); err != nil {
				failures <- err
				return
			}
		}
		if err := writer.Close(ctx); err != nil {
			failures <- err
		}
	}()
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	page, err := store.JournalPage(context.Background(), scope, JournalQuery{After: -1, Limit: JournalPageEvents})
	if err != nil || len(page.Items) != nativeCount+producers*each || page.HasMore || writer.Written().Count != nativeCount {
		t.Fatalf("controls starved or lost native publication: items=%d range=%+v %v", len(page.Items), writer.Written(), err)
	}
	native, controls := 0, 0
	for i, entry := range page.Items {
		if entry.Seq != uint64(i) {
			t.Fatalf("CAS retry introduced a global event hole: %d %+v", i, entry)
		}
		if entry.Type == FrameAcp {
			var data struct{ Content struct{ Text string } }
			if err := json.Unmarshal(entry.Data, &data); err != nil || data.Content.Text != fmt.Sprintf("native-%d", native) {
				t.Fatalf("native order changed under control contention: %+v %v", entry, err)
			}
			native++
		} else {
			controls++
		}
	}
	if native != nativeCount || controls != producers*each {
		t.Fatalf("mixed journal lost events: native=%d controls=%d", native, controls)
	}
}

func TestJournalWriterBlockedObjectUsesTenSecondCommitDeadline(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	started := make(chan struct{})
	objects.beforePut = func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			return errors.New("journal writer lost its ten-second deadline")
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	var failures atomic.Int32
	writer := NewJournalWriter(context.Background(), func(ctx context.Context, entries []JournalEntry) (JournalRange, error) {
		return store.AppendJournal(ctx, scope, nil, "", entries, nil)
	}, func(error) { failures.Add(1) })
	if err := writer.Add(context.Background(), JournalEntry{Type: FrameSession, Event: "uncommitted", Payload: map[string]any{"text": strings.Repeat("x", 100)}}); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	done := make(chan error, 1)
	go func() { done <- writer.Flush(context.Background()) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("writer never entered object storage")
	}
	probe, cancel := context.WithTimeout(context.Background(), time.Second)
	if head, err := remote.JournalHead(probe, scope); err != nil || head != -1 {
		t.Fatalf("blocked writer held SQL or published an uncommitted event: %d %v", head, err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(begin) > 12*time.Second {
			t.Fatalf("writer exceeded its commit deadline: %s %v", time.Since(begin), err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("writer did not enforce its ten-second commit deadline")
	}
	if err := writer.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) || writer.Written().Count != 0 || failures.Load() != 1 {
		t.Fatalf("expired output was acknowledged or failure was lost: %+v notices=%d %v", writer.Written(), failures.Load(), err)
	}
}
