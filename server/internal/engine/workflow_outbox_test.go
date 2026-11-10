package engine

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestWorkflowStatusAndOutboxCommitTogether(t *testing.T) {
	e, db := setupEngine(t)
	run := models.Run{ID: "outbox-run", OriginSoloID: "origin", CreatedBy: "alice", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "test:fail-workflow-outbox"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "workflow_conversation_events" {
			tx.AddError(errors.New("object storage unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if e.finish(run.ID, "completed") {
		t.Fatal("acknowledged transition without durable event")
	}
	if err := db.First(&run, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || run.EventVersion != 0 {
		t.Fatalf("partial transition: %+v", run)
	}
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if !e.finish(run.ID, "completed") {
		t.Fatal("transition did not retry")
	}
	if e.finish(run.ID, "completed") {
		t.Fatal("duplicate terminal transition")
	}
	var events int64
	db.Model(&models.WorkflowConversationEvent{}).Where("run_id = ?", run.ID).Count(&events)
	if events != 1 {
		t.Fatal(events)
	}
}
