package chatsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestDurableQueueRepairsInconsistentActivePointersWithoutReplayingEffects(t *testing.T) {
	for _, scenario := range []string{"missing", "foreign", "queued", "terminal", "invalid", "orphan"} {
		t.Run(scenario, func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			ctx := context.Background()
			original := admitQueue(t, store, scope, "uncertain external effect", "original")
			next := admitQueue(t, store, scope, "next explicit intent", "next")
			oldLease := acquireQueue(t, store, scope)
			if _, err := store.Claim(ctx, oldLease); err != nil {
				t.Fatal(err)
			}
			if err := store.Dispatch(ctx, oldLease, original.Turn.ID); err != nil {
				t.Fatal(err)
			}

			foreignScope := QueueScope{Surface: scope.Surface, Key: "other-session"}
			foreign := admitQueue(t, store, foreignScope, "unrelated live command", "foreign")
			foreignLease := acquireQueue(t, store, foreignScope)
			if _, err := store.Claim(ctx, foreignLease); err != nil {
				t.Fatal(err)
			}
			if err := store.Dispatch(ctx, foreignLease, foreign.Turn.ID); err != nil {
				t.Fatal(err)
			}
			var err error
			switch scenario {
			case "missing":
				err = db.Where("id = ?", original.Turn.ID).Delete(&models.ChatQueueTurn{}).Error
			case "foreign":
				err = db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("active_turn_id", foreign.Turn.ID).Error
			case "queued":
				err = db.Model(&models.ChatQueueTurn{}).Where("id = ?", original.Turn.ID).Update("state", TurnQueued).Error
			case "terminal":
				err = db.Model(&models.ChatQueueTurn{}).Where("id = ?", original.Turn.ID).
					Updates(map[string]any{"state": TurnSucceeded, "reason": "saved_result", "result_id": "original-result"}).Error
			case "invalid":
				err = db.Model(&models.ChatQueueTurn{}).Where("id = ?", original.Turn.ID).Update("state", "partially_migrated").Error
			case "orphan":
				err = db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("active_turn_id", "").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			lease, _, err := remote.Acquire(ctx, scope, uuid.NewString())
			if err != nil {
				t.Fatalf("stale pointer permanently blocked takeover: %v", err)
			}
			snapshot, err := remote.Snapshot(ctx, scope)
			if err != nil || snapshot.Active != nil || len(snapshot.Pending) != 1 || snapshot.Pending[0].ID != next.Turn.ID {
				t.Fatalf("recovery retained inconsistent or replayable work: %+v %v", snapshot, err)
			}
			claimed, err := remote.Claim(ctx, lease)
			if err != nil || claimed == nil || claimed.ID != next.Turn.ID {
				t.Fatalf("recovery replayed uncertain effects: %+v %v", claimed, err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return store.Fence(tx, oldLease, original.Turn.ID) }); !errors.Is(err, ErrQueueOwnerLost) {
				t.Fatalf("old worker was not fenced: %v", err)
			}
			unrelated, err := store.Receipt(ctx, foreignScope, foreign.Turn.ID)
			if err != nil || unrelated.State != TurnExecuting || unrelated.ExecutionEpoch != foreignLease.Generation || unrelated.DispatchedAt == nil {
				t.Fatalf("repair modified another conversation: %+v %v", unrelated, err)
			}
			if scenario != "missing" {
				retained, err := store.Receipt(ctx, scope, original.Turn.ID)
				wantState := TurnUnknown
				if scenario == "terminal" {
					wantState = TurnSucceeded
				}
				if err != nil || retained.State != wantState || retained.DispatchedAt == nil || (scenario == "terminal" && retained.ResultID != "original-result") {
					t.Fatalf("recovery lost dispatch/result evidence: %+v %v", retained, err)
				}
			}
		})
	}
}

