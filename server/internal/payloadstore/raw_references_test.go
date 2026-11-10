package payloadstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
)

func TestExplicitArchiveReferencesRemainInMigrationAndBackupClosure(t *testing.T) {
	ctx := context.Background()
	db, err := database.OpenWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "archive.db")}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	objects := &replicaStore{objects: map[string]replicaBytes{}}
	files, _ := filestore.FromDB(db)
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	baseline, err := VerifyReferences(ctx, db, objects)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := files.Prepare(ctx, strings.NewReader("frozen binary Git archive\x00\xff"), filestore.Binary)
	if err != nil {
		t.Fatal(err)
	}
	row := models.WorkspaceImport{ID: "frozen", ArchiveRef: prepared.Ref(), ArchiveSHA256: "metadata"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyReferences(ctx, db, objects)
	if err != nil || verified.Payloads != baseline.Payloads+1 || verified.Cells != baseline.Cells+1 {
		t.Fatalf("raw archive reference omitted: %+v baseline=%+v %v", verified, baseline, err)
	}
	count := len(objects.objects)
	migration, err := filestore.Migrate(ctx, db, &models.WorkspaceImport{})
	if err != nil || migration.Verified != 1 || migration.Migrated != 0 || len(migration.Failures) != 0 || len(objects.objects) != count {
		t.Fatalf("migration re-uploaded a raw ref as literal text: %+v %v", migration, err)
	}
	_, digest, _ := filestore.Parse(prepared.Ref())
	key := "payloads/sha256/" + digest[:2] + "/" + digest
	object := objects.objects[key]
	delete(objects.objects, key)
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("backup accepted a missing raw archive body")
	}
	objects.objects[key] = object
	text, err := files.Prepare(ctx, strings.NewReader("wrong owner encoding"), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{text.Ref(), "null", "literal archive reference", filestore.Prefix + "broken"} {
		if err := db.Model(&row).UpdateColumn("archive_ref", invalid).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyReferences(ctx, db, objects); err == nil {
			t.Fatalf("backup accepted invalid explicit archive ref %q", invalid)
		}
		migration, err := filestore.Migrate(ctx, db, &models.WorkspaceImport{})
		if err != nil || migration.Migrated != 0 || len(migration.Failures) != 1 {
			t.Fatalf("invalid raw field migrated as user bytes: %+v %v", migration, err)
		}
	}
	if err := db.Delete(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&models.WorkspaceImport{}); err != nil {
		t.Fatal(err)
	}
	// A pre-upgrade SQL snapshot has no new explicit-root table/column. Its
	// legacy reference hash must remain byte-for-byte verifiable.
	legacy, err := VerifyReferences(ctx, db, objects)
	if err != nil || legacy != baseline {
		t.Fatalf("new catalogue changed a pre-upgrade backup's hash: %+v want=%+v %v", legacy, baseline, err)
	}
}
