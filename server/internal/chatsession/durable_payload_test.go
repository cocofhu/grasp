package chatsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type queuePayloadObjects struct {
	*countedQueueObjects
	puts       atomic.Int32
	beforePut  func(context.Context) error
	beforeOpen func(context.Context) error
}

func (s *queuePayloadObjects) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	s.puts.Add(1)
	if s.beforePut != nil {
		if err := s.beforePut(ctx); err != nil {
			return err
		}
	}
	return s.countedQueueObjects.Put(ctx, key, body, info)
}

func (s *queuePayloadObjects) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	if s.beforeOpen != nil {
		if err := s.beforeOpen(ctx); err != nil {
			return nil, objectstore.Info{}, err
		}
	}
	return s.countedQueueObjects.Open(ctx, key)
}

func queuePayloadBackend(t *testing.T, stores ...*DurableStore) *queuePayloadObjects {
	t.Helper()
	objects := &queuePayloadObjects{countedQueueObjects: &countedQueueObjects{objects: map[string]countedQueueObject{}}}
	for _, store := range stores {
		files, err := filestore.FromDB(store.db)
		if err != nil {
			t.Fatal(err)
		}
		if err := files.SetObjectStore(objects); err != nil {
			t.Fatal(err)
		}
	}
	return objects
}

func TestDurableQueueBlockedPublicationHoldsNoQueueOrConnectionLock(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	ctx := context.Background()
	ready, err := store.PrepareIntent(ctx, scope, queueIntent("already durable", "ready"))
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.beforePut = func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := store.Admit(ctx, scope, queueIntent("slow publication", "slow"), 0, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("publication did not start")
	}
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := store.Metadata(probe, scope); err != nil {
		t.Fatalf("upload occupied the metadata connection: %v", err)
	}
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error { return store.GuardIdle(tx, scope) }); err != nil {
		t.Fatalf("pending upload held the queue stop lock: %v", err)
	}
	// PreparedRef must not wait on filestore's publication mutex while SQL is
	// locked. An earlier publication can commit during a different slow Put.
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error {
		_, err := store.AdmitPreparedTx(tx, scope, ready, 0, nil)
		return err
	}); err != nil {
		t.Fatalf("prepared SQL admission waited for unrelated upload: %v", err)
	}
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error { return store.GuardIdle(tx, scope) }); !errors.Is(err, ErrQueueBusy) {
		t.Fatalf("accepted command no longer fences stop: %v", err)
	}
	if _, err := store.Cancel(probe, scope, "", true); err != nil {
		t.Fatalf("queue cancellation waited for upload: %v", err)
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admission failed to finish after publication")
	}
	if objects.puts.Load() != 2 || objects.opens.Load() != 0 {
		t.Fatalf("SQL admission performed hidden payload I/O: puts=%d gets=%d", objects.puts.Load(), objects.opens.Load())
	}
}

