package chatsession

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestQueueLineageRequiresFreshPrivateProofAtComposedAdmission(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := t.Context()
	if untouched, err := store.PrepareExecutionContext(ctx); err != nil || untouched != ctx {
		t.Fatal("unwired fixture changed its caller context", err)
	}
	controller, _, _ := queueController(t, db)
	store.SetExecutionController(controller)
	intent := QueueIntent{ID: "proof-bound-admission", ActorID: "alice", ClientMessageID: "private-proof-client", Fingerprint: strings.Repeat("a", 64), Payload: []byte("original body")}
	prepared, err := store.PrepareIntent(ctx, scope, intent)
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { _, err := store.AdmitPreparedTx(tx, scope, prepared, 8, nil); return err })
	if !errors.Is(err, executionlineage.ErrUnavailable) {
		t.Fatal("composed admission manufactured its own proof inside SQL", err)
	}
	var count int64
	if err := db.Model(&models.ChatQueueTurn{}).Where("id = ?", intent.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("unproved body became a live accepted ref", err)
	}
	ready, err := store.PrepareExecutionContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ready).Transaction(func(tx *gorm.DB) error { _, err := store.AdmitPreparedTx(tx, scope, prepared, 8, nil); return err }); err != nil {
		t.Fatal("explicit outside preparation was not consumable by the outer SQL transaction", err)
	}
	for _, guard := range []func(*gorm.DB) error{func(tx *gorm.DB) error { return store.GuardOpen(tx, scope) }, func(tx *gorm.DB) error { return store.GuardIdle(tx, scope) }, func(tx *gorm.DB) error { return store.Retire(tx, scope) }, func(tx *gorm.DB) error { return store.Reopen(tx, scope) }} {
		if err := db.Transaction(guard); !errors.Is(err, executionlineage.ErrUnavailable) {
			t.Fatal("metadata mutation bypassed a missing private execution proof", err)
		}
	}
}

func TestQueueLineageOnlyDefinitiveRevocationCancelsAcknowledgedOwner(t *testing.T) {
	for _, revoked := range []error{executionlineage.ErrInactive, executionlineage.ErrConflict, executionlineage.ErrUnbound, executionlineage.ErrQuarantined, executionlineage.ErrUninitialized} {
		mapped := queueExecutionError(revoked)
		if !errors.Is(mapped, ErrQueueOwnerLost) || !errors.Is(mapped, revoked) {
			t.Fatal("definite revocation failed to stop the old operation with its cause", mapped)
		}
	}
	transient := errors.New("temporary control object network failure")
	if queueExecutionError(transient) != transient || errors.Is(queueExecutionError(transient), ErrQueueOwnerLost) {
		t.Fatal("transient outage discarded the last acknowledged lease")
	}
	if queueExecutionError(nil) != nil {
		t.Fatal("successful renewal became a lost owner")
	}
}

func TestPendingLineageRejectsUnknownScopeAndTransactionBoundBodyIO(t *testing.T) {
	db, _, _, _ := durableFixture(t)
	pending := NewPendingStore(db)
	for _, scope := range []struct{ surface, key string }{{"unknown", "run|node"}, {"workflow.review", "missing-node"}, {"workflow.clarify", "|node"}, {"workflow.review", "run|"}, {"workflow.review", "missing-run|node"}} {
		if _, err := pending.pendingEntities(db, scope.surface, scope.key, nil); err == nil {
			t.Fatal("incomplete execution parent was inferred", scope)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		inside := NewPendingStore(tx)
		if _, err := inside.preparePending(context.Background(), []PendingTurn{{ID: "nested", Payload: []byte("never upload")}}); !errors.Is(err, ErrPayloadInTransaction) {
			return errors.New("pending body preparation accepted a transaction-bound database")
		}
		if _, err := inside.loadPending(context.Background(), "workflow.review"); !errors.Is(err, ErrPayloadInTransaction) {
			return errors.New("pending body loading accepted a transaction-bound database")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
