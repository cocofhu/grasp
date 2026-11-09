package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func workflowManifestFixture(t *testing.T) (*gorm.DB, *messagePayloadObjects, models.SoloSession, models.Run) {
	t.Helper()
	db := newTestDB(t)
	objects := messageObjects(t, db)
	session := models.SoloSession{ID: "manifest-parent", UserID: "alice", Status: models.SoloStatusStopped}
	run := models.Run{ID: "manifest-run", OriginSoloID: session.ID, CreatedBy: session.UserID, WorkflowName: "Frozen workflow", Title: "Original title", Status: "running"}
	for _, row := range []any{&session, &run,
		&models.Artifact{ID: "manifest-artifact", RunID: run.ID, Name: "result.md", Revision: 3, Content: "exact revision three"},
		&models.WorkflowInstructionMailbox{ID: 1, RunID: run.ID, NodeID: "work", Generation: "manifest-generation", State: "open"},
		&models.WorkflowInstruction{ID: "queued", MailboxID: 1, RunID: run.ID, NodeID: "work", Generation: "manifest-generation", ClientMessageID: "queued", State: "queued", Text: "unprocessed requirement"},
		&models.WorkflowInstruction{ID: "dispatching", MailboxID: 1, RunID: run.ID, NodeID: "work", Generation: "manifest-generation", ClientMessageID: "dispatching", State: "dispatching", Text: "uncertain external effect"},
		&models.WorkflowInstruction{ID: "applied", MailboxID: 1, RunID: run.ID, NodeID: "work", Generation: "manifest-generation", ClientMessageID: "applied", State: "applied", Text: "applied original requirement"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, objects, session, run
}

func captureManifest(t *testing.T, db *gorm.DB, runID string) workflowEventRecord {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Run{}).Where("id = ?", runID).Update("status", "completed").Error; err != nil {
			return err
		}
		return EnqueueWorkflowEvent(tx, runID, "completed")
	}); err != nil {
		t.Fatal(err)
	}
	var event workflowEventRecord
	if err := db.Where("run_id = ?", runID).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	return event
}

func TestWorkflowManifestCaptureIsAtomicAndNeverTouchesObjects(t *testing.T) {
	db, objects, _, run := workflowManifestFixture(t)
	objects.onOpen = func(context.Context) error { return errors.New("offline GET") }
	objects.onPut = func(context.Context, []byte) error { return errors.New("offline PUT") }
	gets, puts := objects.gets.Load(), objects.puts.Load()
	injected := errors.New("source SQL write failed")
	var fail atomic.Bool
	fail.Store(true)
	const callback = "manifest_capture_source_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "workflow_event_sources" && fail.Load() {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&run).Update("status", "completed").Error; err != nil {
			return err
		}
		return EnqueueWorkflowEvent(tx, run.ID, "completed")
	})
	if !errors.Is(err, injected) {
		t.Fatalf("capture performed object I/O or swallowed its SQL failure: %v", err)
	}
	var current models.Run
	if err := db.Select("id", "status", "event_version").First(&current, "id = ?", run.ID).Error; err != nil || current.Status != "running" || current.EventVersion != 0 {
		t.Fatalf("failed sources left a terminal transition/version: %+v %v", current, err)
	}
	var box models.WorkflowInstructionMailbox
	if err := db.First(&box, 1).Error; err != nil || box.State != "open" {
		t.Fatalf("failed sources left a sealed mailbox: %+v %v", box, err)
	}
	for _, id := range []string{"queued", "dispatching"} {
		var receipt models.WorkflowInstruction
		if err := db.Omit("text").First(&receipt, "id = ?", id).Error; err != nil || receipt.State != id {
			t.Fatalf("receipt retirement escaped rollback: %+v %v", receipt, err)
		}
	}
	for _, model := range []any{&workflowEventRecord{}, &workflowSourceRecord{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("partial immutable manifest escaped rollback: count=%d %v", count, err)
		}
	}
	fail.Store(false)
	event := captureManifest(t, db, run.ID)
	if event.SourceVersion != 1 || event.SourceCount != 5 || len(event.SourceFingerprint) != 64 || event.Payload != "" || event.Disposition != "pending" || event.Version != 1 {
		t.Fatalf("transition did not freeze a complete unmaterialized manifest: %+v", event)
	}
	if objects.gets.Load() != gets || objects.puts.Load() != puts {
		t.Fatalf("Run transition touched objects: GET %d→%d PUT %d→%d", gets, objects.gets.Load(), puts, objects.puts.Load())
	}
}