func TestDurableQueueSQLFailureLeavesOnlyVerifiableOrphanAndKeepsBackupCompatibility(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	ctx := context.Background()
	intent := queueIntent("original accepted bytes\x00\xff", "retry")
	want := errors.New("injected receipt failure")
	if err := db.Callback().Create().Before("gorm:create").Register("test:prepared-receipt-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queue_turns" {
			_ = tx.AddError(want)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Admit(ctx, scope, intent, 0, nil); !errors.Is(err, want) {
		t.Fatalf("SQL failure acknowledged admission: %v", err)
	}
	if err := db.Callback().Create().Remove("test:prepared-receipt-failure"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.ChatQueueTurn{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed admission left a SQL reference: %d %v", count, err)
	}
	hash := sha256.Sum256(intent.Payload)
	digest := hex.EncodeToString(hash[:])
	key := "payloads/sha256/" + digest[:2] + "/" + digest
	objects.mu.Lock()
	orphan, exists := objects.objects[key]
	objectCount := len(objects.objects)
	objects.mu.Unlock()
	if !exists || !bytes.Equal(orphan.body, intent.Payload) || objectCount != 1 {
		t.Fatal("failed SQL did not retain its exact content-addressed orphan")
	}
	if _, err := store.Admit(ctx, scope, intent, 0, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := rawQueueBody(db.Table("chat_queue_turns").Where("id = ?", intent.ID))
	if err != nil || raw != filestore.Prefix+filestore.Bytes+"/"+digest || objects.opens.Load() != 0 {
		t.Fatalf("SQL contains logical bytes or admission read the body: %s gets=%d %v", raw, objects.opens.Load(), err)
	}
	// Old serializer readers and migration/backup discovery still use exactly
	// the same column and reference format as explicit queue storage.
	var legacy models.ChatQueueTurn
	if err := db.First(&legacy, "id = ?", intent.ID).Error; err != nil || !bytes.Equal(legacy.Payload, intent.Payload) {
		t.Fatalf("old reader cannot read new reference: %q %v", legacy.Payload, err)
	}
	report, err := filestore.Migrate(ctx, db, &models.ChatQueueTurn{})
	if err != nil || report.Verified != 1 || report.Migrated != 0 || len(report.Failures) != 0 {
		t.Fatalf("migration catalogue lost new queue reference: %+v %v", report, err)
	}
	backup, err := payloadstore.VerifyReferences(ctx, db, objects)
	if err != nil || backup.Cells < 1 || backup.Payloads != 1 {
		t.Fatalf("backup reference scan omitted prepared queue body: %+v %v", backup, err)
	}
	puts, gets := objects.puts.Load(), objects.opens.Load()
	objects.beforePut = func(context.Context) error { return errors.New("storage outage") }
	objects.refuse.Store(true)
	if duplicate, err := store.Admit(ctx, scope, intent, 0, nil); err != nil || !duplicate.Duplicate || objects.puts.Load() != puts || objects.opens.Load() != gets {
		t.Fatalf("lost-ACK resolution depends on payload service: %+v puts=%d gets=%d %v", duplicate, objects.puts.Load(), objects.opens.Load(), err)
	}
	objects.refuse.Store(false)
	objects.mu.Lock()
	delete(objects.objects, key)
	objects.mu.Unlock()
	if _, err := payloadstore.VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("backup accepted a missing queue reference")
	}
}

func TestDurableQueueClaimHydratesAfterReleasingSQLAndRechecksFence(t *testing.T) {
	for _, mode := range []string{"ready", "cancelled", "closed", "expired", "missing"} {
		t.Run(mode, func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			objects := queuePayloadBackend(t, store, remote)
			ctx := context.Background()
			now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
			store.now, remote.now = func() time.Time { return now }, func() time.Time { return now }
			accepted := admitQueue(t, store, scope, "precise execution input\x00\xff", "one")
			lease := acquireQueue(t, store, scope)
			var once sync.Once
			objects.beforeOpen = func(ctx context.Context) (hookErr error) {
				once.Do(func() {
					probe, cancel := context.WithTimeout(ctx, time.Second)
					defer cancel()
					// This is the same DB's sole SQLite connection. It cannot
					// renew or commit if Claim still owns a cursor/transaction.
					if _, hookErr = store.RenewUntil(probe, lease); hookErr != nil {
						return
					}
					switch mode {
					case "cancelled":
						_, hookErr = store.Cancel(probe, scope, accepted.Turn.ID, false)
					case "closed":
						hookErr = db.WithContext(probe).Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) })
					case "expired":
						now = now.Add(store.leaseTTL + time.Second)
					case "missing":
						hookErr = errors.New("temporary object unavailability")
					}
				})
				return hookErr
			}
			claimed, err := store.Claim(ctx, lease)
			if mode == "ready" {
				if err != nil || claimed == nil || string(claimed.Payload) != "precise execution input\x00\xff" || objects.opens.Load() != 1 {
					t.Fatalf("claim did not hydrate exact original once outside SQL: %+v gets=%d %v", claimed, objects.opens.Load(), err)
				}
				if err := store.Dispatch(ctx, lease, claimed.ID); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || claimed != nil {
				t.Fatalf("stale or unavailable hydrated input escaped to execution: %+v %v", claimed, err)
			}
			if (mode == "cancelled" || mode == "closed") && !errors.Is(err, context.Canceled) {
				t.Fatalf("explicit cancellation was ignored: %v", err)
			}
			if mode == "expired" && !errors.Is(err, ErrQueueOwnerLost) {
				t.Fatalf("expired hydration retained ownership: %v", err)
			}
			row, readErr := store.Receipt(ctx, scope, accepted.Turn.ID)
			if readErr != nil || row.DispatchedAt != nil {
				t.Fatalf("body preparation left dispatch proof: %+v %v", row, readErr)
			}
			if mode == "cancelled" || mode == "closed" {
				if row.State != TurnCancelled {
					t.Fatalf("cancelled hydration was requeued: %+v", row)
				}
				return
			}
			if mode == "expired" {
				var recovered *models.ChatQueueTurn
				lease, recovered, err = remote.Acquire(ctx, scope, uuid.NewString())
				if err != nil || recovered == nil || recovered.State != TurnQueued || len(recovered.Payload) != 0 {
					t.Fatalf("undispatched preparation could not recover metadata: %+v %v", recovered, err)
				}
				store = remote
			}
			claimed, err = store.Claim(ctx, lease)
			if err != nil || claimed == nil || claimed.ID != accepted.Turn.ID || string(claimed.Payload) != "precise execution input\x00\xff" {
				t.Fatalf("safe undispatched preparation did not retry: %+v %v", claimed, err)
			}
		})
	}
}

