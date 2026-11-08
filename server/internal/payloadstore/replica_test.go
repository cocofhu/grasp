package payloadstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

type replicaBytes struct {
	body []byte
	info objectstore.Info
}

type replicaStore struct {
	objectstore.Store
	objects map[string]replicaBytes
}

func (s *replicaStore) Put(_ context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	raw, err := io.ReadAll(body)
	info.Metadata = maps.Clone(info.Metadata)
	s.objects[key] = replicaBytes{raw, info}
	return err
}
func (s *replicaStore) Open(_ context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	value, ok := s.objects[key]
	if !ok {
		return nil, objectstore.Info{}, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(value.body)), value.info, nil
}
func (s *replicaStore) Head(_ context.Context, key string) (objectstore.Info, error) {
	value, ok := s.objects[key]
	if !ok {
		return objectstore.Info{}, os.ErrNotExist
	}
	return value.info, nil
}
func (s *replicaStore) List(_ context.Context, _, cursor string) (objectstore.Page, error) {
	keys := []string{}
	for key := range s.objects {
		if key > cursor {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	page := objectstore.Page{}
	for i, key := range keys {
		if i == 2 { // Exercise multiple pages, not only an empty/single-page bucket.
			page.Next = keys[i-1]
			break
		}
		info := s.objects[key].info
		info.Key = key
		page.Objects = append(page.Objects, info)
	}
	return page, nil
}

func TestVerifiedReplicaRestoresSQLPayloadsNestedAttachmentsAndInstructions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "live.db")}
	db, err := database.OpenWithFiles(cfg, filepath.Join(dir, "source-payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	source := &replicaStore{objects: map[string]replicaBytes{}}
	target := &replicaStore{objects: map[string]replicaBytes{}}
	files, _ := filestore.FromDB(db)
	if err := files.SetObjectStore(source); err != nil {
		t.Fatal(err)
	}
	sourceLocation, destination, err := Locations(config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://source.test", Bucket: "private", Prefix: "source"}},
		config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://target.test", Bucket: "private", Prefix: "backup"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := bindLocation(db, sourceLocation); err != nil {
		t.Fatal(err)
	}
	blobs, _ := blob.NewObjects(source, nil, 1000)
	ref, err := blobs.Put(ctx, strings.NewReader("binary\x00\xff"), blob.Meta{Name: "原始附件.bin"})
	if err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&models.SoloMessage{ID: "chat", SessionID: "private-user", Content: "原聊天与更正", WorkflowContext: "result-context", Images: []models.PromptImage{{Ref: ref.String()}}, Events: []json.RawMessage{json.RawMessage(`{"type":"delta","data":"original"}`)}},
		&models.ChatPendingTurn{ID: "pending", Payload: []byte(`{"images":[{"ref":"` + ref.String() + `"}]}`)},
		&models.WorkflowInstruction{ID: "instruction", MailboxID: 1, Generation: "generation", ClientMessageID: "manual", Text: "补充要求同样保留"},
		&models.WorkflowConversationEvent{ID: "outbox", RunID: "run", Payload: "frozen-result"},
		&models.Artifact{ID: "file", Kind: "text", Content: "latest-file"},
		&models.ArtifactVersion{ArtifactID: "file", Content: "old-file", Revision: 1},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.sqlite")
	if err := database.Snapshot(ctx, db, cfg, owner, backup); err != nil {
		t.Fatal(err)
	}
	databaseHash, _ := FileSHA256(backup)
	header := ReplicaHeader{Version: 1, ID: owner, Source: sourceLocation, Destination: destination, DatabaseSHA256: databaseHash, CreatedAt: time.Now().UTC()}
	var manifest bytes.Buffer
	summary, err := WriteReplica(ctx, db, source, target, header, &manifest)
	if err != nil || summary.Objects.Files < 8 || summary.References.Attachments != 1 {
		t.Fatalf("copy: %+v %v", summary, err)
	}
	if len(source.objects) != len(target.objects) {
		t.Fatal("source was deleted or target inventory incomplete")
	}
	if _, _, err := VerifyReplica(ctx, db, target, strings.NewReader(strings.TrimSuffix(manifest.String(), strings.Split(strings.TrimSpace(manifest.String()), "\n")[summary.Objects.Files+1]+"\n"))); err == nil {
		t.Fatal("partial manifest accepted")
	}
	backupBytes, _ := os.ReadFile(backup)
	restoredPath := filepath.Join(dir, "restored.db")
	if err := os.WriteFile(restoredPath, backupBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := database.ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: restoredPath}, filepath.Join(dir, "empty-cold-root"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(restored) }()
	verifiedHeader, verifiedSummary, err := VerifyReplica(ctx, restored, target, bytes.NewReader(manifest.Bytes()))
	if err != nil || verifiedSummary != summary {
		t.Fatalf("cold restored SQL closure: %+v %v", verifiedSummary, err)
	}
	if err := ActivateReplica(ctx, restored, verifiedHeader, verifiedSummary); err != nil {
		t.Fatal(err)
	}
	if err := ActivateReplica(ctx, restored, verifiedHeader, verifiedSummary); err != nil {
		t.Fatalf("idempotent cutover: %v", err)
	}
	coldFiles, _ := filestore.FromDB(restored)
	if err := coldFiles.SetObjectStore(target); err != nil {
		t.Fatal(err)
	}
	var chat models.SoloMessage
	if err := restored.First(&chat, "id = ?", "chat").Error; err != nil || chat.Content != "原聊天与更正" || chat.WorkflowContext != "result-context" {
		t.Fatalf("cold history lost: %+v %v", chat, err)
	}
	var instruction models.WorkflowInstruction
	if err := restored.First(&instruction, "id = ?", "instruction").Error; err != nil || instruction.Text != "补充要求同样保留" {
		t.Fatalf("supplement lost: %+v %v", instruction, err)
	}
	key := "attachments/" + ref.ID()
	attachment := target.objects[key]
	delete(target.objects, key)
	if _, err := VerifyReferences(ctx, restored, target); err == nil {
		t.Fatal("dangling nested blob reference hidden by retained local source")
	}
	target.objects[key] = attachment
	if err := restored.Exec("UPDATE solo_messages SET content = ? WHERE id = ?", "different inline SQL body", "chat").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := VerifyReplica(ctx, restored, target, bytes.NewReader(manifest.Bytes())); err == nil {
		t.Fatal("same-sized reference counts concealed changed SQL")
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if err := ActivateReplica(ctx, db, header, summary); err == nil {
		t.Fatal("catalog switched after losing maintenance")
	}
	var current Location
	if err := db.First(&current, "name = ?", "primary").Error; err != nil || current.Fingerprint != sourceLocation.Fingerprint {
		t.Fatalf("source catalog changed after failed switch: %v", err)
	}
}

func TestReplicaRejectsOverlappingPhysicalLocationsAndInvalidReferences(t *testing.T) {
	for _, prefix := range []string{"", "source", "source/nested", "sour"} {
		a := config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://objects.test/", Bucket: "bucket", Prefix: "source"}}
		b := a
		b.S3.Prefix = prefix
		_, _, err := Locations(a, b)
		if (err != nil) != (prefix != "sour") {
			t.Fatalf("overlap %q: %v", prefix, err)
		}
	}
	if _, _, err := Locations(config.StorageConfig{}, config.StorageConfig{Driver: "local"}); err == nil {
		t.Fatal("unverified local destination accepted")
	}
	if _, _, err := VerifyReplica(context.Background(), nil, nil, strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("empty manifest: %v", err)
	}
	if _, err := storedBytes(10); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatal("unexpected serialized cell type accepted")
	}
}
