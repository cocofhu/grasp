package services

import (
	"context"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestWorkflowHistoryCommitUsesCurrentLockedRunMetadata(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	run := models.Run{ID: "locked-history", Status: "failed", Attempt: 3, EventVersion: 7, ControlVersion: 11}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	session := models.SoloSession{ID: "locked-history-chat", UserID: "alice", Status: models.SoloStatusStopped}
	prepared, err := prepareWorkflowHistory(context.Background(), db, &session, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	gets, puts := objects.gets.Load(), objects.puts.Load()
	checked := false
	const callback = "history_commit_check_current_read"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != reflect.TypeFor[models.Run]() {
			return
		}
		checked = true
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
			t.Error("history Run fence was read outside its publication transaction")
		}
		locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if !ok || locking.Strength != "SHARE" {
			t.Errorf("history commit must use a current, locked Run read: %#v", tx.Statement.Clauses["FOR"].Expression)
		}
		want := []string{"id", "status", "event_version", "control_version", "attempt"}
		if !reflect.DeepEqual(tx.Statement.Selects, want) {
			t.Errorf("locked Run read included payloads or omitted a generation fence: %v", tx.Statement.Selects)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		if err := LockWorkflowAdmission(tx); err != nil {
			return err
		}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return commitWorkflowHistoryTx(tx, &session, prepared)
	}); err != nil {
		t.Fatal(err)
	}
	if !checked || objects.gets.Load() != gets || objects.puts.Load() != puts {
		t.Fatalf("history commit skipped its SQL fence or performed object I/O: checked=%v GET=%d PUT=%d", checked, objects.gets.Load(), objects.puts.Load())
	}
}
