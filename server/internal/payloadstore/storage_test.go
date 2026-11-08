package payloadstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestLocalStorageBootstrapAndStableReferences(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "grasp.db")},
		Storage: config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "blobs")}}
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	storage, err := Open(context.Background(), db, cfg)
	if err != nil || storage.Blobs == nil || storage.Objects != nil {
		t.Fatalf("local storage=%+v err=%v", storage, err)
	}
	row := models.Artifact{ID: "local", Kind: "text", Content: "retained legacy text"}
	if err = db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = Open(context.Background(), db, cfg); err != nil {
		t.Fatal(err)
	}
	var read models.Artifact
	if err = db.First(&read, "id = ?", row.ID).Error; err != nil || read.Content != row.Content {
		t.Fatalf("body=%q err=%v", read.Content, err)
	}
}

func TestStorageLocationCannotSilentlySwitchPhysicalData(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	root := t.TempDir()
	local, err := proposedLocation(config.StorageConfig{Driver: "local", BlobsRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := proposedLocation(config.StorageConfig{Driver: "s3", BlobsRoot: root,
		S3: config.S3StorageConfig{Endpoint: "https://objects.test", Bucket: "bucket", Prefix: "root"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = bindLocation(db, local); err != nil {
		t.Fatal(err)
	}
	if err = bindLocation(db, remote); err != nil {
		t.Fatal(err)
	}
	if err = bindLocation(db, remote); err != nil {
		t.Fatal(err)
	}
	var legacy Location
	if err = db.First(&legacy, "name = ?", "legacy-local-"+local.Fingerprint[:12]).Error; err != nil || legacy.LocalRoot != root {
		t.Fatalf("legacy source lost: %+v err=%v", legacy, err)
	}
	changed := remote
	changed.Bucket = "empty-other-bucket"
	changed.Fingerprint = "another-physical-location"
	if err = bindLocation(db, changed); err == nil {
		t.Fatal("new bucket silently replaced historical object location")
	}
	if err = bindLocation(db, local); err == nil {
		t.Fatal("switching back to an incomplete local directory was allowed")
	}
	var current Location
	if err = db.First(&current, "name = ?", "primary").Error; err != nil || current.Bucket != remote.Bucket {
		t.Fatalf("failed switch mutated primary: %+v err=%v", current, err)
	}
}

func TestInvalidRemoteConfigCannotChangeStorageCatalog(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	if _, err = Open(context.Background(), db, nil); err == nil {
		t.Fatal("nil configuration accepted")
	}
	for _, driver := range []string{"unknown", "s3", "cos"} {
		if _, err = Open(context.Background(), db, &config.Config{Storage: config.StorageConfig{Driver: driver, BlobsRoot: t.TempDir()}}); err == nil {
			t.Fatalf("invalid %s backend accepted", driver)
		}
	}
	if db.Migrator().HasTable(&Location{}) {
		t.Fatal("invalid backend persisted a storage location")
	}
}
