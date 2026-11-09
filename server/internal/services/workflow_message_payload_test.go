package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func messageSQLProbe(ctx context.Context, db *gorm.DB) error {
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return db.WithContext(probe).Create(&models.Setting{Key: "payload-sql-probe-" + uuid.NewString(), Value: "SQL released"}).Error
}

func TestWorkflowHistoryPreparesCapturedSourcesOutsideCreationTransaction(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	ctx := context.Background()
	run := models.Run{ID: "history-payload", Status: "completed", WorkflowName: "Build", Title: "Original title", McpToken: "never-copy-token",
		Inputs: map[string]any{"requirement": "original input"}, FirstMessage: &models.CompositeText{Text: "original first message"},
		Graph: models.Graph{Nodes: []models.Node{{ID: "work", Type: "agent"}}}}
	for _, row := range []any{
		&run,
		&models.StateRun{RunID: run.ID, NodeID: "work", Iteration: 1, Status: "completed", Outputs: map[string]any{models.OutputHandoff: "original handoff"}, Events: []models.AcpEvent{{}}},
		&models.ReactConversation{RunID: run.ID, NodeID: "work", Iteration: 1, Messages: []models.ReactMessage{{Role: "user", Text: "original clarification"}}},
		&models.Artifact{ID: "history-artifact", RunID: run.ID, Name: "result.md", Revision: 1, Content: "captured revision one"},
		&models.WorkflowInstruction{ID: "history-instruction", RunID: run.ID, Text: "original supplement", State: "applied"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filestore.FromDB(db)
	replacement, err := files.Prepare(ctx, strings.NewReader("later revision two"), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	objects.onOpen = func(ctx context.Context) error {
		if err := messageSQLProbe(ctx, db); err != nil {
			return err
		}
		var mutation error
		once.Do(func() {
			mutation = db.WithContext(ctx).Table("artifacts").Where("id = ?", "history-artifact").Updates(map[string]any{"content": replacement.Ref(), "revision": 2}).Error
		})
		return mutation
	}
	objects.onPut = func(ctx context.Context, _ []byte) error { return messageSQLProbe(ctx, db) }
	session := models.SoloSession{ID: "captured-history-chat", UserID: "bob", Status: models.SoloStatusStopped}
	prepared, err := prepareWorkflowHistory(ctx, db, &session, run.ID)
	if err != nil {
		t.Fatalf("history GET/PUT held SQL snapshot/creation connection: %v", err)
	}
	gets, puts := objects.gets.Load(), objects.puts.Load()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return commitWorkflowHistoryTx(tx, &session, prepared)
	}); err != nil || objects.gets.Load() != gets || objects.puts.Load() != puts {
		t.Fatalf("creation commit performed payload I/O: get=%d put=%d %v", objects.gets.Load(), objects.puts.Load(), err)
	}
	message, err := readSoloMessage(db.Where("session_id = ?", session.ID), soloMessageAll)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"original input", "original first message", "original handoff", "original clarification", "original supplement", "captured revision one"} {
		if !strings.Contains(message.WorkflowContext, required) {
			t.Fatalf("frozen history omitted %q: %s", required, message.WorkflowContext)
		}
	}
	for _, forbidden := range []string{"never-copy-token", "later revision two"} {
		if strings.Contains(message.WorkflowContext, forbidden) {
			t.Fatalf("captured history loaded latest data or credentials: %s", message.WorkflowContext)
		}
	}
	// A new execution/status generation cannot commit an already prepared old
	// context into a different continuation admission transaction.
	second := models.SoloSession{ID: "stale-history-chat", UserID: "bob", Status: models.SoloStatusStopped}
	stale, err := prepareWorkflowHistory(ctx, db, &second, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&run).UpdateColumn("event_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&second).Error; err != nil {
			return err
		}
		return commitWorkflowHistoryTx(tx, &second, stale)
	})
	var failure *SoloError
	if !errors.As(err, &failure) || failure.Code != "workflow_history_changed" {
		t.Fatalf("stale source generation accepted: %v", err)
	}
	var count int64
	if err := db.Model(&models.SoloSession{}).Where("id = ?", second.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected context leaked a new continuation: %d %v", count, err)
	}
}

func TestWorkflowHistoryRejectsAnOldAttemptWithoutOriginEventVersion(t *testing.T) {
	db := newTestDB(t)
	run := models.Run{ID: "standalone-retry", Status: "failed", Attempt: 1}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	session := models.SoloSession{ID: "old-attempt-context", UserID: "alice", Status: models.SoloStatusStopped}
	prepared, err := prepareWorkflowHistory(context.Background(), db, &session, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Standalone Runs have no origin-Solo outbox eventVersion. A complete
	// retry can return to the same terminal status while advancing Attempt.
	if err := db.Model(&run).UpdateColumn("attempt", 2).Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return commitWorkflowHistoryTx(tx, &session, prepared)
	})
	var failure *SoloError
	if !errors.As(err, &failure) || failure.Code != "workflow_history_changed" {
		t.Fatalf("context from an earlier execution attempt was accepted: %v", err)
	}
}

func TestWorkflowDeliveryPreparesMessageOutsidePublicationAndKeepsStableIdentity(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	ctx := context.Background()
	session := models.SoloSession{ID: "delivery-payload", UserID: "alice", Status: models.SoloStatusStopped}
	run := models.Run{ID: "delivery-workflow", Status: "completed", CreatedBy: session.UserID, OriginSoloID: session.ID, Title: "Result"}
	for _, row := range []any{&session, &run, &models.Artifact{ID: "delivery-artifact", RunID: run.ID, Name: "result", Revision: 1, Content: "frozen result body"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Enqueue's Run-transition storage conversion is a separate manifest
	// closure. This regression isolates the message delivery boundary.
	if err := db.Transaction(func(tx *gorm.DB) error { return EnqueueWorkflowEvent(tx, run.ID, "completed") }); err != nil {
		t.Fatal(err)
	}
	objects.onOpen = func(ctx context.Context) error { return messageSQLProbe(ctx, db) }
	objects.onPut = func(ctx context.Context, _ []byte) error { return messageSQLProbe(ctx, db) }
	solo := NewSoloService(db, nil, nil, nil)
	if err := solo.DeliverWorkflowEvents(ctx); err != nil {
		t.Fatalf("event hydrate or message Prepare held delivery SQL: %v", err)
	}
	if err := db.Model(&models.WorkflowConversationEvent{}).Where("run_id = ?", run.ID).Update("disposition", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if err := solo.DeliverWorkflowEvents(ctx); err != nil {
		t.Fatal(err)
	}
	var reloaded models.SoloSession
	if err := db.First(&reloaded, "id = ?", session.ID).Error; err != nil || reloaded.WorkflowContextVersion != 1 || reloaded.Status != models.SoloStatusStopped || reloaded.SandboxID != 0 {
		t.Fatalf("replay changed context identity or started compute: %+v %v", reloaded, err)
	}
	messages, err := readSoloMessages(db.Where("session_id = ?", session.ID), soloMessageAll)
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].WorkflowContext, "frozen result body") {
		t.Fatalf("delivery duplicated/lost immutable result: %+v %v", messages, err)
	}
}