func TestDurableQueueStaleActivePointerDoesNotBlockRetiredRecoveryOrReopen(t *testing.T) {
	for _, operation := range []string{"retired", "reopen"} {
		t.Run(operation, func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			ctx := context.Background()
			row := admitQueue(t, store, scope, "retained external effect", "original")
			lease := acquireQueue(t, store, scope)
			if _, err := store.Claim(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if err := store.Dispatch(ctx, lease, row.Turn.ID); err != nil {
				t.Fatal(err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Updates(map[string]any{
				"active_turn_id": "missing-active-receipt", "owner_expires_at": time.Now().Add(-time.Second),
			}).Error; err != nil {
				t.Fatal(err)
			}
			if operation == "retired" {
				if err := remote.ReconcileRetired(ctx, scope.Surface); err != nil {
					t.Fatal(err)
				}
			} else if err := db.Transaction(func(tx *gorm.DB) error { return remote.Reopen(tx, scope) }); err != nil {
				t.Fatal(err)
			}
			snapshot, err := remote.Snapshot(ctx, scope)
			if err != nil || snapshot.Active != nil || len(snapshot.Pending) != 0 || snapshot.Queue.Owner != "" {
				t.Fatalf("repair left an immortal active receipt: %+v %v", snapshot, err)
			}
			retained, err := store.Receipt(ctx, scope, row.Turn.ID)
			if err != nil || retained.State != TurnUnknown || retained.DispatchedAt == nil {
				t.Fatalf("retired/reopen repair made dispatch replayable: %+v %v", retained, err)
			}
		})
	}
}

func TestDurableQueueDeferredAndRecoveredPreparationPrecedesReorderedWaiters(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "deferred", true: "crashed"}[recovery], func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			ctx := context.Background()
			// Give the later active command a high original position, then
			// renumber pending items while that command is preparing.
			var last QueueReceipt
			for i := range 8 {
				last = admitQueue(t, store, scope, "previously settled", string(rune('a'+i)))
				if err := store.Remove(ctx, scope, last.Turn.ID); err != nil {
					t.Fatal(err)
				}
			}
			active := admitQueue(t, store, scope, "preparing first", "active")
			second := admitQueue(t, store, scope, "second", "second")
			third := admitQueue(t, store, scope, "third", "third")
			lease := acquireQueue(t, store, scope)
			if _, err := store.Claim(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if err := remote.Reorder(ctx, scope, []string{third.Turn.ID, second.Turn.ID}); err != nil {
				t.Fatal(err)
			}
			if recovery {
				if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
					t.Fatal(err)
				}
				var err error
				lease, _, err = remote.Acquire(ctx, scope, uuid.NewString())
				if err != nil {
					t.Fatal(err)
				}
			} else if err := store.DeferPreparation(ctx, lease, active.Turn.ID); err != nil {
				t.Fatal(err)
			}
			snapshot, err := remote.Snapshot(ctx, scope)
			if err != nil || len(snapshot.Pending) != 3 || snapshot.Pending[0].ID != active.Turn.ID || snapshot.Pending[1].ID != third.Turn.ID || snapshot.Pending[2].ID != second.Turn.ID {
				t.Fatalf("preparation moved behind reordered work: %+v %v", snapshot, err)
			}
			claimed, err := remote.Claim(ctx, lease)
			if err != nil || claimed == nil || claimed.ID != active.Turn.ID || claimed.DispatchedAt != nil {
				t.Fatalf("preparation did not resume first: %+v %v", claimed, err)
			}
		})
	}
}

func TestDurableQueueRenewUntilReturnsOnlyCommittedDeadline(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	admitQueue(t, store, scope, "original", "one")
	lease := acquireQueue(t, store, scope)
	deadline, err := store.RenewUntil(ctx, lease)
	if err != nil || deadline.Before(lease.ExpiresAt) {
		t.Fatalf("confirmed renewal did not extend deadline: %v %v", deadline, err)
	}
	var row models.ChatQueue
	if err := db.First(&row, "id = ?", scope.ID()).Error; err != nil || row.OwnerExpiresAt == nil || !row.OwnerExpiresAt.Equal(deadline) {
		t.Fatalf("returned deadline differs from durable lease: %+v %v", row.OwnerExpiresAt, err)
	}
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if deadline, err := store.RenewUntil(ctx, lease); !errors.Is(err, ErrQueueOwnerLost) || !deadline.IsZero() {
		t.Fatalf("unowned renewal returned an unconfirmed deadline: %v %v", deadline, err)
	}
}

