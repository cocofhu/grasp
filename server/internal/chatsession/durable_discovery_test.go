package chatsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestDurableQueueDiscoverySkipsLiveOwnersWithoutMutatingTheirRevision(t *testing.T) {
	db, store, remote, scope := durableFixture(t)
	ctx := context.Background()
	admitQueue(t, store, scope, "queued command", "one")
	lease := acquireQueue(t, store, scope)
	var before models.ChatQueue
	if err := db.First(&before, "id = ?", scope.ID()).Error; err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if scopes, err := remote.ReadyScopes(ctx, scope.Surface); err != nil || len(scopes) != 0 {
			t.Fatalf("discovery competes with a live executor: %+v %v", scopes, err)
		}
	}
	var after models.ChatQueue
	if err := db.First(&after, "id = ?", scope.ID()).Error; err != nil || before.Revision != after.Revision {
		t.Fatalf("discovery wrote a live queue: before=%d after=%d %v", before.Revision, after.Revision, err)
	}
	if err := store.Abandon(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if scopes, err := remote.ReadyScopes(ctx, scope.Surface); err != nil || len(scopes) != 1 || scopes[0] != scope {
		t.Fatalf("discovery lost a crashed owner's accepted work: %+v %v", scopes, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	if scopes, err := remote.ReadyScopes(ctx, scope.Surface); err != nil || len(scopes) != 0 {
		t.Fatalf("discovery scheduled retired work: %+v %v", scopes, err)
	}
}

func TestDurableQueueGuardIdleSerializesWithAdmission(t *testing.T) {
	for _, admissionFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop_first", true: "admit_first"}[admissionFirst], func(t *testing.T) {
			db, store, remote, scope := durableFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := db.Create(&models.Setting{Key: "desired-state", Value: "running"}).Error; err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			stopResult, admitResult := make(chan error, 1), make(chan error, 1)
			stopped := errors.New("environment already stopped")
			admit := func(block bool) {
				_, err := store.Admit(ctx, scope, queueIntent("one explicit native request", "one"), 0, func(tx *gorm.DB) error {
					if block {
						close(entered)
						<-release
					}
					var state models.Setting
					if err := tx.First(&state, "key = ?", "desired-state").Error; err != nil {
						return err
					}
					if state.Value == "stopped" {
						return stopped
					}
					return nil
				})
				admitResult <- err
			}
			stop := func(block bool) {
				err := remote.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err := remote.GuardIdle(tx, scope); err != nil {
						return err
					}
					if block {
						close(entered)
						<-release
					}
					return tx.Model(&models.Setting{}).Where("key = ?", "desired-state").Update("value", "stopped").Error
				})
				stopResult <- err
			}
			if admissionFirst {
				go admit(true)
			} else {
				go stop(true)
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("first transaction never acquired queue lock")
			}
			if admissionFirst {
				go stop(false)
			} else {
				go admit(false)
			}
			close(release)
			var admitErr, stopErr error
			select {
			case admitErr = <-admitResult:
			case <-ctx.Done():
				t.Fatal("admission remained blocked")
			}
			select {
			case stopErr = <-stopResult:
			case <-ctx.Done():
				t.Fatal("stop remained blocked")
			}
			if admissionFirst && (admitErr != nil || !errors.Is(stopErr, ErrQueueBusy)) {
				t.Fatalf("stop bypassed accepted work: admit=%v stop=%v", admitErr, stopErr)
			}
			if !admissionFirst && (stopErr != nil || !errors.Is(admitErr, stopped)) {
				t.Fatalf("message bypassed committed stopped state: admit=%v stop=%v", admitErr, stopErr)
			}
		})
	}
}

func TestDurableQueueGuardIdleRejectsUntrackedExecutionsAndRetiredScopes(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	row := admitQueue(t, store, scope, "original accepted command", "one")
	if err := db.Model(&models.ChatQueueTurn{}).Where("id = ?", row.Turn.ID).Update("state", TurnExecuting).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.GuardIdle(tx, scope) }); !errors.Is(err, ErrQueueBusy) {
		t.Fatalf("orphan execution appeared idle: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.GuardIdle(tx, scope) }); !errors.Is(err, ErrQueueRetired) {
		t.Fatalf("retired queue was admitted for environment mutation: %v", err)
	}
	if err := db.Where("id = ?", scope.ID()).Delete(&models.ChatQueue{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cancellation(ctx, QueueLease{Scope: scope}, row.Turn.ID); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("deleted queue was treated as a transient cancellation read: %v", err)
	}
}
