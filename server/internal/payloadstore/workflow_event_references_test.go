package payloadstore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestUnmaterializedWorkflowSourcesRestoreFromDestinationAlone(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "live.db")}
	db, err := database.OpenWithFiles(cfg, filepath.Join(dir, "live-payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	source := &replicaStore{objects: map[string]replicaBytes{}}
	target := &replicaStore{objects: map[string]replicaBytes{}}
	files, _ := filestore.FromDB(db)
	if err := files.SetObjectStore(source); err != nil {
		t.Fatal(err)
	}
	sourceLocation, destination, err := Locations(
		config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://source.test", Bucket: "private", Prefix: "workflow"}},
		config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://target.test", Bucket: "private", Prefix: "backup"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := seedPriorPrimary(db, sourceLocation); err != nil {
		t.Fatal(err)
	}
	session := models.SoloSession{ID: "cold-manifest-parent", UserID: "alice", Status: models.SoloStatusStopped}
	run := models.Run{ID: "cold-manifest-run", OriginSoloID: session.ID, CreatedBy: session.UserID, Status: "running", Title: "Captured in backup"}
	for _, row := range []any{&session, &run, &models.WorkflowInstruction{ID: "cold-instruction", RunID: run.ID, NodeID: "work", Text: "exact supplement", State: "applied"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	refs := map[string]string{}
	for encoding, body := range map[string]string{
		filestore.Text:   filestore.Prefix + "text/" + strings.Repeat("0", 64),
		filestore.JSON:   `{"exact":"JSON source"}`,
		filestore.Bytes:  "raw bytes\x00\xff",
		filestore.Binary: "retained binary\x00\xff",
		filestore.Image:  "original image bytes",
	} {
		prepared, err := files.Prepare(ctx, strings.NewReader(body), encoding)
		if err != nil {
			t.Fatal(err)
		}
		refs[encoding] = prepared.Ref()
		if err := db.Table("artifacts").Create(map[string]any{"id": "cold-" + encoding, "run_id": run.ID, "name": encoding, "revision": 9, "content": prepared.Ref()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Run{}).Where("id = ?", run.ID).Update("status", "completed").Error; err != nil {
			return err
		}
		return services.EnqueueWorkflowEvent(tx, run.ID, "completed")
	}); err != nil {
		t.Fatal(err)
	}
	// Only immutable source rows now own these objects. No live source or
	// already-materialized event body can conceal an incomplete backup scan.
	for _, model := range []any{&models.Artifact{}, &models.WorkflowInstruction{}, &models.Run{}} {
		query := db.Where("run_id = ?", run.ID)
		if _, isRun := model.(*models.Run); isRun {
			query = db.Where("id = ?", run.ID)
		}
		if err := query.Delete(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	var event struct{ ID, Payload, Disposition string }
	if err := db.Table("workflow_conversation_events").First(&event).Error; err != nil || event.Payload != "" || event.Disposition != "pending" {
		t.Fatalf("test did not back up an unmaterialized event: %+v %v", event, err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := database.Snapshot(ctx, db, cfg, owner, backup); err != nil {
		t.Fatal(err)
	}
	databaseHash, err := FileSHA256(backup)
	if err != nil {
		t.Fatal(err)
	}
	header := ReplicaHeader{Version: 1, ID: owner, Source: sourceLocation, Destination: destination, DatabaseSHA256: databaseHash, CreatedAt: time.Now().UTC()}
	var manifest bytes.Buffer
	summary, err := WriteReplica(ctx, db, source, target, header, &manifest)
	if err != nil || summary.References.Payloads < 6 {
		t.Fatalf("source-only ownership was omitted from backup: %+v %v", summary, err)
	}
	backupBytes, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(dir, "restored.db")
	if err := os.WriteFile(restoredPath, backupBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := database.ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: restoredPath}, filepath.Join(dir, "empty-cold-payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(restored) })
	if _, _, err := VerifyReplica(ctx, restored, target, bytes.NewReader(manifest.Bytes())); err != nil {
		t.Fatalf("destination-only references failed cold verification: %v", err)
	}
	_, binaryHash, _ := filestore.Parse(refs[filestore.Binary])
	binaryKey := "payloads/sha256/" + binaryHash[:2] + "/" + binaryHash
	binaryObject := target.objects[binaryKey]
	delete(target.objects, binaryKey)
	if _, err := VerifyReferences(ctx, restored, target); err == nil {
		t.Fatal("missing source-only binary reference was absent from backup closure")
	}
	target.objects[binaryKey] = binaryObject
	coldFiles, _ := filestore.FromDB(restored)
	if err := coldFiles.SetObjectStore(target); err != nil {
		t.Fatal(err)
	}
	clear(source.objects)
	if err := services.NewSoloService(restored, nil, nil, nil).DeliverWorkflowEvents(ctx); err != nil {
		t.Fatalf("unmaterialized event could not recover without the source backend/live Run: %v", err)
	}
	var message models.SoloMessage
	if err := restored.First(&message, "id = ?", "workflow-event-"+event.ID).Error; err != nil {
		t.Fatal(err)
	}
	var payload services.WorkflowEventPayload
	if err := json.Unmarshal([]byte(message.WorkflowContext), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Title != run.Title || len(payload.Artifacts) != 5 || len(payload.Instructions) != 1 || payload.Instructions[0].Text != "exact supplement" {
		t.Fatalf("cold materialization lost immutable metadata: %+v", payload)
	}
	for _, artifact := range payload.Artifacts {
		if artifact.Version != 9 || artifact.Name == filestore.Text && artifact.Content != filestore.Prefix+"text/"+strings.Repeat("0", 64) || artifact.Name == filestore.Binary && artifact.Content != refs[filestore.Binary] {
			t.Fatalf("cold source changed version, literal text or opaque binary: %+v", artifact)
		}
	}
}

func TestWorkflowSourceCataloguePreservesOldSnapshotAndRejectsBrokenCurrentTable(t *testing.T) {
	ctx := context.Background()
	db, err := database.OpenWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "legacy.db")}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	objects := &replicaStore{objects: map[string]replicaBytes{}}
	files, _ := filestore.FromDB(db)
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	before, err := VerifyReferences(ctx, db, objects)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&models.WorkflowEventSource{}); err != nil {
		t.Fatal(err)
	}
	legacy, err := VerifyReferences(ctx, db, objects)
	if err != nil || legacy != before {
		t.Fatalf("new source table changed pre-upgrade reference hash: %+v want=%+v %v", legacy, before, err)
	}
	if report, err := filestore.Migrate(ctx, db, &models.WorkflowEventSource{}); err != nil || report.Migrated != 0 || len(report.Failures) != 0 {
		t.Fatalf("missing optional source table broke old migration: %+v %v", report, err)
	}
	if err := db.AutoMigrate(&models.WorkflowEventSource{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&models.WorkflowEventSource{}, "Metadata"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("allowlist hid a broken existing source table")
	}
	if err := db.AutoMigrate(&models.WorkflowEventSource{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&models.WorkflowEventSource{}, "TextRef"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("allowlist hid a missing typed reference in an existing source table")
	}
	if _, err := filestore.Migrate(ctx, db, &models.WorkflowEventSource{}); err == nil {
		t.Fatal("migration hid a missing typed reference in an existing source table")
	}
	if filestore.OptionalPayloadTable("solo_messages") || filestore.OptionalPayloadTable("workflow_instructions") {
		t.Fatal("optional-table compatibility expanded to required legacy roots")
	}
	if err := db.Migrator().DropTable(&models.WorkflowEventSource{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("workflow_conversation_events").Create(map[string]any{"id": "current-source-owner", "source_version": 1, "source_count": 1, "disposition": "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("legacy compatibility hid loss of a current manifest's entire source table")
	}
	if _, err := filestore.Migrate(ctx, db, &models.WorkflowEventSource{}); err == nil {
		t.Fatal("migration hid loss of a current manifest's entire source table")
	}
}
