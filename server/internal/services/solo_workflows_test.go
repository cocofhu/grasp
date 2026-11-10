package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestWorkflowTerminalStatusSealsInstructionsInTheSameTransaction(t *testing.T) {
	db := newTestDB(t)
	run := models.Run{ID: "terminal-instructions", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowInstructionMailbox{ID: 1, RunID: run.ID, NodeID: "work", Iteration: 1, Generation: "terminal-generation", State: "open"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"queued", "dispatching", "applied"} {
		if err := db.Create(&models.WorkflowInstruction{ID: state, MailboxID: 1, RunID: run.ID, Generation: "terminal-generation", ClientMessageID: state, State: state, Text: state}).Error; err != nil {
			t.Fatal(err)
		}
	}
	aborted := errors.New("simulate transaction failure")
	transition := func(rollback bool) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&run).Update("status", "cancelled").Error; err != nil {
				return err
			}
			if err := EnqueueWorkflowEvent(tx, run.ID, "cancelled"); err != nil {
				return err
			}
			if rollback {
				return aborted
			}
			return nil
		})
	}
	if err := transition(true); !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	var box models.WorkflowInstructionMailbox
	if err := db.First(&box, 1).Error; err != nil || box.State != "open" {
		t.Fatalf("mailbox closure escaped the rolled-back transaction: %+v %v", box, err)
	}
	if err := transition(false); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&box, 1).Error; err != nil || box.State != "sealed" {
		t.Fatalf("terminal run kept an open inbox: %+v %v", box, err)
	}
	for id, want := range map[string]string{"queued": "expired", "dispatching": "unknown", "applied": "applied"} {
		var item models.WorkflowInstruction
		if err := db.First(&item, "id = ?", id).Error; err != nil || item.State != want {
			t.Fatalf("terminal receipt %s: %+v %v", id, item, err)
		}
	}
}

func TestWorkflowOutboxOfflineDeliveryIsPrivateAndIdempotent(t *testing.T) {
	db := newTestDB(t)
	session := models.SoloSession{ID: "private-origin", UserID: "alice", Status: models.SoloStatusStopped}
	run := models.Run{ID: "workflow-run", OriginSoloID: session.ID, CreatedBy: "alice", WorkflowName: "Implementation", Title: "Keep dirty files", Status: "running"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowInstruction{ID: "delivered-instruction", RunID: run.ID, NodeID: "work", Iteration: 1, Generation: "delivery-generation", ClientMessageID: "delivery-client", Text: "explicit workflow supplement", State: "applied"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Artifact{ID: "result", RunID: run.ID, Name: "result.md", Revision: 3, Content: "actual implementation result"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloWorkflowLink{ID: "parent", SoloID: session.ID, RunID: run.ID, UserID: session.UserID, Kind: "parent"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&run).Update("status", "completed").Error; err != nil {
			return err
		}
		return EnqueueWorkflowEvent(tx, run.ID, "completed")
	}); err != nil {
		t.Fatal(err)
	}
	// Delivery is performed by a new service, with no sandbox manager or browser.
	solo := NewSoloService(db, nil, nil, NewProjectService(db))
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := solo.Messages(session.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].WorkflowRunID != run.ID || !strings.Contains(messages[0].Content, "版本 3") {
		t.Fatalf("%+v", messages)
	}
	if _, err := solo.Messages(session.ID, "mallory"); err == nil {
		t.Fatal("private parent history leaked")
	}
	preamble, err := solo.contextPreambleChecked(session.ID, "none", "history")
	if err != nil || !strings.Contains(preamble, "actual implementation result") || !strings.Contains(preamble, "explicit workflow supplement") {
		t.Fatalf("result not supplied to continuing Solo: %q %v", preamble, err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	if session.Status != models.SoloStatusStopped || session.SandboxID != 0 || session.WorkflowContextVersion != 1 || !session.ConversationContextPending {
		t.Fatalf("delivery woke or lost context: %+v", session)
	}
	// A stale worker replay cannot duplicate a message or bump its context again.
	if err := db.Model(&models.WorkflowConversationEvent{}).Where("run_id = ?", run.ID).Update("disposition", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	if session.WorkflowContextVersion != 1 {
		t.Fatal("duplicate delivery advanced context")
	}
	if err := db.Delete(&models.SoloSession{}, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return EnqueueWorkflowEvent(tx, run.ID, "failed") }); err != nil {
		t.Fatal(err)
	}
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	var event models.WorkflowConversationEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, "failed").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Disposition != "orphaned" {
		t.Fatalf("%+v", event)
	}
	var count int64
	db.Model(&models.SoloSession{}).Where("id = ?", session.ID).Count(&count)
	if count != 0 {
		t.Fatal("deleted private session was resurrected")
	}
}
