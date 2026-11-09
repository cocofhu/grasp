package chatsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func durableFixture(t *testing.T) (*gorm.DB, *DurableStore, *DurableStore, QueueScope) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.db")
	a, err := database.OpenSQLiteTest(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := database.ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: path}, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(a); _ = database.Close(b) })
	return a, NewDurableStore(a), NewDurableStore(b), QueueScope{Surface: "solo", Key: uuid.NewString()}
}

func queueIntent(text, client string) QueueIntent {
	h := sha256.Sum256([]byte(text))
	return QueueIntent{ID: uuid.NewString(), ActorID: "owner", ClientMessageID: client, Fingerprint: hex.EncodeToString(h[:]), Payload: []byte(text)}
}

func admitQueue(t *testing.T, store *DurableStore, scope QueueScope, text, client string) QueueReceipt {
	t.Helper()
	receipt, err := store.Admit(context.Background(), scope, queueIntent(text, client), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func acquireQueue(t *testing.T, store *DurableStore, scope QueueScope) QueueLease {
	t.Helper()
	lease, _, err := store.Acquire(context.Background(), scope, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestDurableQueueDeferredPreparationKeepsOrderAndHonorsCancellation(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	ctx := context.Background()
	first := admitQueue(t, store, scope, "first original body", "one")
	second := admitQueue(t, store, scope, "second original body", "two")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := store.DeferPreparation(ctx, lease, first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := remote.Snapshot(ctx, scope)
	if err != nil || snapshot.Active != nil || len(snapshot.Pending) != 2 || snapshot.Pending[0].ID != first.Turn.ID || snapshot.Pending[1].ID != second.Turn.ID {
		t.Fatalf("defer changed FIFO: %+v %v", snapshot, err)
	}
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Cancel(ctx, scope, first.Turn.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := store.DeferPreparation(ctx, lease, first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	row, err := store.Receipt(ctx, scope, first.Turn.ID)
	if err != nil || row.State != TurnCancelled {
		t.Fatalf("explicit cancel was requeued: %+v %v", row, err)
	}
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := store.Dispatch(ctx, lease, second.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeferPreparation(ctx, lease, second.Turn.ID); !errors.Is(err, ErrQueueChanged) {
		t.Fatalf("dispatched side effect was made replayable: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Reopen(tx, scope) }); !errors.Is(err, ErrQueueChanged) {
		t.Fatalf("reopen overlapped active dispatch: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Finish(tx, lease, second.Turn.ID, TurnSucceeded, "", "result") }); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Reopen(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	if row, err := store.Existing(ctx, scope, "owner", "one", first.Turn.Fingerprint); err != nil || row == nil || row.State != TurnCancelled {
		t.Fatalf("reopen forgot original acceptance: %+v %v", row, err)
	}
	if _, err := store.Existing(ctx, scope, "owner", "one", second.Turn.Fingerprint); !errors.Is(err, ErrClientConflict) {
		t.Fatalf("changed body reused receipt: %v", err)
	}
	if row, err := store.Existing(ctx, scope, "owner", "ONE", first.Turn.Fingerprint); err != nil || row != nil {
		t.Fatalf("case-sensitive nonce aliased another message: %+v %v", row, err)
	}
	if waiting, err := store.Waiting(ctx, scope); err != nil || waiting != 0 {
		t.Fatalf("terminal work became pending: %d %v", waiting, err)
	}
}

func TestDurableQueueUnavailableDatabaseFailsClosed(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	row := admitQueue(t, store, scope, "retained", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{
		"metadata":          func() error { _, err := store.Metadata(ctx, scope); return err },
		"admission":         func() error { _, err := store.Admit(ctx, scope, queueIntent("retry", "two"), 0, nil); return err },
		"deduplication":     func() error { _, err := store.Existing(ctx, scope, "owner", "one", row.Turn.Fingerprint); return err },
		"snapshot":          func() error { _, err := store.Snapshot(ctx, scope); return err },
		"snapshot metadata": func() error { _, err := store.SnapshotMetadata(ctx, scope); return err },
		"read body":         func() error { _, err := store.ReadTurn(ctx, scope, row.Turn.ID); return err },
		"backfill preview": func() error {
			return store.BackfillDisplay(ctx, scope, row.Turn.ID, row.Turn.Fingerprint, []byte(`{}`))
		},
		"acquisition":       func() error { _, _, err := store.Acquire(ctx, scope, uuid.NewString()); return err },
		"renewal":           func() error { return store.Renew(ctx, lease) },
		"claim":             func() error { _, err := store.Claim(ctx, lease); return err },
		"dispatch":          func() error { return store.Dispatch(ctx, lease, row.Turn.ID) },
		"waiting":           func() error { _, err := store.Waiting(ctx, scope); return err },
		"cancel":            func() error { _, err := store.Cancel(ctx, scope, "", true); return err },
		"cancellation read": func() error { _, err := store.Cancellation(ctx, lease, row.Turn.ID); return err },
		"remove":            func() error { return store.Remove(ctx, scope, row.Turn.ID) },
		"reorder":           func() error { return store.Reorder(ctx, scope, []string{row.Turn.ID}) },
		"discovery":         func() error { _, err := store.ReadyScopes(ctx, scope.Surface); return err },
		"watermark":         func() error { _, err := store.Watermark(ctx); return err },
		"defer":             func() error { return store.DeferPreparation(ctx, lease, row.Turn.ID) },
		"release":           func() error { return store.Release(ctx, lease) },
		"legacy migration":  func() error { _, err := store.ImportLegacy(ctx, scope, "owner"); return err },
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); err == nil {
				t.Fatal("database failure reported success")
			}
		})
	}
}

func TestDurableQueueMetadataDoesNotDecodeAcceptedBodies(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	ctx := context.Background()
	if meta, err := remote.Metadata(ctx, scope); err != nil || meta.Busy || meta.Waiting != 0 {
		t.Fatalf("empty metadata changed: %+v %v", meta, err)
	}
	row := admitQueue(t, store, scope, "unreadable JSON body", "one")
	admitQueue(t, store, scope, "another opaque body", "two")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	meta, err := remote.Metadata(ctx, scope)
	if err != nil || !meta.Busy || meta.Waiting != 1 || len(meta.Items) != 0 || meta.ActiveItem != nil {
		t.Fatalf("metadata loaded bodies or lost active receipt %s: %+v %v", row.Turn.ID, meta, err)
	}
}

func TestDurableQueueRejectsInvalidIdentityAndInconsistentActiveReceipt(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	valid := queueIntent("original", "client")
	for name, mutate := range map[string]func(*QueueIntent){
		"missing turn":   func(i *QueueIntent) { i.ID = "" },
		"missing actor":  func(i *QueueIntent) { i.ActorID = "" },
		"missing nonce":  func(i *QueueIntent) { i.ClientMessageID = "" },
		"control nonce":  func(i *QueueIntent) { i.ClientMessageID = "bad\nnonce" },
		"invalid digest": func(i *QueueIntent) { i.Fingerprint = strings.Repeat("z", 64) },
		"missing body":   func(i *QueueIntent) { i.Payload = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if _, err := store.Admit(ctx, scope, candidate, 0, nil); err == nil {
				t.Fatal("invalid intent accepted")
			}
		})
	}
	if _, err := store.Admit(ctx, QueueScope{Surface: "solo", Key: ""}, valid, 0, nil); err == nil {
		t.Fatal("empty conversation accepted")
	}
	if _, _, err := store.Acquire(ctx, scope, "process-local-name"); err == nil {
		t.Fatal("invalid owner accepted")
	}
	if snapshot, err := store.Snapshot(ctx, scope); err != nil || snapshot.Active != nil || len(snapshot.Pending) != 0 {
		t.Fatalf("rejected intent created work: %+v %v", snapshot, err)
	}
	row := admitQueue(t, store, scope, "retained body", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("active_turn_id", "missing-receipt").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(ctx, scope); err == nil {
		t.Fatal("inconsistent active turn appeared idle")
	}
	var retained models.ChatQueueTurn
	if err := db.First(&retained, "id = ?", row.Turn.ID).Error; err != nil || string(retained.Payload) != "retained body" {
		t.Fatal("inconsistent metadata discarded original intent")
	}
}

func TestDurableQueueCrashAfterCancelDoesNotResurrectUnsentWork(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(fmt.Sprint(dispatched), func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			ctx := context.Background()
			first := admitQueue(t, store, scope, "must not replay", "one")
			second := admitQueue(t, store, scope, "next explicit intent", "two")
			lease := acquireQueue(t, store, scope)
			if _, err := store.Claim(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if dispatched {
				if err := store.Dispatch(ctx, lease, first.Turn.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := remote.Cancel(ctx, scope, first.Turn.ID, false); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			next, recovered, err := remote.Acquire(ctx, scope, uuid.NewString())
			want := TurnCancelled
			if dispatched {
				want = TurnUnknown
			}
			if err != nil || recovered == nil || recovered.State != want {
				t.Fatalf("cancel request vanished on crash: %+v %v", recovered, err)
			}
			if turn, err := remote.Claim(ctx, next); err != nil || turn == nil || turn.ID != second.Turn.ID {
				t.Fatalf("cancelled action replayed: %+v %v", turn, err)
			}
		})
	}
}

func TestDurableQueueClosedCrashEventuallyReleasesItsOwner(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(fmt.Sprint(dispatched), func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			ctx := context.Background()
			row := admitQueue(t, store, scope, "retained original", "one")
			lease := acquireQueue(t, store, scope)
			if _, err := store.Claim(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if dispatched {
				if err := store.Dispatch(ctx, lease, row.Turn.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
				t.Fatal(err)
			}
			if err := remote.ReconcileRetired(ctx, scope.Surface); err != nil {
				t.Fatal(err)
			}
			if snapshot, err := remote.Snapshot(ctx, scope); err != nil || snapshot.Active == nil {
				t.Fatalf("closed queue stole a live executor: %+v %v", snapshot, err)
			}
			if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			if err := remote.ReconcileRetired(ctx, scope.Surface); err != nil {
				t.Fatal(err)
			}
			if snapshot, err := remote.Snapshot(ctx, scope); err != nil || snapshot.Active != nil || !snapshot.Queue.Retired || snapshot.Queue.Owner != "" {
				t.Fatalf("closed crash left an immortal active owner: %+v %v", snapshot, err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return remote.Reopen(tx, scope) }); err != nil {
				t.Fatal(err)
			}
			if scopes, err := store.ReadyScopes(ctx, scope.Surface); err != nil || len(scopes) != 0 {
				t.Fatalf("reopen replayed closed work: %+v %v", scopes, err)
			}
			var retained models.ChatQueueTurn
			if err := db.First(&retained, "id = ?", row.Turn.ID).Error; err != nil || string(retained.Payload) != "retained original" {
				t.Fatal("retired recovery deleted command evidence")
			}
		})
	}
}

func TestDurableQueueLegacyConflictOrClassificationFailurePreservesWholeSource(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	legacy := []models.ChatPendingTurn{{ID: "legacy-one", Surface: scope.Surface, SessionKey: scope.Key, Position: 1, Payload: []byte("original one")}, {ID: "legacy-two", Surface: scope.Surface, SessionKey: scope.Key, Position: 2, Payload: []byte("original two")}}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	failure := errors.New("transcript classification unavailable")
	if _, err := store.ImportLegacy(ctx, scope, "owner", func(_ *gorm.DB, old models.ChatPendingTurn) (string, string, error) {
		if old.ID == "legacy-two" {
			return "", "", failure
		}
		return TurnQueued, "", nil
	}); !errors.Is(err, failure) {
		t.Fatalf("classification was not atomic: %v", err)
	}
	var originals, receipts int64
	if err := db.Model(&models.ChatPendingTurn{}).Count(&originals).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ChatQueueTurn{}).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if originals != 2 || receipts != 0 {
		t.Fatalf("failed migration discarded intent: original=%d receipt=%d", originals, receipts)
	}
	intent := queueIntent("different existing receipt", "existing")
	intent.ID = "legacy-one"
	admitted, err := store.Admit(ctx, scope, intent, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportLegacy(ctx, scope, "owner"); err == nil {
		t.Fatal("conflicting receipt replaced original bytes")
	}
	var retained models.ChatPendingTurn
	if err := db.First(&retained, "id = ?", "legacy-one").Error; err != nil || string(retained.Payload) != "original one" {
		t.Fatalf("source lost: %q %v", retained.Payload, err)
	}
	if got, err := store.Existing(ctx, scope, "owner", "existing", intent.Fingerprint); err != nil || got == nil || got.ID != admitted.Turn.ID {
		t.Fatal("conflict changed preexisting receipt")
	}
}

func TestDurableQueueConcurrentAdmissionAndTerminalIdentity(t *testing.T) {
	ctx := context.Background()
	db, a, b, scope := durableFixture(t)
	var accepted, duplicates atomic.Int32
	var wg sync.WaitGroup
	errorsCh := make(chan error, 16)
	ids := make(chan string, 16)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := a
			if i%2 != 0 {
				store = b
			}
			receipt, err := store.Admit(ctx, scope, queueIntent("same original request", "one-intent"), 0, nil)
			if err != nil {
				errorsCh <- err
				return
			}
			if receipt.Duplicate {
				duplicates.Add(1)
			} else {
				accepted.Add(1)
			}
			ids <- receipt.Turn.ID
		}()
	}
	wg.Wait()
	close(errorsCh)
	close(ids)
	for err := range errorsCh {
		t.Error(err)
	}
	if accepted.Load() != 1 || duplicates.Load() != 15 {
		t.Fatalf("admitted=%d duplicate=%d", accepted.Load(), duplicates.Load())
	}
	var identity string
	for id := range ids {
		if identity != "" && id != identity {
			t.Fatal("HTTP retries created different turns")
		}
		identity = id
	}
	if _, err := a.Admit(ctx, scope, queueIntent("different", "one-intent"), 0, nil); !errors.Is(err, ErrClientConflict) {
		t.Fatalf("changed intent reused identity: %v", err)
	}
	lease := acquireQueue(t, a, scope)
	if _, _, err := b.Acquire(ctx, scope, uuid.NewString()); !errors.Is(err, ErrQueueOwned) {
		t.Fatalf("second executor admitted: %v", err)
	}
	turn, err := a.Claim(ctx, lease)
	if err != nil || turn == nil || turn.ID != identity || string(turn.Payload) != "same original request" {
		t.Fatalf("claimed original=%+v err=%v", turn, err)
	}
	if err := a.Dispatch(ctx, lease, turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&models.Setting{Key: "atomic-result", Value: "committed"}).Error; err != nil {
			return err
		}
		return a.Finish(tx, lease, turn.ID, TurnSucceeded, "", "reply:"+turn.ID)
	}); err != nil {
		t.Fatal(err)
	}
	retry, err := b.Admit(ctx, scope, queueIntent("same original request", "one-intent"), 0, nil)
	if err != nil || !retry.Duplicate || retry.Turn.ID != identity || retry.Turn.State != TurnSucceeded {
		t.Fatalf("lost HTTP acknowledgement replayed terminal request: %+v %v", retry, err)
	}
	if err := a.Release(ctx, lease); err != nil {
		t.Fatal(err)
	}
	watermark, err := b.Watermark(ctx)
	if err != nil || watermark < lease.Generation || watermark > 1<<53 {
		t.Fatalf("generation cannot be compared by browser: %d %v", watermark, err)
	}
}

func TestDurableQueueAdmissionFailureRollsBackAndNeverAcknowledges(t *testing.T) {
	ctx := context.Background()
	db, store, _, scope := durableFixture(t)
	want := errors.New("database unavailable")
	if err := db.Callback().Create().Before("gorm:create").Register("test:admission_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queue_turns" {
			_ = tx.AddError(want)
		}
	}); err != nil {
		t.Fatal(err)
	}
	intent := queueIntent("retained on client until receipt commits", "retry")
	if _, err := store.Admit(ctx, scope, intent, 0, nil); !errors.Is(err, want) {
		t.Fatalf("failed database write was accepted: %v", err)
	}
	if err := db.Callback().Create().Remove("test:admission_failure"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.ChatQueueTurn{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial admission leaked SQL state: %d %v", count, err)
	}
	if _, err := store.Admit(ctx, scope, intent, 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Admit(ctx, scope, queueIntent("over capacity", "another"), 1, nil); err == nil {
		t.Fatal("durable capacity limit ignored")
	}
	other := QueueScope{Surface: "solo", Key: "closed-parent"}
	if _, err := store.Admit(ctx, other, queueIntent("late", "closed"), 0, func(*gorm.DB) error { return ErrQueueRetired }); !errors.Is(err, ErrQueueRetired) {
		t.Fatalf("concurrent parent close ignored: %v", err)
	}
}

func TestDurableQueueExpiredDispatchBecomesUnknownAndStaleOwnerCannotPublish(t *testing.T) {
	ctx := context.Background()
	db, a, b, scope := durableFixture(t)
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	a.now, b.now = func() time.Time { return now }, func() time.Time { return now }
	first := admitQueue(t, a, scope, "external action", "one")
	second := admitQueue(t, a, scope, "next user request", "two")
	lease := acquireQueue(t, a, scope)
	if _, err := a.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := a.Dispatch(ctx, lease, first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(a.leaseTTL + time.Second)
	if err := a.Renew(ctx, lease); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("expired owner revived its lease: %v", err)
	}
	successor, recovered, err := b.Acquire(ctx, scope, uuid.NewString())
	if err != nil || recovered == nil || recovered.ID != first.Turn.ID || recovered.State != TurnUnknown || len(recovered.Payload) != 0 {
		t.Fatalf("uncertain dispatch was replayed or discarded: %+v %v", recovered, err)
	}
	retained, err := b.ReadTurn(ctx, scope, recovered.ID)
	if err != nil || string(retained.Payload) != "external action" {
		t.Fatalf("unknown receipt lost its accepted body: %+v %v", retained, err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&models.Setting{Key: "stale-result", Value: "must rollback"}).Error; err != nil {
			return err
		}
		return a.Finish(tx, lease, first.Turn.ID, TurnSucceeded, "", "stale")
	})
	if !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("stale result accepted: %v", err)
	}
	var count int64
	if err := db.Model(&models.Setting{}).Where("key = ?", "stale-result").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("publication was not atomic with the generation fence")
	}
	claimed, err := b.Claim(ctx, successor)
	if err != nil || claimed == nil || claimed.ID != second.Turn.ID {
		t.Fatalf("already dispatched turn ran twice: %+v %v", claimed, err)
	}
	if err := a.Dispatch(ctx, lease, first.Turn.ID); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatal("old actor dispatched after recovery")
	}
}

func TestDurableQueuePreparationRecoveryReorderAndTargetedCancellation(t *testing.T) {
	ctx := context.Background()
	db, a, b, scope := durableFixture(t)
	now := time.Now().UTC()
	a.now, b.now = func() time.Time { return now }, func() time.Time { return now }
	one := admitQueue(t, a, scope, "one", "one")
	two := admitQueue(t, a, scope, "two", "two")
	three := admitQueue(t, a, scope, "three", "three")
	if err := b.Reorder(ctx, scope, []string{three.Turn.ID, one.Turn.ID, two.Turn.ID}); err != nil {
		t.Fatal(err)
	}
	if err := b.Reorder(ctx, scope, []string{one.Turn.ID, one.Turn.ID, two.Turn.ID}); !errors.Is(err, ErrQueueChanged) {
		t.Fatal("duplicate reorder identity accepted")
	}
	lease := acquireQueue(t, a, scope)
	active, err := a.Claim(ctx, lease)
	if err != nil || active.ID != three.Turn.ID {
		t.Fatalf("wrong durable order: %+v %v", active, err)
	}
	now = now.Add(a.leaseTTL + time.Second)
	next, recovered, err := b.Acquire(ctx, scope, uuid.NewString())
	if err != nil || recovered.State != TurnQueued || recovered.ID != three.Turn.ID {
		t.Fatalf("preparation-only work lost: %+v %v", recovered, err)
	}
	active, err = b.Claim(ctx, next)
	if err != nil || active.ID != three.Turn.ID {
		t.Fatal("preparation resumed out of order")
	}
	matched, err := a.Cancel(ctx, scope, one.Turn.ID, true)
	if err != nil || matched {
		t.Fatal("stale cancellation affected a different active turn")
	}
	matched, err = a.Cancel(ctx, scope, active.ID, false)
	if err != nil || !matched {
		t.Fatal("cross-replica cancellation was lost")
	}
	cancelled, err := b.Cancellation(ctx, next, active.ID)
	if err != nil || !cancelled {
		t.Fatal("owner did not see targeted cancellation")
	}
	if err := b.Remove(ctx, scope, one.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove(ctx, scope, active.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("active request removed as a waiter")
	}
	if _, err := a.Cancel(ctx, scope, active.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return b.Finish(tx, next, active.ID, TurnCancelled, "user_cancelled", "") }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.Snapshot(ctx, scope)
	if err != nil || snapshot.Active != nil || len(snapshot.Pending) != 0 {
		t.Fatalf("cancelled rows remained dispatchable: %+v %v", snapshot, err)
	}
	for _, intent := range []QueueIntent{queueIntent("one", "one"), queueIntent("two", "two")} {
		receipt, err := a.Admit(ctx, scope, intent, 0, nil)
		if err != nil || !receipt.Duplicate || receipt.Turn.State != TurnCancelled {
			t.Fatal("cancelled intent was resubmitted by an acknowledgement retry")
		}
	}
}

func TestDurableLegacyImportPreservesBytesAndDoesNotReplayStartedWork(t *testing.T) {
	ctx := context.Background()
	db, store, _, scope := durableFixture(t)
	created := time.Now().UTC().Add(-time.Hour)
	for _, row := range []models.ChatPendingTurn{
		{ID: "old-active", Surface: scope.Surface, SessionKey: scope.Key, Started: true, Payload: []byte(`{"original":"active"}`), CreatedAt: created},
		{ID: "old-waiter", Surface: scope.Surface, SessionKey: scope.Key, Position: 1, Payload: []byte(`{"original":"queued"}`), CreatedAt: created},
		{ID: "unreadable", Surface: scope.Surface, SessionKey: scope.Key, Position: 2, Payload: []byte{}},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	imported, err := store.ImportLegacy(ctx, scope, "owner")
	if err != nil || len(imported) != 3 || imported[0].State != TurnUnknown || imported[1].State != TurnQueued || imported[2].State != TurnUnknown || !imported[0].CreatedAt.Equal(created) {
		t.Fatalf("legacy import lost intent or invented safe replay: %+v %v", imported, err)
	}
	var count int64
	if err := db.Model(&models.ChatPendingTurn{}).Where("session_key = ?", scope.Key).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("successful import retained competing legacy queue authority")
	}
	if again, err := store.ImportLegacy(ctx, scope, "owner"); err != nil || len(again) != 0 {
		t.Fatalf("resuming migration duplicated original turns: %+v %v", again, err)
	}
	lease := acquireQueue(t, store, scope)
	active, err := store.Claim(ctx, lease)
	if err != nil || active.ID != "old-waiter" || string(active.Payload) != `{"original":"queued"}` {
		t.Fatalf("started legacy action was replayed: %+v %v", active, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Admit(ctx, scope, queueIntent("late", "late"), 0, nil); !errors.Is(err, ErrQueueRetired) {
		t.Fatalf("closed session accepted new work: %v", err)
	}
	ready, err := store.ReadyScopes(ctx, scope.Surface)
	if err != nil || len(ready) != 0 {
		t.Fatal("retired queue was scheduled")
	}
	if cancelled, err := store.Cancellation(ctx, lease, active.ID); err != nil || !cancelled {
		t.Fatal("close did not notify its active executor")
	}
}
