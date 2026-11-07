package filestore_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBoundedImportDoesNotPublishOversizeOrFailedInput(t *testing.T) {
	s, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PutBounded(context.Background(), strings.NewReader("12345"), filestore.Binary, 4); err == nil {
		t.Fatal("oversize accepted")
	}
	files, err := filepath.Glob(filepath.Join(s.Root, "sha256", "*", "*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("published=%v err=%v", files, err)
	}
	files, _ = filepath.Glob(filepath.Join(s.Root, "tmp", "*"))
	if len(files) != 0 {
		t.Fatalf("temporary files leaked: %v", files)
	}
	if _, _, err = s.PutBounded(context.Background(), strings.NewReader("x"), filestore.Text, 0); err == nil {
		t.Fatal("invalid limit accepted")
	}
}

func TestBinaryPendingPayloadAndInvalidLegacyRecords(t *testing.T) {
	db := fileDB(t)
	if err := db.AutoMigrate(&models.ChatPendingTurn{}, &models.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	payload := []byte{0, 255, 1, 9, 0}
	row := models.ChatPendingTurn{ID: "pending-file", Payload: payload}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.Table("chat_pending_turns").Select("payload").Where("id = ?", row.ID).Row().Scan(&stored); err != nil || !filestore.IsRef(stored) {
		t.Fatalf("SQL=%q err=%v", stored, err)
	}
	var got models.ChatPendingTurn
	if err := db.First(&got, "id = ?", row.ID).Error; err != nil || !bytes.Equal(got.Payload, payload) {
		t.Fatalf("payload=%v err=%v", got.Payload, err)
	}
	// Raw SQL deliberately simulates data written by the previous release.
	if err := db.Table("artifacts").Create(map[string]any{"id": "bad-image", "kind": "image", "content": "invalid base64"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("audit_events").Create(map[string]any{"id": "bad-json", "payload": "{broken"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("artifacts").Create(map[string]any{"id": "bad-reference", "kind": "text", "content": "fileref:v1/text/invalid"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("artifacts").Create(map[string]any{"id": "literal-null", "kind": "text", "content": "null"}).Error; err != nil {
		t.Fatal(err)
	}
	r, err := filestore.Migrate(context.Background(), db, &models.ChatPendingTurn{}, &models.Artifact{}, &models.AuditEvent{})
	if err != nil || r.Verified != 1 || len(r.Failures) != 3 || r.Migrated != 1 {
		t.Fatalf("report=%+v err=%v", r, err)
	}
	if err = db.Table("artifacts").Select("content").Where("id = ?", "bad-image").Row().Scan(&stored); err != nil || stored != "invalid base64" {
		t.Fatalf("damaged legacy record was cleared: %q %v", stored, err)
	}
}

func TestReferencesSurviveRestartAndCannotBeInjectedByImageInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite")
	root := filepath.Join(dir, "objects")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err = filestore.Attach(db, root); err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := open()
	if err := db.AutoMigrate(&models.Artifact{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Artifact{ID: "text", Kind: "text", Content: "retained bytes"}).Error; err != nil {
		t.Fatal(err)
	}
	var ref string
	if err := db.Table("artifacts").Select("content").Where("id = ?", "text").Row().Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Artifact{ID: "injected", Kind: "image", Content: ref}).Error; err == nil {
		t.Fatal("untrusted image input selected a stored object")
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	db = open()
	t.Cleanup(func() { conn, _ := db.DB(); _ = conn.Close() })
	var got models.Artifact
	if err := db.First(&got, "id = ?", "text").Error; err != nil || got.Content != "retained bytes" {
		t.Fatalf("reopened=%+v err=%v", got, err)
	}
	s, _ := filestore.FromDB(db)
	f, err := s.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	r, err := filestore.Migrate(context.Background(), db, &models.Artifact{})
	if err != nil || len(r.Failures) != 1 {
		t.Fatalf("missing file report=%+v err=%v", r, err)
	}
}
