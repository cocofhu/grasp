package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestSandboxOperationLosingFenceCancelsWorkWithoutClearingSuccessor(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	s.operationRenewInterval = 5 * time.Millisecond
	row := models.Sandbox{Name: "operation-fence", Purpose: SandboxPurposeSolo}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, release, err := s.acquireSandboxOperation(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{
		"operation_owner": "successor", "generation": gorm.Expr("generation + 1"),
		"operation_expires_at": time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), errSandboxOperationLost) {
			t.Fatalf("wrong cancellation cause: %v", context.Cause(ctx))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale owner kept running after losing its generation")
	}
	release()
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.OperationOwner != "successor" {
		t.Fatal("stale release cleared the successor's operation")
	}
}

func TestSandboxOperationSerializesReplicasAndReentersScopedResources(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	other := NewSandboxService(db, s.mgr, s.skills, s.host, SandboxOptions{})
	rows := []models.Sandbox{{Name: "first-resource"}, {Name: "second-resource"}}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	parent, release, err := s.acquireSandboxOperation(context.Background(), rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	child, releaseChild, err := s.acquireSandboxOperation(parent, rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseChild()
	bounded, cancel := context.WithTimeout(child, time.Second)
	defer cancel()
	_, releaseNested, err := s.acquireSandboxOperation(bounded, rows[0].ID)
	if err != nil {
		t.Fatalf("same scoped operation deadlocked on its parent resource: %v", err)
	}
	releaseNested()
	ctx, cancelOther := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelOther()
	acquired := make(chan error, 1)
	go func() {
		_, unlock, err := other.acquireSandboxOperation(ctx, rows[0].ID)
		if err == nil {
			unlock()
		}
		acquired <- err
	}()
	select {
	case err := <-acquired:
		t.Fatalf("another replica entered an owned resource: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	releaseChild()
	release()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("successor never acquired the released resource")
	}
}

func TestSoloTurnOwnsEnvironmentUntilReplyIsPersisted(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running environment", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	first := newRestoreChat(true)
	solo.SetChatterForTest(first)
	solo.SetModelAlignerForTest(&recordAligner{})
	otherSB := NewSandboxService(db, sbx.mgr, sbx.skills, sbx.host, SandboxOptions{ModelBinding: sbx.modelBinding})
	other := NewSoloService(db, otherSB, solo.agents, solo.projects)
	second := newRestoreChat(false)
	other.SetChatterForTest(second)
	other.SetModelAlignerForTest(&recordAligner{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstDone := make(chan error, 1)
	go func() {
		chat := solo.acquire(view.ID)
		chat.mu.Lock()
		chat.turnID = "lease-first"
		chat.mu.Unlock()
		_, err := chat.execute(ctx, &soloTurn{ID: "lease-first", Text: "first"})
		firstDone <- err
	}()
	select {
	case <-first.started:
	case <-ctx.Done():
		t.Fatal("first turn did not reach native chat")
	}
	secondDone := make(chan error, 1)
	go func() {
		chat := other.acquire(view.ID)
		chat.mu.Lock()
		chat.turnID = "lease-second"
		chat.mu.Unlock()
		_, err := chat.execute(ctx, &soloTurn{ID: "lease-second", Text: "second"})
		secondDone <- err
	}()
	select {
	case <-second.started:
		t.Fatal("second replica entered native chat while the first turn owned the environment")
	case <-time.After(60 * time.Millisecond):
	}
	close(first.release)
	for _, done := range []chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("serialized turns did not finish")
		}
	}
	var replies []models.SoloMessage
	if err := db.Where("session_id = ? AND role = ?", view.ID, "assistant").Find(&replies).Error; err != nil || len(replies) != 2 {
		t.Fatalf("both serialized replies must be durable: count=%d err=%v", len(replies), err)
	}
}

func TestSandboxOperationPublicationFencesStaleWorkersAndKeepsUserCancellation(t *testing.T) {
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	row := models.Sandbox{Name: "publication-fence"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, release, err := s.acquireSandboxOperation(context.Background(), row.ID, "turn")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.commitSandboxOperation(cancelled, row.ID, func(tx *gorm.DB) error {
		return tx.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("error", "user stopped").Error
	}); err != nil {
		t.Fatalf("a cancelled user turn still owns its final publication: %v", err)
	}
	if err := db.Model(&models.Sandbox{}).Where("id = ?", row.ID).Updates(map[string]any{"operation_owner": "next-owner", "generation": gorm.Expr("generation + 1"), "error": "successor state"}).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	err = s.commitSandboxOperation(ctx, row.ID, func(tx *gorm.DB) error {
		called = true
		return tx.Model(&models.Sandbox{}).Where("id = ?", row.ID).Update("error", "stale state").Error
	})
	if !errors.Is(err, errSandboxOperationLost) || called {
		t.Fatalf("stale publication was not fenced: called=%v err=%v", called, err)
	}
	if err := db.First(&row, row.ID).Error; err != nil || row.Error != "successor state" {
		t.Fatalf("stale worker changed successor: %s, %v", row.Error, err)
	}
}
