package filestore_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func fileDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "files.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = filestore.Attach(db, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.Artifact{}, &models.ArtifactVersion{}, &models.StateRun{}, &models.Run{}, &models.SoloMessage{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStoreImmutableBytesAndValidation(t *testing.T) {
	s, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{0, 1, 255, 8, 0}
	ref, size, err := s.Put(context.Background(), bytes.NewReader(body), filestore.Binary)
	if err != nil || size != int64(len(body)) {
		t.Fatalf("size=%d err=%v", size, err)
	}
	if err = s.Verify(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	raw, err := s.Read(context.Background(), ref)
	if err != nil || !bytes.Equal(raw, body) {
		t.Fatalf("body=%v err=%v", raw, err)
	}
	for _, invalid := range []string{"../secret", "fileref:v1/text/../../etc/passwd", "fileref:v1/nope/" + strings.Repeat("a", 64)} {
		if _, err = s.Open(context.Background(), invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = s.Put(ctx, bytes.NewReader(body), filestore.Binary); err == nil {
		t.Fatal("cancelled upload succeeded")
	}
	f, err := s.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	_ = f.Close()
	if err = os.WriteFile(name, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = s.Verify(context.Background(), ref); err == nil {
		t.Fatal("corruption was not detected")
	}
}

func TestPayloadsOnlyPersistReferencesAndKeepReadSemantics(t *testing.T) {
	db := fileDB(t)
	body := "<html>文件正文</html>\n"
	a := models.Artifact{ID: "a", Name: "page.html", Kind: "html", Content: body, Revision: 3}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.Table("artifacts").Select("content").Where("id = ?", a.ID).Row().Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !filestore.IsRef(stored) || strings.Contains(stored, "html>") {
		t.Fatalf("SQL contains body: %q", stored)
	}
	var got models.Artifact
	if err := db.First(&got, "id = ?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Content != body || got.PathRef != stored {
		t.Fatalf("round trip=%+v", got)
	}
	sr := models.StateRun{RunID: "run", Outputs: map[string]any{"page": body}, McpCalls: []models.McpCall{{Tool: "write_artifact", Args: body}}}
	if err := db.Create(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("state_runs").Select("outputs").Where("id = ?", sr.ID).Row().Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !filestore.IsRef(stored) {
		t.Fatalf("inline outputs: %q", stored)
	}
	if err := db.WithContext(context.Background()).First(&sr, sr.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sr.Outputs["page"] != body || sr.McpCalls[0].Args != body {
		t.Fatalf("outputs=%v calls=%v", sr.Outputs, sr.McpCalls)
	}
	if err := db.Model(&sr).Update("outputs", map[string]any{"page": "changed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&sr, sr.ID).Error; err != nil || sr.Outputs["page"] != "changed" {
		t.Fatalf("update round trip=%v err=%v", sr.Outputs, err)
	}
}

func TestLegacyMigrationPreservesVersionsAndRawImageBytes(t *testing.T) {
	db := fileDB(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	image := []byte("\x89PNG\r\n\x1a\nlegacy bytes")
	encoded := base64.StdEncoding.EncodeToString(image)
	if err := db.Table("artifacts").Create(map[string]any{"id": "image", "name": "shot.png", "kind": "image", "content": encoded, "revision": 7, "updated_at": at}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("artifact_versions").Create(map[string]any{"artifact_id": "image", "kind": "image", "content": encoded, "revision": 2, "node_id": "old-writer", "created_at": at}).Error; err != nil {
		t.Fatal(err)
	}
	var before models.Artifact
	if err := db.First(&before, "id = ?", "image").Error; !errors.Is(err, filestore.ErrNonCanonicalPayload) {
		t.Fatalf("runtime read unconverted image content: %+v err=%v", before, err)
	}
	r, err := filestore.Migrate(context.Background(), db, &models.Artifact{}, &models.ArtifactVersion{})
	if err != nil || r.Migrated != 2 || len(r.Failures) != 0 {
		t.Fatalf("report=%+v err=%v", r, err)
	}
	var after models.Artifact
	if err = db.First(&after, "id = ?", "image").Error; err != nil {
		t.Fatal(err)
	}
	if after.Content != encoded || after.Revision != 7 || !after.UpdatedAt.Equal(at) {
		t.Fatalf("migration changed logical record: %+v", after)
	}
	s, _ := filestore.FromDB(db)
	raw, err := s.Read(context.Background(), after.PathRef)
	if err != nil || !bytes.Equal(raw, image) {
		t.Fatalf("image bytes=%q err=%v", raw, err)
	}
	var old models.ArtifactVersion
	if err = db.First(&old).Error; err != nil || old.Content != encoded || old.Revision != 2 || old.NodeID != "old-writer" || !old.CreatedAt.Equal(at) {
		t.Fatalf("version=%+v err=%v", old, err)
	}
	r, err = filestore.Migrate(context.Background(), db, &models.Artifact{}, &models.ArtifactVersion{})
	if err != nil || r.Migrated != 0 || r.Verified != 2 {
		t.Fatalf("second migration=%+v err=%v", r, err)
	}
	f, err := s.Open(context.Background(), after.PathRef)
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = db.First(&after, "id = ?", "image").Error; err == nil {
		t.Fatal("missing file silently returned empty content")
	}
}
