package chatsession

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestDurableQueueGuardOpenAllowsAcceptedWorkWithoutLoadingItsBody(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	admitQueue(t, store, scope, "first accepted command", "first")
	admitQueue(t, store, scope, "queued command", "second")
	lease := acquireQueue(t, store, scope)
	active, err := store.Claim(context.Background(), lease)
	if err != nil || active == nil {
		t.Fatalf("claim: %+v %v", active, err)
	}
	var before models.ChatQueue
	if err := db.First(&before, "id = ?", scope.ID()).Error; err != nil {
		t.Fatal(err)
	}
	// A temporarily unavailable body must not be loaded by metadata readiness.
	missing := "fileref:v1/bytes/" + strings.Repeat("e", 64)
	if err := db.Exec("UPDATE chat_queue_turns SET payload = ? WHERE queue_id = ?", missing, scope.ID()).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.GuardOpen(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	var after models.ChatQueue
	if err := db.First(&after, "id = ?", scope.ID()).Error; err != nil || after.Owner != before.Owner || after.Generation != before.Generation || after.ActiveTurnID != before.ActiveTurnID || after.Retired || after.CancelRequested {
		t.Fatalf("guard changed existing execution authority: %+v %+v %v", before, after, err)
	}
	var bodies []string
	if err := db.Model(&models.ChatQueueTurn{}).Where("queue_id = ?", scope.ID()).Pluck("payload", &bodies).Error; err != nil || len(bodies) != 2 || bodies[0] != missing || bodies[1] != missing {
		t.Fatalf("guard rewrote accepted bodies: %+v %v", bodies, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Retire(tx, scope) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.GuardOpen(tx, scope) }); !errors.Is(err, ErrQueueRetired) {
		t.Fatalf("retired queue was reopened by publication: %v", err)
	}
}

func TestDurableQueueGuardOpenRejectsAutocommitBeforeAnySQL(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	queries := 0
	for _, register := range []func(string, func(*gorm.DB)) error{db.Callback().Query().Before("gorm:query").Register, db.Callback().Create().Before("gorm:create").Register, db.Callback().Update().Before("gorm:update").Register} {
		if err := register("test:guard_open_no_autocommit", func(*gorm.DB) { queries++ }); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("test:guard_open_no_autocommit")
		_ = db.Callback().Create().Remove("test:guard_open_no_autocommit")
		_ = db.Callback().Update().Remove("test:guard_open_no_autocommit")
	})
	if err := store.GuardOpen(db, scope); err == nil || queries != 0 {
		t.Fatalf("guard used autocommit SQL: queries=%d err=%v", queries, err)
	}
}
