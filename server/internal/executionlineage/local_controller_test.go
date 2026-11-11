package executionlineage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

func TestLocalControllerKeepsOneStableBindingAndStillFencesQuarantines(t *testing.T) {
	ctx := context.Background()
	db, _, owner := authorityFixture(t)
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	first, err := NewLocalController(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := first.Current(ctx)
	if err != nil || !validUUID(lineage) || !validUUID(first.DatasetID()) {
		t.Fatal("fresh local binding is not a valid lineage", lineage, err)
	}
	restarted, err := NewLocalController(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := restarted.Current(ctx); err != nil || again != lineage || restarted.DatasetID() != first.DatasetID() {
		t.Fatal("restart minted a new lineage for the same SQL dataset", again, err)
	}
	if err := restarted.Check(ctx, Entity{Kind: EntityRun, ID: "run-1"}); err != nil {
		t.Fatal(err)
	}

	quarantine := models.ExecutionQuarantine{ID: "q-1", DatasetID: first.DatasetID(), EntityKind: string(EntityRun), EntityID: "run-1", ReviewState: "pending"}
	if err := db.Create(&quarantine).Error; err != nil {
		t.Fatal(err)
	}
	if err := restarted.Check(ctx, Entity{Kind: EntityRun, ID: "run-1"}); !errors.Is(err, ErrQuarantined) {
		t.Fatal("local mode replayed a quarantined entity", err)
	}
	if err := restarted.Check(ctx, Entity{Kind: EntityRun, ID: "run-2"}); err != nil {
		t.Fatal(err)
	}

	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Check(ctx); !errors.Is(err, ErrInactive) {
		t.Fatal("offline maintenance did not fence local execution", err)
	}
}

func TestLocalControllerRefusesUpgradedHistoryWithoutExecutionInit(t *testing.T) {
	ctx := context.Background()
	db, _, owner := authorityFixture(t)
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.OfflineUpgradeReceipt{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.OfflineUpgradeReceipt{ID: uuid.NewString(), CompletedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocalController(ctx, db); !errors.Is(err, ErrUnbound) {
		t.Fatal("upgraded history was granted a fresh binding without quarantine", err)
	}
	var count int64
	if err := db.Model(&models.ExecutionDataset{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("refused startup still wrote a binding", count, err)
	}
}

func TestLocalControllerAdoptsAnExistingInitializedBinding(t *testing.T) {
	ctx := context.Background()
	db, a, owner := authorityFixture(t)
	_, permit := activateFixture(t, db, a, owner)
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	local, err := NewLocalController(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if lineage, err := local.Current(ctx); err != nil || lineage != permit.Lineage() || local.DatasetID() != permit.DatasetID() {
		t.Fatal("upgraded install lost its persisted lineage", lineage, err)
	}
}
