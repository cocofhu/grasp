package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestSandboxOperationPublicationPreservesEarlierDeadlineAndRollsBackLateWork(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	row := models.Sandbox{Name: "earlier-publication-deadline", Error: "original"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	held, release, err := s.acquireSandboxOperation(context.Background(), row.ID, "inference")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	deadline := time.Now().Add(80 * time.Millisecond)
	ctx, cancel := context.WithDeadline(held, deadline)
	defer cancel()
	called := false
	err = s.commitSandboxOperation(ctx, row.ID, func(tx *gorm.DB) error {
		called = true
		actual, ok := tx.Statement.Context.Deadline()
		if !ok || !actual.Equal(deadline) {
			t.Fatalf("inner publication extended the caller deadline: %v %v", actual, ok)
		}
		if err := tx.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("error", "must roll back").Error; err != nil {
			return err
		}
		<-tx.Statement.Context.Done()
		return nil
	})
	if !called || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late publication was acknowledged: called=%v %v", called, err)
	}
	if err := db.First(&row, row.ID).Error; err != nil || row.Error != "original" {
		t.Fatalf("publication crossing its deadline changed state: %+v %v", row, err)
	}
	expired, expiredCancel := context.WithDeadline(held, time.Now().Add(-time.Second))
	defer expiredCancel()
	if err := s.commitSandboxOperationPrepared(expired, row.ID, func(*gorm.DB) error {
		t.Error("expired publication took its admission lock")
		return nil
	}, func(*gorm.DB) error {
		t.Error("expired publication reached its writer")
		return nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("already expired publication got another five seconds: %v", err)
	}
}

func TestSandboxOperationPublicationKeepsFiveSecondCapAndDetachesUserStop(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	row := models.Sandbox{Name: "bounded-stop-publication"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	held, release, err := s.acquireSandboxOperation(context.Background(), row.ID, "inference")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	long, cancelDeadline := context.WithTimeout(held, time.Hour)
	defer cancelDeadline()
	stopped, stop := context.WithCancel(long)
	stop()
	err = s.commitSandboxOperation(stopped, row.ID, func(tx *gorm.DB) error {
		deadline, ok := tx.Statement.Context.Deadline()
		if remaining := time.Until(deadline); !ok || remaining > 5*time.Second || remaining < 4*time.Second || tx.Statement.Context.Err() != nil {
			t.Fatalf("user stop lost bounded final publication: deadline=%v cause=%v", deadline, tx.Statement.Context.Err())
		}
		return tx.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("error", "user stopped").Error
	})
	if err != nil {
		t.Fatalf("cancelled user request could not save its final state: %v", err)
	}
	if err := db.First(&row, row.ID).Error; err != nil || row.Error != "user stopped" {
		t.Fatalf("user stop final state was not committed: %+v %v", row, err)
	}
}
