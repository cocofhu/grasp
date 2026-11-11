package executionlineage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestControllerDoesNotAdoptAChangedSQLGeneration(t *testing.T) {
	ctx := context.Background()
	db, a, owner := authorityFixture(t)
	_, original := activateFixture(t, db, a, owner)
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewLocalController(ctx, db)
	if err != nil || runtime.DatasetID() != original.DatasetID() {
		t.Fatal(err)
	}
	if err := runtime.Check(ctx); err != nil {
		t.Fatal(err)
	}
	owner = uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(ctx); !errors.Is(err, ErrInactive) {
		t.Fatal("maintenance did not fence execution", err)
	}
	ticket, err := a.BeginRestore(ctx, owner, strings.Repeat("f", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Prepare(ctx); !errors.Is(err, ErrInactive) {
		t.Fatal("inactive marker did not revoke original process", err)
	}
	commitQuarantine(t, db, a, owner, ticket)
	if _, err := a.Activate(ctx, db, owner, ticket); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Prepare(ctx); !errors.Is(err, ErrUnbound) {
		t.Fatal("old process adopted newly finalized SQL generation", err)
	}
	restarted, err := NewLocalController(ctx, db)
	if err != nil || restarted.DatasetID() == runtime.DatasetID() {
		t.Fatal("new explicitly restarted process did not bind new dataset", err)
	}
	if err := restarted.Check(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestControllerDisabledStartupStaysClosedAndQuarantinedParentsFenceNewChildren(t *testing.T) {
	ctx := context.Background()
	db, a, owner := authorityFixture(t)
	disabled, err := NewLocalController(ctx, db)
	if err == nil {
		t.Fatal("uninitialized startup granted authority")
	}
	if err := db.Table("runs").Create(map[string]any{"id": "old-run", "status": "queued"}).Error; err != nil {
		t.Fatal(err)
	}
	activateFixture(t, db, a, owner)
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.Prepare(ctx); err == nil {
		t.Fatal("previously disabled process auto-adopted later initialization")
	}
	runtime, err := NewLocalController(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = runtime.Transaction(ctx, db, []Entity{{EntityRun, "old-run"}, {EntityInstruction, "brand-new-child"}}, func(*gorm.DB) error { called = true; return nil })
	if !errors.Is(err, ErrQuarantined) || called {
		t.Fatal("new child bypassed quarantined parent", err)
	}
	err = runtime.Transaction(ctx, db, []Entity{{EntityRun, "explicit-new-run"}}, func(tx *gorm.DB) error {
		return tx.Table("runs").Create(map[string]any{"id": "explicit-new-run", "status": "queued"}).Error
	})
	if err != nil {
		t.Fatal("explicit new work was blocked or used network inside SQL", err)
	}
	var binding models.ExecutionDataset
	if err := db.First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&binding).Update("lineage", uuid.NewString()).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Prepare(ctx); !errors.Is(err, ErrUnbound) {
		t.Fatal("SQL dataset rollback bypassed startup authority", err)
	}
}