func TestDurableQueueGenerationSurvivesQueueDeletionAndClockRollback(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	store.now, remote.now = func() time.Time { return now }, func() time.Time { return now }
	admitQueue(t, store, scope, "original receipt", "original")
	oldLease := acquireQueue(t, store, scope)
	if err := db.Where("queue_id = ?", scope.ID()).Delete(&models.ChatQueueTurn{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", scope.ID()).Delete(&models.ChatQueue{}).Error; err != nil {
		t.Fatal(err)
	}
	// A replaced queue and a backwards wall clock must not reuse the prior
	// generation, even when an executor identity is accidentally reused.
	now = now.Add(-time.Hour)
	admitQueue(t, remote, scope, "replacement receipt", "replacement")
	replacement, _, err := remote.Acquire(ctx, scope, oldLease.Owner)
	if err != nil || replacement.Generation <= oldLease.Generation {
		t.Fatalf("replacement reused old fence: old=%+v new=%+v %v", oldLease, replacement, err)
	}
	if err := store.Renew(ctx, oldLease); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("deleted queue's worker renewed replacement: %v", err)
	}
	watermark, err := remote.Watermark(ctx)
	if err != nil || watermark < replacement.Generation {
		t.Fatalf("replacement no longer respects global snapshot watermark: %d %v", watermark, err)
	}
}

func TestDurableQueueClaimNeverErasesDispatchProofFromAnInconsistentWaiter(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	ctx := context.Background()
	first := admitQueue(t, store, scope, "effect already dispatched", "one")
	second := admitQueue(t, store, scope, "next explicit intent", "two")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := store.Dispatch(ctx, lease, first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ChatQueueTurn{}).Where("id = ?", first.Turn.ID).Update("state", TurnQueued).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("active_turn_id", "").Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := remote.Claim(ctx, lease)
	if err != nil || claimed == nil || claimed.ID != second.Turn.ID {
		t.Fatalf("queued state erased previous dispatch proof: %+v %v", claimed, err)
	}
	retained, err := store.Receipt(ctx, scope, first.Turn.ID)
	if err != nil || retained.State != TurnUnknown || retained.DispatchedAt == nil || retained.Reason != "queued_dispatch_inconsistent" {
		t.Fatalf("dispatch evidence was not preserved: %+v %v", retained, err)
	}
}

func TestDurableQueueOrphanOnlyExecutionsRemainDiscoverableAndDoNotReplay(t *testing.T) {
	for _, operation := range []string{"acquire", "retired", "reopen"} {
		t.Run(operation, func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			ctx := context.Background()
			row := admitQueue(t, store, scope, "last uncertain effect", "one")
			lease := acquireQueue(t, store, scope)
			if _, err := store.Claim(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if err := store.Dispatch(ctx, lease, row.Turn.ID); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("active_turn_id", "").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Transaction(func(tx *gorm.DB) error { return remote.Reopen(tx, scope) }); !errors.Is(err, ErrQueueChanged) {
				t.Fatalf("reopen stole live orphan execution: %v", err)
			}
			if err := store.Abandon(ctx, lease); err != nil {
				t.Fatal(err)
			}
			if scopes, err := remote.ReadyScopes(ctx, scope.Surface); err != nil || len(scopes) != 1 || scopes[0] != scope {
				t.Fatalf("orphan-only queue disappeared from recovery: %+v %v", scopes, err)
			}
			switch operation {
			case "acquire":
				acquireQueue(t, remote, scope)
			case "retired":
				if err := db.Transaction(func(tx *gorm.DB) error { return remote.Retire(tx, scope) }); err != nil {
					t.Fatal(err)
				}
				if err := remote.ReconcileRetired(ctx, scope.Surface); err != nil {
					t.Fatal(err)
				}
			case "reopen":
				if err := db.Transaction(func(tx *gorm.DB) error { return remote.Reopen(tx, scope) }); err != nil {
					t.Fatal(err)
				}
			}
			if receipt, err := store.Receipt(ctx, scope, row.Turn.ID); err != nil || receipt.State != TurnUnknown || receipt.DispatchedAt == nil {
				t.Fatalf("orphan execution was made replayable: %+v %v", receipt, err)
			}
			if snapshot, err := store.SnapshotMetadata(ctx, scope); err != nil || snapshot.Active != nil || len(snapshot.Pending) != 0 {
				t.Fatalf("orphan left a permanently inconsistent view: %+v %v", snapshot, err)
			}
		})
	}
}
