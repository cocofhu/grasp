package payloadstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

// This native S3 check uses only an owned random prefix and a temporary SQL
// fixture. It never lists/deletes application objects or initializes authority.
func TestLivePrimaryClosureRejectsMissingAndCorruptRemoteDespiteLocalSource(t *testing.T) {
	endpoint := os.Getenv("GRASP_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GRASP_S3_TEST_ENDPOINT and business credentials for native S3 verification")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	pathStyle := true
	prefix := strings.Trim(os.Getenv("GRASP_S3_TEST_PREFIX"), "/")
	if prefix == "" {
		prefix = "grasp-verification"
	}
	cfg := config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(dir, "blobs"), S3: config.S3StorageConfig{
		Endpoint: endpoint, Bucket: os.Getenv("GRASP_S3_TEST_BUCKET"), Prefix: prefix + "/primary-closure-" + uuid.NewString(),
		AccessKeyID: os.Getenv("GRASP_S3_TEST_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("GRASP_S3_TEST_SECRET_ACCESS_KEY"), PathStyle: &pathStyle}}
	db, err := database.OpenWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "fixture.sqlite")}, filepath.Join(cfg.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	files, _ := filestore.FromDB(db)
	const original = "native S3 retained history\x00original"
	ref, _, err := files.Put(ctx, strings.NewReader(original), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("solo_messages").Create(map[string]any{"id": "original", "content": ref}).Error; err != nil {
		t.Fatal(err)
	}
	location, _ := proposedLocation(cfg)
	if err := seedPriorPrimary(db, location); err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := CertifyPrimary(ctx, db, cfg, owner); err == nil {
		t.Fatal("missing native primary used the local source")
	}
	objects, err := objectstore.NewS3(cfg.S3)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(original))
	key := "payloads/sha256/" + hex.EncodeToString(digest[:1]) + "/" + hex.EncodeToString(digest[:])
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := objects.Delete(cleanup, key); err != nil {
			t.Errorf("could not delete the single owned native fixture object: %v", err)
		}
	})
	if err := objects.Put(ctx, key, strings.NewReader(strings.Repeat("x", len(original))), objectstore.Info{Size: int64(len(original))}); err != nil {
		t.Fatal(err)
	}
	if _, err := CertifyPrimary(ctx, db, cfg, owner); err == nil {
		t.Fatal("corrupt native primary used the local source")
	}
	if err := objects.Put(ctx, key, strings.NewReader(original), objectstore.Info{Size: int64(len(original))}); err != nil {
		t.Fatal(err)
	}
	row, err := CertifyPrimary(ctx, db, cfg, owner)
	if err != nil || row.Payloads != 1 {
		t.Fatal(row, err)
	}
	if actual, err := files.Load(ctx, ref, filestore.Text); err != nil || string(actual) != original {
		t.Fatal("native verification removed or altered the retained source", err)
	}
	if err := storageguard.Check(ctx, db, owner); err != nil {
		t.Fatal("native verification released maintenance implicitly", err)
	}
	if _, err := FinalizePrimaryClosure(ctx, db, cfg, owner); err != nil {
		t.Fatal(err)
	}
	t.Log("native S3 missing/corrupt/valid verification and explicit finalize passed in one owned random prefix")
}