func TestWorkflowManifestProgressEventsCaptureOnlyTheirExactHeader(t *testing.T) {
	for _, status := range []string{"queued", "paused", "running", "waiting_human"} {
		t.Run(status, func(t *testing.T) {
			db, objects, _, run := workflowManifestFixture(t)
			objects.onOpen = func(context.Context) error { return errors.New("offline GET") }
			objects.onPut = func(context.Context, []byte) error { return errors.New("offline PUT") }
			gets, puts := objects.gets.Load(), objects.puts.Load()
			var sourceReads int
			const callback = "progress_event_must_not_read_result_sources"
			if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "artifacts" || tx.Statement.Table == "workflow_instructions" {
					sourceReads++
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&run).Update("status", status).Error; err != nil {
					return err
				}
				return EnqueueWorkflowEvent(tx, run.ID, status)
			}); err != nil {
				t.Fatalf("progress transition depends on body availability: %v", err)
			}
			var event workflowEventRecord
			if err := db.Where("run_id = ?", run.ID).First(&event).Error; err != nil {
				t.Fatal(err)
			}
			body, err := materializeWorkflowSources(context.Background(), db, event)
			if err != nil {
				t.Fatal(err)
			}
			var payload WorkflowEventPayload
			if err := json.Unmarshal(body, &payload); err != nil || payload.Status != status || payload.RunID != run.ID || payload.Title != run.Title || len(payload.Artifacts) != 0 || len(payload.Instructions) != 0 || event.SourceCount != 1 || sourceReads != 0 {
				t.Fatalf("progress copied mutable/full result data: event=%+v payload=%+v sourceReads=%d %v", event, payload, sourceReads, err)
			}
			if objects.gets.Load() != gets || objects.puts.Load() != puts {
				t.Fatalf("progress capture/materialization metadata touched objects: GET %d→%d PUT %d→%d", gets, objects.gets.Load(), puts, objects.puts.Load())
			}
		})
	}
}

