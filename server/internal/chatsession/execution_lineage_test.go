package chatsession

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func queueController(t *testing.T, db *gorm.DB) (*executionlineage.Controller, *executionlineage.Authority, string) {
	t.Helper()
	ctx := context.Background()
	local, err := objectstore.NewLocalControl(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	authority, err := executionlineage.Open(ctx, local)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	ticket, err := authority.Initialize(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := executionlineage.EnsureRecoverySchema(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := authority.QuarantineTx(tx, owner, ticket); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Activate(ctx, db, owner, ticket); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	controller, err := executionlineage.NewController(ctx, db, authority)
	if err != nil {
		t.Fatal(err)
	}
	return controller, authority, owner
}

func TestQueueLineageFencesClaimsRenewalDispatchAndPublicationWhileHistoryRemainsReadable(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	controller, authority, owner := queueController(t, db)
	store.SetExecutionController(controller)
	accepted := admitQueue(t, store, scope, "exact original accepted payload", "manual")
	lease := acquireQueue(t, store, scope)
	claimed, err := store.Claim(ctx, lease)
	if err != nil || claimed == nil || string(claimed.Payload) != "exact original accepted payload" {
		t.Fatal("current dataset could not execute explicit accepted work", err)
	}
	if err := store.Dispatch(ctx, lease, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RenewUntil(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	ticket, err := authority.BeginRestore(ctx, uuid.NewString(), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RenewUntil(ctx, lease); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatal("inactive lineage renewed an old execution", err)
	}
	if err := store.Dispatch(ctx, lease, claimed.ID); err == nil {
		t.Fatal("inactive lineage dispatched a native side effect")
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return store.Finish(tx, lease, claimed.ID, TurnSucceeded, "", "late-reply") }); err == nil {
		t.Fatal("unprepared or revoked authority published a late native result")
	}
	if snapshot, err := store.SnapshotMetadata(ctx, scope); err != nil || snapshot.Active == nil || snapshot.Active.ID != accepted.Turn.ID {
		t.Fatal("execution barrier hid observational history", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := authority.QuarantineTx(tx, owner, ticket); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Activate(ctx, db, owner, ticket); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Acquire(ctx, scope, uuid.NewString()); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatal("old process automatically adopted the restored active dataset", err)
	}
	restarted, err := executionlineage.NewController(ctx, db, authority)
	if err != nil {
		t.Fatal(err)
	}
	store.SetExecutionController(restarted)
	if _, _, err := store.Acquire(ctx, scope, uuid.NewString()); !errors.Is(err, executionlineage.ErrQuarantined) {
		t.Fatal("restarted worker replayed a restored dispatch without review", err)
	}
	newScope := QueueScope{Surface: "solo", Key: uuid.NewString()}
	admitQueue(t, store, newScope, "new explicit request", "new")
	newLease := acquireQueue(t, store, newScope)
	if turn, err := store.Claim(ctx, newLease); err != nil || turn == nil {
		t.Fatal("restored dataset prevented a new explicit request", err)
	}
}

func TestQueueLineageKeepsQuarantinedLegacyEvidenceSeparateFromNewIntents(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	// A legacy payload can be unavailable. Offline adoption and startup must
	// still isolate it by SQL identity before any body GET or re-upload.
	if err := db.Table("chat_pending_turns").Create(map[string]any{"id": "old-legacy", "surface": scope.Surface, "session_key": scope.Key, "payload": "fileref:v1/bytes/" + strings.Repeat("f", 64)}).Error; err != nil {
		t.Fatal(err)
	}
	controller, _, _ := queueController(t, db)
	store.SetExecutionController(controller)
	var old models.ChatPendingTurn
	if err := db.Omit("payload").Take(&old, "id = ?", "old-legacy").Error; err != nil {
		t.Fatal("quarantined legacy source was removed", err)
	}
	admitQueue(t, store, scope, "new manual request", "manual")
	lease := acquireQueue(t, store, scope)
	if turn, err := store.Claim(ctx, lease); err != nil || turn == nil || string(turn.Payload) != "new manual request" {
		t.Fatal("quarantined legacy blocked independent explicit new work", err)
	}
}
