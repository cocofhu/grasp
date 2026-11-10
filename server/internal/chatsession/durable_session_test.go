package chatsession

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type durableTestTurn struct {
	ID   string
	Text string
}

func durableTestConfig(execute func(context.Context, durableTestTurn) (bool, error)) DurableConfig[durableTestTurn] {
	view := func(turn durableTestTurn) ItemView { return ItemView{ID: turn.ID, Text: turn.Text} }
	return DurableConfig[durableTestTurn]{Config: Config[durableTestTurn]{View: view, Execute: execute,
		Publish: func(string, map[string]any) {}}, Decode: JSONDecoder(view), Intent: func(turn durableTestTurn) (QueueIntent, error) {
		intent := queueIntent(turn.Text, turn.ID)
		intent.ID = turn.ID
		intent.Payload, _ = json.Marshal(turn)
		return intent, nil
	}}
}

func waitDurable(t *testing.T, label string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out: " + label)
}

func TestDurableSessionRequiresCommittedPreparationFramesBeforeNativeDispatch(t *testing.T) {
	for _, refused := range []string{EventQueueState, EventTurnBegin} {
		t.Run(refused, func(t *testing.T) {
			_, store, _, scope := durableFixture(t)
			var calls atomic.Int32
			cfg := durableTestConfig(func(context.Context, durableTestTurn) (bool, error) { calls.Add(1); return false, nil })
			cfg.PublishExecution = func(ctx context.Context, event string, _ map[string]any) error {
				if _, ok := CurrentExecution(ctx); !ok {
					t.Error("preparation publication lost its owner")
				}
				if event == refused {
					return errors.New("fixture publication refused")
				}
				return nil
			}
			session := NewDurableSession(store, scope, cfg)
			receipt, err := session.EnqueueReceipt(durableTestTurn{ID: "refused", Text: "accepted intent"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			waitDurable(t, "failed before dispatch", func() bool {
				row, err := store.Receipt(context.Background(), scope, receipt.Turn.ID)
				return err == nil && row.State == TurnFailed && row.DispatchedAt == nil
			})
			if calls.Load() != 0 {
				t.Fatal("native work started before its preparation frames committed")
			}
		})
	}
}

func TestDurableSessionTerminalNotificationFailureDoesNotReplaySavedWork(t *testing.T) {
	_, store, _, scope := durableFixture(t)
	var calls atomic.Int32
	notified := make(chan struct{}, 1)
	cfg := durableTestConfig(func(context.Context, durableTestTurn) (bool, error) { calls.Add(1); return false, nil })
	cfg.PublishExecution = func(_ context.Context, event string, _ map[string]any) error {
		if event == EventTurnDone {
			notified <- struct{}{}
			return errors.New("fixture terminal notification unavailable")
		}
		return nil
	}
	session := NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{ID: "saved", Text: "once"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal notification missing")
	}
	session.Wake()
	waitDurable(t, "completed owner released", func() bool { return session.Idle() })
	row, err := store.Receipt(context.Background(), scope, "saved")
	if err != nil || row.State != TurnSucceeded || calls.Load() != 1 {
		t.Fatalf("notification failure changed a saved result: %+v %v", row, err)
	}
}

func TestDurableSessionTwoExecutorsUseOneOrderedNativeLane(t *testing.T) {
	db, first, second, scope := durableFixture(t)
	var inFlight, peak atomic.Int32
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	started := make(chan string, 4)
	execute := func(ctx context.Context, turn durableTestTurn) (bool, error) {
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		active := inFlight.Add(1)
		defer inFlight.Add(-1)
		for previous := peak.Load(); active > previous && !peak.CompareAndSwap(previous, active); previous = peak.Load() {
		}
		mu.Lock()
		order = append(order, turn.Text)
		mu.Unlock()
		started <- turn.Text
		if turn.ID == "first" {
			<-release
		}
		return false, db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&models.Setting{Key: turn.ID, Value: "committed"}).Error; err != nil {
				return err
			}
			return FinishExecution(ctx, tx, TurnSucceeded, "", turn.ID)
		})
	}
	a := NewDurableSession(first, scope, durableTestConfig(execute))
	b := NewDurableSession(second, scope, durableTestConfig(execute))
	if _, err := a.Enqueue(durableTestTurn{"first", "one"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("native execution did not start")
	}
	if _, err := b.Enqueue(durableTestTurn{"second", "two"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Enqueue(durableTestTurn{"third", "three"}, nil); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := b.SnapshotChecked(); err != nil || !snapshot.Busy || snapshot.Waiting != 2 || snapshot.ActiveItem["id"] != "first" {
		t.Fatalf("replica did not read durable authority: %+v %v", snapshot, err)
	}
	close(release)
	waitDurable(t, "all results", func() bool { return a.Idle() && b.Idle() })
	mu.Lock()
	defer mu.Unlock()
	if peak.Load() != 1 || len(order) != 3 || order[0] != "one" || order[1] != "two" || order[2] != "three" {
		t.Fatalf("native lane overlap/order changed: peak=%d order=%q", peak.Load(), order)
	}
	var count int64
	if err := db.Model(&models.Setting{}).Where("key IN ?", []string{"first", "second", "third"}).Count(&count).Error; err != nil || count != 3 {
		t.Fatal("turn completion was published without its result")
	}
}

func TestDurableSessionTargetedCancelIsJoinedBeforeNextNativeTurn(t *testing.T) {
	db, store, other, scope := durableFixture(t)
	started, cancelStarted, cancelRelease := make(chan string, 4), make(chan struct{}), make(chan struct{})
	cfg := durableTestConfig(func(ctx context.Context, turn durableTestTurn) (bool, error) {
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		started <- turn.ID
		if turn.ID == "first" {
			<-ctx.Done()
			return true, ctx.Err()
		}
		return false, db.Transaction(func(tx *gorm.DB) error { return FinishExecution(ctx, tx, TurnSucceeded, "", "") })
	})
	cfg.CancelTurn = func() { close(cancelStarted); <-cancelRelease }
	session := NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{"first", "stop me"}, nil); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := session.Enqueue(durableTestTurn{"second", "keep me"}, nil); err != nil {
		t.Fatal(err)
	}
	if matched, err := other.Cancel(context.Background(), scope, "wrong", true); err != nil || matched {
		t.Fatal("old viewer canceled current work")
	}
	if matched, err := other.Cancel(context.Background(), scope, "first", false); err != nil || !matched {
		t.Fatal("remote cancellation was not accepted")
	}
	select {
	case <-cancelStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not observe remote cancellation")
	}
	select {
	case next := <-started:
		t.Fatalf("native cancellation raced the next turn: %s", next)
	default:
	}
	close(cancelRelease)
	select {
	case next := <-started:
		if next != "second" {
			t.Fatal("wrong queued turn resumed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting turn was lost after cancellation")
	}
	waitDurable(t, "cancelled receipt durable", session.Idle)
	row, err := store.Receipt(context.Background(), scope, "first")
	if err != nil || row.State != TurnUnknown {
		t.Fatalf("unacknowledged cancelled dispatch claimed a known result: %+v %v", row, err)
	}
}

func TestDurableSessionLocalCancellationWakesOnlyAfterCommit(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	started, cancelled := make(chan struct{}), make(chan struct{}, 1)
	var refuse atomic.Bool
	if err := db.Callback().Update().Before("gorm:update").Register("test:reject_local_cancel", func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "chat_queues" && values["cancel_requested"] == true && refuse.Load() {
			_ = tx.AddError(errors.New("cancel intent did not commit"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:reject_local_cancel") })
	cfg := durableTestConfig(func(ctx context.Context, _ durableTestTurn) (bool, error) {
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		close(started)
		<-ctx.Done()
		return true, ctx.Err()
	})
	var session *DurableSession[durableTestTurn]
	cfg.CancelTurn = func() {
		unlocked := make(chan struct{})
		go func() { session.mu.Lock(); session.mu.Unlock(); close(unlocked) }()
		select {
		case <-unlocked:
		case <-time.After(250 * time.Millisecond):
			t.Error("native cancellation ran while holding the session mutex")
		}
		var row models.ChatQueue
		if err := db.First(&row, "id = ?", scope.ID()).Error; err != nil || !row.CancelRequested {
			t.Errorf("native cancellation preceded its SQL commit: %+v %v", row, err)
		}
		cancelled <- struct{}{}
	}
	session = NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{"local-cancel", "cancel me"}, nil); err != nil {
		t.Fatal(err)
	}
	<-started
	refuse.Store(true)
	if _, err := session.CancelChecked("local-cancel", false); err == nil {
		t.Fatal("failed cancellation commit was acknowledged")
	}
	select {
	case <-cancelled:
		t.Fatal("failed cancellation intent reached native execution")
	case <-time.After(30 * time.Millisecond):
	}
	refuse.Store(false)
	if matched, err := session.CancelChecked("local-cancel", false); err != nil || !matched {
		t.Fatalf("local cancellation was not committed: %v %v", matched, err)
	}
	select {
	case <-cancelled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("same-replica cancellation waited for the one-second fallback")
	}
	waitDurable(t, "local cancelled turn settled", session.Idle)
}

func TestDurableSessionRemoteCancellationUsesOneSecondAuthorityRead(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	started, cancelled := make(chan struct{}), make(chan struct{}, 1)
	var active atomic.Bool
	var reads atomic.Int32
	if err := db.Callback().Query().Before("gorm:query").Register("test:count_cancel_poll", func(tx *gorm.DB) {
		if active.Load() && tx.Statement.Table == "chat_queues" {
			reads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:count_cancel_poll") })
	cfg := durableTestConfig(func(ctx context.Context, _ durableTestTurn) (bool, error) {
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		active.Store(true)
		close(started)
		<-ctx.Done()
		active.Store(false)
		return true, ctx.Err()
	})
	cfg.CancelTurn = func() { cancelled <- struct{}{} }
	session := NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{"remote-cancel", "wait for another replica"}, nil); err != nil {
		t.Fatal(err)
	}
	<-started
	time.Sleep(250 * time.Millisecond)
	if count := reads.Load(); count != 0 {
		t.Fatalf("active native execution still polls cancellation faster than one second: %d reads", count)
	}
	if matched, err := remote.Cancel(context.Background(), scope, "remote-cancel", false); err != nil || !matched {
		t.Fatalf("remote cancellation was not accepted: %v %v", matched, err)
	}
	select {
	case <-cancelled:
	case <-time.After(1300 * time.Millisecond):
		t.Fatal("the one-second cross-replica fallback did not observe cancellation")
	}
	if count := reads.Load(); count != 1 {
		t.Fatalf("a remote cancellation should need one authority read: %d", count)
	}
	waitDurable(t, "remote cancelled turn settled", session.Idle)
}

func TestDurableSessionLocalCancelJoinsNativeStopBeforeNextTurnAndRejectsOldTarget(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	started := make(chan string, 2)
	cancelStarted, releaseCancel, releaseSecond := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var cancels atomic.Int32
	cfg := durableTestConfig(func(ctx context.Context, turn durableTestTurn) (bool, error) {
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		started <- turn.ID
		if turn.ID == "first" {
			<-ctx.Done()
			return true, ctx.Err()
		}
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-releaseSecond:
			return false, db.Transaction(func(tx *gorm.DB) error { return FinishExecution(ctx, tx, TurnSucceeded, "", "") })
		}
	})
	cfg.CancelTurn = func() {
		if cancels.Add(1) == 1 {
			close(cancelStarted)
			<-releaseCancel
		}
	}
	session := NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{"first", "stop this one"}, nil); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := session.Enqueue(durableTestTurn{"second", "retain this one"}, nil); err != nil {
		t.Fatal(err)
	}
	if matched, err := session.CancelChecked("first", false); err != nil || !matched {
		t.Fatalf("local stop was not accepted: %v %v", matched, err)
	}
	select {
	case <-cancelStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("local watcher was not signalled")
	}
	select {
	case next := <-started:
		t.Fatalf("next turn started before native stop returned: %s", next)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseCancel)
	select {
	case next := <-started:
		if next != "second" {
			t.Fatalf("wrong next turn: %s", next)
		}
	case <-time.After(time.Second):
		t.Fatal("retained queued turn did not start after native stop joined")
	}
	if matched, err := session.CancelChecked("first", false); err != nil || matched {
		t.Fatalf("old cancellation targeted the new turn: %v %v", matched, err)
	}
	if cancels.Load() != 1 {
		t.Fatal("an old cancellation invoked native stop for the next turn")
	}
	close(releaseSecond)
	waitDurable(t, "next turn retained after old cancellation", session.Idle)
	row, err := store.Receipt(context.Background(), scope, "second")
	if err != nil || row.State != TurnSucceeded {
		t.Fatalf("old cancellation changed the next receipt: %+v %v", row, err)
	}
}

func TestDurableSessionPassiveViewAndUnreadableRequestNeverDispatch(t *testing.T) {
	_, store, _, scope := durableFixture(t)
	var calls atomic.Int32
	var ready atomic.Bool
	cfg := durableTestConfig(func(context.Context, durableTestTurn) (bool, error) { calls.Add(1); return false, nil })
	cfg.Ready = func() (bool, error) { return ready.Load(), nil }
	session := NewDurableSession(store, scope, cfg)
	intent := queueIntent("opaque-corrupt-payload", "broken")
	intent.ID = "bad-message"
	if _, err := store.Admit(context.Background(), scope, intent, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := session.SnapshotChecked(); err == nil {
		t.Fatal("unreadable request appeared as an empty queue")
	}
	if !session.Snapshot().Busy || session.Ready() {
		t.Fatal("failed read declared execution idle")
	}
	session.Wake()
	waitDurable(t, "readiness wait", func() bool { session.mu.Lock(); defer session.mu.Unlock(); return !session.pumping })
	if calls.Load() != 0 {
		t.Fatal("passive view woke stopped environment")
	}
	ready.Store(true)
	session.Wake()
	waitDurable(t, "bad input retained as unknown", func() bool {
		row, err := store.Receipt(context.Background(), scope, "bad-message")
		return err == nil && row.State == TurnUnknown && session.Idle()
	})
	if calls.Load() != 0 {
		t.Fatal("corrupt input reached a native command")
	}
}

func TestExecutionFenceAndDispatchDoNotReviveCancelledWork(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	row := admitQueue(t, store, scope, "one", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	ctx := executionContext(context.Background(), Execution{Store: store, Lease: lease, TurnID: row.Turn.ID})
	if err := CheckExecution(ctx); err != nil {
		t.Fatal(err)
	}
	if cancelled, err := ExecutionCancellation(ctx, db); err != nil || cancelled {
		t.Fatalf("uncancelled turn changed: %v %v", cancelled, err)
	}
	if _, err := store.Cancel(context.Background(), scope, row.Turn.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := DispatchExecution(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel during preparation still dispatched native work: %v", err)
	}
	if cancelled, err := ExecutionCancellation(ctx, db); err != nil || !cancelled {
		t.Fatalf("durable stop disappeared: %v %v", cancelled, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := FenceExecution(ctx, tx); err != nil {
			return err
		}
		return FinishExecution(ctx, tx, TurnCancelled, "user_cancelled", "")
	}); err != nil {
		t.Fatal(err)
	}
	if err := DispatchExecution(context.Background()); err != nil {
		t.Fatal("non-queue runtime surface was changed")
	}
	if err := FenceExecution(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := FinishExecution(context.Background(), db, TurnSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := CheckExecution(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cancelled, err := ExecutionCancellation(context.Background(), db); err != nil || cancelled {
		t.Fatal("non-queue compatibility changed")
	}
}

func TestDurableSessionDefersUnsentWorkInPlaceAndRetriesAfterReadiness(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	var ready atomic.Bool
	ready.Store(true)
	var attempts, native atomic.Int32
	cfg := durableTestConfig(func(ctx context.Context, turn durableTestTurn) (bool, error) {
		if attempts.Add(1) == 1 {
			ready.Store(false)
			return false, ErrPreparationDeferred
		}
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		native.Add(1)
		return false, db.Transaction(func(tx *gorm.DB) error { return FinishExecution(ctx, tx, TurnSucceeded, "", turn.ID) })
	})
	cfg.Ready = func() (bool, error) { return ready.Load(), nil }
	session := NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{"first", "keep exact intent"}, nil); err != nil {
		t.Fatal(err)
	}
	waitDurable(t, "preparation deferred", func() bool {
		session.mu.Lock()
		pumping := session.pumping
		session.mu.Unlock()
		return !pumping && session.Snapshot().Waiting == 1
	})
	row, err := store.Receipt(context.Background(), scope, "first")
	if err != nil || row.State != TurnQueued || row.DispatchedAt != nil || native.Load() != 0 {
		t.Fatalf("unsent command changed: %+v %v", row, err)
	}
	ready.Store(true)
	session.Wake()
	waitDurable(t, "retry completed", session.Idle)
	if attempts.Load() != 2 || native.Load() != 1 {
		t.Fatalf("wrong retry boundary: preparation=%d native=%d", attempts.Load(), native.Load())
	}
}

func TestDurableSessionRenewalLossCancelsAndFencesLateResult(t *testing.T) {
	db, store, other, scope := durableFixture(t)
	store.leaseTTL = 90 * time.Millisecond
	started, exited := make(chan struct{}), make(chan struct{})
	var failRenew atomic.Bool
	failure := errors.New("database renewal unavailable")
	if err := db.Callback().Update().Before("gorm:update").Register("test:renewal_loss", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queues" && failRenew.Load() {
			if values, ok := tx.Statement.Dest.(map[string]any); ok {
				if _, renewing := values["owner_expires_at"]; renewing && len(values) == 1 {
					_ = tx.AddError(failure)
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:renewal_loss") })
	cfg := durableTestConfig(func(ctx context.Context, turn durableTestTurn) (bool, error) {
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		failRenew.Store(true)
		close(started)
		<-ctx.Done()
		defer close(exited)
		if !errors.Is(context.Cause(ctx), ErrQueueOwnerLost) {
			return false, errors.New("renewal failure did not cancel ownership")
		}
		return false, context.Cause(ctx)
	})
	session := NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{"first", "external effect"}, nil); err != nil {
		t.Fatal(err)
	}
	<-started
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("renewal loss left native execution running")
	}
	failRenew.Store(false)
	waitDurable(t, "executor exits", func() bool { session.mu.Lock(); defer session.mu.Unlock(); return !session.pumping })
	// The rejected result stays executing until another owner explicitly
	// classifies its dispatched receipt. It must never be replayed.
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	lease, recovered, err := other.Acquire(context.Background(), scope, uuid.NewString())
	if err != nil || recovered == nil || recovered.State != TurnUnknown {
		t.Fatalf("unsafe recovery after lost renewal: %+v %v", recovered, err)
	}
	if claimed, err := other.Claim(context.Background(), lease); err != nil || claimed != nil {
		t.Fatalf("replayed uncertain external effect: %+v %v", claimed, err)
	}
}

func TestDurableSessionTransientRenewalAndCancelReadFailuresDoNotAbortNativeWork(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	store.leaseTTL = 900 * time.Millisecond
	var active atomic.Bool
	var renewals, failedReads atomic.Int32
	if err := db.Callback().Update().Before("gorm:update").Register("test:transient_durable_renewal", func(tx *gorm.DB) {
		if tx.Statement.Table != "chat_queues" || !active.Load() {
			return
		}
		if values, ok := tx.Statement.Dest.(map[string]any); ok && len(values) == 1 {
			if _, ok := values["owner_expires_at"]; ok && renewals.Add(1) == 1 {
				_ = tx.AddError(errors.New("transient renewal connection failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:transient_cancellation_read", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queues" && active.Load() && failedReads.Add(1) == 1 {
			_ = tx.AddError(errors.New("transient cancellation read failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove("test:transient_durable_renewal")
		_ = db.Callback().Query().Remove("test:transient_cancellation_read")
	})
	started, release, exited := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	cfg := durableTestConfig(func(ctx context.Context, _ durableTestTurn) (bool, error) {
		if err := DispatchExecution(ctx); err != nil {
			return false, err
		}
		active.Store(true)
		close(started)
		select {
		case <-ctx.Done():
			exited <- context.Cause(ctx)
			return false, context.Cause(ctx)
		case <-release:
			active.Store(false)
			exited <- nil
			return false, db.Transaction(func(tx *gorm.DB) error { return FinishExecution(ctx, tx, TurnSucceeded, "", "") })
		}
	})
	session := NewDurableSession(store, scope, cfg)
	if _, err := session.Enqueue(durableTestTurn{"transient", "keep native execution alive"}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("native work did not start")
	}
	select {
	case err := <-exited:
		t.Fatalf("a transient database error aborted native work: %v", err)
	case <-time.After(2100 * time.Millisecond):
	}
	if renewals.Load() < 2 || failedReads.Load() < 2 {
		t.Fatal("test did not exercise renewal retry and failed cancellation reads")
	}
	close(release)
	if err := <-exited; err != nil {
		t.Fatal(err)
	}
	waitDurable(t, "transient failures recovered", session.Idle)
	row, err := store.Receipt(context.Background(), scope, "transient")
	if err != nil || row.State != TurnSucceeded {
		t.Fatalf("recoverable outage lost the completed receipt: %+v %v", row, err)
	}
}

func TestDurableSessionRemoteQueueControlsAndPanicReceipt(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	var ready atomic.Bool
	var mu sync.Mutex
	var published []string
	cfg := durableTestConfig(func(context.Context, durableTestTurn) (bool, error) { panic("native setup panic") })
	cfg.Ready = func() (bool, error) { return ready.Load(), nil }
	cfg.FrameExtra = map[string]any{"source": "test"}
	cfg.TurnBeginExtra = func(durableTestTurn) map[string]any { return map[string]any{"phase": "preparing"} }
	cfg.ErrorExtra = func(error) map[string]any { return map[string]any{"code": "execution_failed"} }
	cfg.AfterTurn = func(durableTestTurn) {}
	cfg.Publish = func(event string, payload map[string]any) {
		mu.Lock()
		published = append(published, event)
		mu.Unlock()
	}
	session := NewDurableSession(store, scope, cfg)
	viewer := NewDurableSession(remote, scope, cfg)
	for _, id := range []string{"one", "two", "three"} {
		if _, err := session.Enqueue(durableTestTurn{id, id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := viewer.Reorder([]string{"three", "two", "one"}); err != nil {
		t.Fatal(err)
	}
	if err := viewer.Reorder([]string{"one"}); err == nil {
		t.Fatal("stale reorder was accepted")
	}
	if err := viewer.Remove("two"); err != nil {
		t.Fatal(err)
	}
	if err := viewer.Remove("two"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed receipt became queued again: %v", err)
	}
	view := viewer.View()
	if view.HasActive || len(view.Pending) != 2 || view.Pending[0].ID != "three" {
		t.Fatalf("remote view disagrees: %+v", view)
	}
	if _, active := viewer.Active(); active || viewer.CancelActive("", false) || viewer.CancelActive("stale", true) || viewer.CancelRequested() {
		t.Fatal("idle controls affected another turn")
	}
	if _, err := session.Enqueue(durableTestTurn{"denied", "denied"}, func(durableTestTurn, bool, []durableTestTurn) error { return ErrQueueFull }); !errors.Is(err, ErrQueueFull) {
		t.Fatal("admission policy ignored")
	}
	ready.Store(true)
	session.Wake()
	waitDurable(t, "panics durably settled", session.Idle)
	for _, id := range []string{"one", "three"} {
		row, err := store.Receipt(context.Background(), scope, id)
		if err != nil || row.State != TurnFailed {
			t.Fatalf("panic vanished: %+v %v", row, err)
		}
	}
	mu.Lock()
	events := append([]string(nil), published...)
	mu.Unlock()
	if !slices.Contains(events, EventError) {
		t.Fatal("committed failure was not surfaced")
	}
	viewer.Cancel(true)
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.Enqueue(durableTestTurn{"closed", "closed"}, nil); !errors.Is(err, ErrQueueRetired) {
		t.Fatal("retired conversation admitted a new command")
	}
}