func TestDurableQueueRecoveryReturnsMetadataWithoutBlockingOnPayloadStore(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	store.now, remote.now = func() time.Time { return now }, func() time.Time { return now }
	accepted := admitQueue(t, store, scope, "uncertain external action", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if err := store.Dispatch(context.Background(), lease, accepted.Turn.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(store.leaseTTL + time.Second)
	objects.opens.Store(0)
	objects.refuse.Store(true)
	successor, recovered, err := remote.Acquire(context.Background(), scope, uuid.NewString())
	if err != nil || recovered == nil || recovered.State != TurnUnknown || len(recovered.Payload) != 0 || objects.opens.Load() != 0 {
		t.Fatalf("lease acquisition tried to read an uncertain body: %+v gets=%d %v", recovered, objects.opens.Load(), err)
	}
	if claimed, err := remote.Claim(context.Background(), successor); err != nil || claimed != nil || objects.opens.Load() != 0 {
		t.Fatalf("unknown execution was replayed: %+v gets=%d %v", claimed, objects.opens.Load(), err)
	}
}

func TestDurableQueuePayloadPreparationCannotHideInsideOuterTransaction(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	ctx := context.Background()
	intent := queueIntent("original immutable payload", "one")
	intent.Display = []byte(`{"id":"original"}`)
	prepared, err := store.PrepareIntent(ctx, scope, intent)
	if err != nil {
		t.Fatal(err)
	}
	intent.Payload[0], intent.Display[7] = 'X', 'X'
	otherScope := QueueScope{Surface: scope.Surface, Key: "other"}
	if _, err := store.AdmitPreparedTx(db, scope, prepared, 0, nil); err == nil {
		t.Fatal("prepared admission allowed partial autocommit writes")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		bound := NewDurableStore(tx)
		for name, operation := range map[string]func() error{
			"prepare":   func() error { _, err := bound.PrepareIntent(ctx, scope, intent); return err },
			"admit":     func() error { _, err := bound.Admit(ctx, scope, intent, 0, nil); return err },
			"read":      func() error { _, err := bound.ReadTurn(ctx, scope, intent.ID); return err },
			"snapshot":  func() error { _, err := bound.Snapshot(ctx, scope); return err },
			"claim":     func() error { _, err := bound.Claim(ctx, QueueLease{Scope: scope}); return err },
			"migration": func() error { _, err := bound.ImportLegacy(ctx, scope, "owner"); return err },
		} {
			if err := operation(); !errors.Is(err, ErrPayloadInTransaction) {
				t.Fatalf("%s hid object I/O under outer TX: %v", name, err)
			}
		}
		if _, err := store.AdmitPreparedTx(tx, scope, PreparedIntent{}, 0, nil); err == nil {
			t.Fatal("zero proof admitted")
		}
		if _, err := store.AdmitPreparedTx(tx, otherScope, prepared, 0, nil); err == nil {
			t.Fatal("foreign-scope proof admitted")
		}
		_, err := store.AdmitPreparedTx(tx, scope, prepared, 0, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if objects.puts.Load() != 1 || objects.opens.Load() != 0 {
		t.Fatalf("outer TX performed hidden payload I/O: puts=%d gets=%d", objects.puts.Load(), objects.opens.Load())
	}
	read, err := store.ReadTurn(ctx, scope, prepared.intent.ID)
	if err != nil || string(read.Payload) != "original immutable payload" || read.Display != `{"id":"original"}` {
		t.Fatalf("caller mutated its already prepared bytes: %+v %v", read, err)
	}
	if err := remote.db.Transaction(func(tx *gorm.DB) error {
		_, err := remote.AdmitPreparedTx(tx, scope, prepared, 0, nil)
		if err == nil {
			t.Fatal("a different storage handle accepted a prepared proof")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDurableQueueLegacyPreparationChecksSourceAgainAfterUpload(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	objects := queuePayloadBackend(t, store, remote)
	ctx := context.Background()
	for i, id := range []string{"legacy-one", "legacy-two"} {
		if err := db.Table("chat_pending_turns").Create(map[string]any{"id": id, "surface": scope.Surface, "session_key": scope.Key,
			"position": i, "payload": "raw body " + id, "started": false, "created_at": time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
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
	go func() { _, err := store.ImportLegacy(ctx, scope, "owner"); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("legacy preparation did not begin")
	}
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := db.WithContext(probe).Table("chat_pending_turns").Where("id = ?", "legacy-one").Update("started", true).Error; err != nil {
		t.Fatalf("legacy upload held a SQL connection/lock: %v", err)
	}
	unblock.Do(func() { close(release) })
	if err := <-done; !errors.Is(err, ErrQueueChanged) {
		t.Fatalf("migration silently consumed a changed legacy source: %v", err)
	}
	var sources, receipts int64
	if err := db.Table("chat_pending_turns").Count(&sources).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("chat_queue_turns").Count(&receipts).Error; err != nil || sources != 2 || receipts != 0 {
		t.Fatalf("source CAS failure partially migrated: source=%d receipt=%d %v", sources, receipts, err)
	}
	imported, err := store.ImportLegacy(ctx, scope, "owner")
	if err != nil || len(imported) != 2 || imported[0].State != TurnUnknown || imported[1].State != TurnQueued {
		t.Fatalf("safe retry replayed started work or lost unstarted work: %+v %v", imported, err)
	}
}
