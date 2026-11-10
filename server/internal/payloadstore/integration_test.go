package payloadstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
)

// TestS3LiveStorageContinuity uses the real Go SDK and a real S3 endpoint. Each
// run owns a unique prefix and a temporary database; it never touches app data.
// CI unit tests remain offline; deployment verification sets these variables.
func TestS3LiveStorageContinuity(t *testing.T) {
	endpoint := os.Getenv("GRASP_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GRASP_S3_TEST_ENDPOINT and credentials for live S3 integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := t.TempDir()
	pathStyle := true
	cfg := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "grasp.db")},
		Storage: config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(root, "first-local-root"), MaxFileBytes: 32 << 20,
			S3: config.S3StorageConfig{Endpoint: endpoint, Region: "us-east-1", Bucket: os.Getenv("GRASP_S3_TEST_BUCKET"),
				Prefix: "grasp-verification/" + uuid.NewString(), PathStyle: &pathStyle,
				AccessKeyID: os.Getenv("GRASP_S3_TEST_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("GRASP_S3_TEST_SECRET_ACCESS_KEY")}}}
	if cfg.Storage.S3.Bucket == "" {
		cfg.Storage.S3.Bucket = "grasp"
	}
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	// Start with an existing local installation, then migrate without changing
	// SQL references. The next replica has no legacy files to fall back to.
	cfg.Storage.Driver = "local"
	legacy, err := Open(ctx, db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	legacyMessage := models.SoloMessage{ID: "legacy-message", SessionID: "live-session", Role: "user", Content: "升级前的聊天和用户更正"}
	if err := db.Create(&legacyMessage).Error; err != nil {
		t.Fatal(err)
	}
	legacyRef, err := legacy.Blobs.Put(ctx, strings.NewReader("old-attachment"), blob.Meta{Name: "旧附件.txt"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Storage.Driver = "s3"
	storage, err := Open(ctx, db, cfg)
	if err != nil {
		_ = database.Close(db)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		page, listErr := storage.Objects.List(context.Background(), "", "")
		if listErr != nil {
			t.Errorf("cannot list integration objects for cleanup: %v", listErr)
			return
		}
		for _, item := range page.Objects {
			if deleteErr := storage.Objects.Delete(context.Background(), item.Key); deleteErr != nil {
				t.Errorf("integration object cleanup: %v", deleteErr)
			}
		}
	})
	archived, err := storage.ArchiveLegacy(ctx)
	if err != nil || archived.Payloads.Files < 1 || archived.Attachments.Files != 1 {
		t.Fatalf("legacy archive: %+v %v", archived, err)
	}
	if _, err := storage.ArchiveLegacy(ctx); err != nil {
		t.Fatalf("idempotent legacy archive: %v", err)
	}
	large := strings.Repeat("multipart-data\x00", 650000)
	artifact := models.Artifact{ID: "live-artifact", Name: "large.txt", Kind: "text", Content: large}
	if err = db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	message := models.SoloMessage{ID: "live-message", SessionID: "live-session", Role: "user", Content: "历史续聊：保留中文、更正与附件。"}
	if err = db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	var reference string
	if err = db.Table("solo_messages").Select("content").Where("id = ?", message.ID).Row().Scan(&reference); err != nil || !filestore.IsRef(reference) {
		t.Fatalf("history body was not externalized: %v", err)
	}
	attachment := []byte("附件\x00\xff保留原始字节")
	ref, err := storage.Blobs.Put(ctx, bytes.NewReader(attachment), blob.Meta{Name: "需求截图.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Close(db); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(cfg.Storage.BlobsRoot); err != nil {
		t.Fatal(err)
	}
	cfg.Storage.BlobsRoot = filepath.Join(root, "fresh-replica-with-no-local-payloads")
	db, err = database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	restored, err := Open(ctx, db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var readMessage models.SoloMessage
	if err = db.First(&readMessage, "id = ?", message.ID).Error; err != nil || readMessage.Content != message.Content {
		t.Fatalf("history could not be restored without local bytes: %v", err)
	}
	var readArtifact models.Artifact
	var restoredLegacy models.SoloMessage
	if err := db.First(&restoredLegacy, "id = ?", legacyMessage.ID).Error; err != nil || restoredLegacy.Content != legacyMessage.Content {
		t.Fatalf("legacy message lost during archive: %v", err)
	}
	legacyReader, legacyMetadata, err := restored.Blobs.Open(ctx, legacyRef)
	if err != nil {
		t.Fatal(err)
	}
	legacyBody, err := io.ReadAll(legacyReader)
	_ = legacyReader.Close()
	if err != nil || string(legacyBody) != "old-attachment" || legacyMetadata.Name != "旧附件.txt" {
		t.Fatalf("legacy attachment archive lost bytes or metadata: %v", err)
	}
	if err = db.First(&readArtifact, "id = ?", artifact.ID).Error; err != nil || readArtifact.Content != large {
		t.Fatalf("multipart payload changed after restart: %v", err)
	}
	reader, metadata, err := restored.Blobs.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	readAttachment, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(readAttachment, attachment) || metadata.Name != "需求截图.bin" {
		t.Fatalf("attachment lost bytes or metadata: %v", err)
	}
	hash := sha256.Sum256([]byte(large))
	t.Logf("live S3 continuity passed: %d-byte multipart payload SHA256=%s; legacy and new SQL history/attachments survived empty local roots; verified archive=%+v", len(large), hex.EncodeToString(hash[:]), archived)
}
