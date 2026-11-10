package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestOfflineSchemaUpgradeRequiresOwnedMaintenanceAndExistingCore(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "old.sqlite")}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close(db) }()
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []string{"", uuid.NewString(), owner} {
		if err := UpgradeOfflineSchema(ctx, db, attempt); err == nil {
			t.Fatal("empty core or incorrect owner was silently adopted")
		}
		if db.Migrator().HasTable("workspaces") {
			t.Fatal("rejected upgrade seeded application schema")
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := UpgradeOfflineSchema(ctx, tx, owner); err == nil {
			t.Fatal("schema upgrade ran inside an application transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Check(ctx, db, owner); err != nil {
		t.Fatal("rejected upgrade cleared maintenance", err)
	}
}

func TestOfflineSchemaUpgradeReusesStartupSchemaAndRetainsOwner(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLiteTest(filepath.Join(t.TempDir(), "schema.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = Close(db) }()
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if err := UpgradeOfflineSchema(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Check(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := storageguard.Register(ctx, db); err == nil {
		t.Fatal("schema upgrade unexpectedly made the database serveable")
	}
}