func TestWorkflowManifestDeliveryUsesCapturedSourcesAfterLiveDeletion(t *testing.T) {
	db, objects, session, run := workflowManifestFixture(t)
	event := captureManifest(t, db, run.ID)
	// Captured ownership outlives mutable source records, including the Run.
	for _, table := range []string{"artifacts", "workflow_instructions", "runs"} {
		query := db.Table(table)
		if table == "runs" {
			query = query.Where("id = ?", run.ID)
		} else {
			query = query.Where("run_id = ?", run.ID)
		}
		if err := query.Delete(map[string]any{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	objects.onOpen = func(ctx context.Context) error { return messageSQLProbe(ctx, db) }
	objects.onPut = func(ctx context.Context, _ []byte) error { return messageSQLProbe(ctx, db) }
	solo := NewSoloService(db, nil, nil, nil)
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatalf("materialization used live rows or held SQL during object I/O: %v", err)
	}
	message, err := readSoloMessage(db.Where("id = ?", "workflow-event-"+event.ID), soloMessageAll)
	if err != nil {
		t.Fatal(err)
	}
	var payload WorkflowEventPayload
	if err := json.Unmarshal([]byte(message.WorkflowContext), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Title != "Original title" || payload.Status != "completed" || len(payload.Artifacts) != 1 || payload.Artifacts[0].Version != 3 || payload.Artifacts[0].Content != "exact revision three" {
		t.Fatalf("frozen artifact/header changed: %+v", payload)
	}
	want := map[string]string{"queued": "expired", "dispatching": "unknown", "applied": "applied"}
	for _, receipt := range payload.Instructions {
		if want[receipt.ID] != receipt.State || receipt.Text == "" {
			t.Fatalf("frozen retirement receipt lost intent or replayed unknown: %+v", receipt)
		}
		delete(want, receipt.ID)
	}
	if len(want) != 0 {
		t.Fatal("frozen instruction receipts were omitted")
	}
	if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Disposition != "delivered" || !filestore.IsRef(event.Payload) {
		t.Fatalf("frozen body/disposition was not published: %+v %v", event, err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil || session.WorkflowContextVersion != 1 || session.Status != models.SoloStatusStopped || session.SandboxID != 0 {
		t.Fatalf("delivery duplicated context or woke compute: %+v %v", session, err)
	}
}

func TestWorkflowManifestCorruptionOrMissingBodyKeepsPendingReceipt(t *testing.T) {
	for _, broken := range []string{"missing-body", "source-count", "source-cell", "partial-sources", "unknown-format"} {
		t.Run(broken, func(t *testing.T) {
			db, objects, session, run := workflowManifestFixture(t)
			event := captureManifest(t, db, run.ID)
			switch broken {
			case "missing-body":
				objects.mu.Lock()
				clear(objects.objects)
				objects.mu.Unlock()
			case "source-count":
				if err := db.Model(&workflowEventRecord{}).Where("id = ?", event.ID).UpdateColumn("source_count", event.SourceCount+1).Error; err != nil {
					t.Fatal(err)
				}
			case "source-cell":
				if err := db.Model(&workflowSourceRecord{}).Where("event_id = ? AND position = ?", event.ID, 0).UpdateColumn("metadata", `{}`).Error; err != nil {
					t.Fatal(err)
				}
			case "partial-sources":
				if err := db.Where("event_id = ? AND position = ?", event.ID, 1).Delete(&workflowSourceRecord{}).Error; err != nil {
					t.Fatal(err)
				}
			case "unknown-format":
				if err := db.Model(&workflowEventRecord{}).Where("id = ?", event.ID).UpdateColumn("source_version", 99).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := NewSoloService(db, nil, nil, nil).DeliverWorkflowEvents(context.Background()); err != nil {
				t.Fatalf("one invalid result escaped its retry isolation: %v", err)
			}
			if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Disposition != "pending" || event.Payload != "" || event.Attempts != 1 || event.NextAttemptAt == nil || event.LastError != "prepare_payload" {
				t.Fatalf("failed materialization cleared its receipt: %+v %v", event, err)
			}
			var count int64
			if err := db.Model(&models.SoloMessage{}).Where("session_id = ?", session.ID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed materialization inserted a partial result: %d %v", count, err)
			}
		})
	}
}

func TestWorkflowManifestBlockedPublicationLeavesRunAndMailboxAvailable(t *testing.T) {
	db, objects, _, run := workflowManifestFixture(t)
	event := captureManifest(t, db, run.ID)
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.onPut = func(ctx context.Context, _ []byte) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := preparePendingWorkflowEvent(context.Background(), db, event)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("result body upload did not start")
	}
	probe, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Run{}).Where("id = ?", run.ID).UpdateColumn("attempt", 2).Error; err != nil {
			return err
		}
		return tx.Model(&models.WorkflowInstructionMailbox{}).Where("id = ?", 1).UpdateColumn("revision", gorm.Expr("revision + 1")).Error
	}); err != nil {
		t.Fatalf("object PUT held a Run/mailbox/SQL lock: %v", err)
	}
	unblock.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowManifestPublicationAndDeliverySQLFailuresAreRetryable(t *testing.T) {
	for _, phase := range []string{"payload-ref", "message-receipt"} {
		t.Run(phase, func(t *testing.T) {
			db, objects, session, run := workflowManifestFixture(t)
			event := captureManifest(t, db, run.ID)
			objects.onOpen = func(ctx context.Context) error { return messageSQLProbe(ctx, db) }
			objects.onPut = func(ctx context.Context, _ []byte) error { return messageSQLProbe(ctx, db) }
			var fail atomic.Bool
			fail.Store(true)
			injected := errors.New("frozen result SQL publication failed")
			const callback = "manifest_publish_failure"
			if phase == "payload-ref" {
				if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "workflow_conversation_events" && fail.Load() {
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
			} else {
				if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "solo_messages" && fail.Load() {
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			}
			solo := NewSoloService(db, nil, nil, nil)
			if err := solo.DeliverWorkflowEvents(context.Background()); phase == "payload-ref" && !errors.Is(err, injected) || phase != "payload-ref" && err != nil {
				t.Fatalf("SQL failure was neither isolated nor reported as failed retry metadata: %v", err)
			}
			if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Disposition != "pending" {
				t.Fatalf("SQL failure cleared the pending event: %+v %v", event, err)
			}
			if err := db.First(&session, "id = ?", session.ID).Error; err != nil || session.WorkflowContextVersion != 0 {
				t.Fatalf("SQL failure advanced conversation context: %+v %v", session, err)
			}
			fail.Store(false)
			if event.NextAttemptAt != nil {
				// Advance the durable schedule, without sleeping or silently
				// bypassing backoff in production just to satisfy an old test.
				if err := db.Model(&workflowEventRecord{}).Where("id = ?", event.ID).UpdateColumn("next_attempt_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := db.First(&session, "id = ?", session.ID).Error; err != nil || session.WorkflowContextVersion != 1 {
				t.Fatalf("retry duplicated context advancement: %+v %v", session, err)
			}
		})
	}
}

func TestWorkflowManifestCompetingWorkersPublishOneStableMessageAndContextVersion(t *testing.T) {
	db, objects, session, run := workflowManifestFixture(t)
	event := captureManifest(t, db, run.ID)
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	var opens atomic.Int32
	objects.onOpen = func(ctx context.Context) error {
		if err := messageSQLProbe(ctx, db); err != nil {
			return err
		}
		if opens.Add(1) != 1 {
			return nil
		}
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	objects.onPut = func(ctx context.Context, _ []byte) error { return messageSQLProbe(ctx, db) }
	solo := NewSoloService(db, nil, nil, nil)
	done := make(chan error, 1)
	go func() { done <- solo.DeliverWorkflowEvents(context.Background()) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first materializer did not start")
	}
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatalf("competing materializer could not publish during another GET: %v", err)
	}
	unblock.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.SoloMessage{}).Where("id = ?", "workflow-event-"+event.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent materializers duplicated the result message: count=%d %v", count, err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil || session.WorkflowContextVersion != 1 {
		t.Fatalf("concurrent materializers bumped context twice: %+v %v", session, err)
	}
}

func TestWorkflowManifestKeepsLegacyFrozenEventsAndLiteralInlineSources(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	session := models.SoloSession{ID: "legacy-result-parent", UserID: "alice", Status: models.SoloStatusStopped}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	for i, ref := range []bool{false, true} {
		body, _ := json.Marshal(WorkflowEventPayload{RunID: "deleted-run", Title: "Captured legacy title", Status: "completed", Artifacts: []WorkflowArtifactRef{{Name: "legacy.md", Version: 4, Content: "already frozen legacy body"}}})
		event := workflowEventRecord{ID: "legacy-event-" + string(rune('a'+i)), RunID: "deleted-run", Version: uint64(i + 1), SoloID: session.ID, UserID: session.UserID, Kind: "completed", Payload: string(body), Disposition: "pending"}
		if ref {
			files, _ := filestore.FromDB(db)
			prepared, err := files.Prepare(context.Background(), strings.NewReader(event.Payload), filestore.Text)
			if err != nil {
				t.Fatal(err)
			}
			event.Payload = prepared.Ref()
		}
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	objects.onOpen = func(ctx context.Context) error { return messageSQLProbe(ctx, db) }
	objects.onPut = func(ctx context.Context, _ []byte) error { return messageSQLProbe(ctx, db) }
	if err := NewSoloService(db, nil, nil, nil).DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatalf("legacy event was reconstructed from absent live data: %v", err)
	}
	row, err := workflowSource("inline-event", "artifact", 1, workflowSourceMetadata{Artifact: &WorkflowArtifactRef{ID: "literal", Name: "literal.md"}}, filestore.Prefix+"malformed user text")
	if err != nil {
		t.Fatal(err)
	}
	var metadata workflowSourceMetadata
	if err := json.Unmarshal([]byte(row.Metadata), &metadata); err != nil {
		t.Fatal(err)
	}
	if ref, _, err := workflowSourceBody(row, metadata); err != nil || ref != "" || string(metadata.InlineBody) != filestore.Prefix+"malformed user text" {
		t.Fatalf("legacy inline text was recursively interpreted as a reference: ref=%q %+v %v", ref, metadata, err)
	}
}
