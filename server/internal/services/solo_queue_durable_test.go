package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSoloDurableClientIdentitySurvivesParallelRetryAndCompletion(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "environment ready", func() bool {
		row, _ := solo.Require(view.ID, "alice")
		return row != nil && row.Status == models.SoloStatusRunning
	})
	chatter := newRestoreChat(true)
	solo.SetChatterForTest(chatter)
	solo.SetModelAlignerForTest(&recordAligner{})
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	other.SetChatterForTest(chatter)
	other.SetModelAlignerForTest(&recordAligner{})
	var wg sync.WaitGroup
	results := make(chan SoloTurnReceipt, 12)
	failures := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			source := solo
			if i%2 == 1 {
				source = other
			}
			receipt, err := source.SendWithClientMessageID(view.ID, "alice", "same-request", "one native action", nil, nil)
			if err != nil {
				failures <- err
				return
			}
			results <- receipt
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var id string
	for receipt := range results {
		if receipt.ClientMessageID != "same-request" || receipt.TurnID == "" || (id != "" && id != receipt.TurnID) {
			t.Fatalf("different receipts for one intent: %+v", receipt)
		}
		id = receipt.TurnID
	}
	select {
	case <-chatter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted command did not execute")
	}
	close(chatter.release)
	waitFor(t, "finished receipt", func() bool {
		row, err := solo.durable.Receipt(context.Background(), soloQueueScope(view.ID), id)
		return err == nil && row.State == chatsession.TurnSucceeded
	})
	retry, err := other.SendWithClientMessageID(view.ID, "alice", "same-request", "one native action", nil, nil)
	if err != nil || retry.TurnID != id || retry.State != chatsession.TurnSucceeded || len(chatter.seen()) != 1 {
		t.Fatalf("acknowledgement retry replayed native action: %+v calls=%q err=%v", retry, chatter.seen(), err)
	}
	if _, err := solo.SendWithClientMessageID(view.ID, "alice", "same-request", "changed intent", nil, nil); err == nil {
		t.Fatal("reused client identity changed original command")
	}
	var messages []models.SoloMessage
	if err := db.Where("session_id = ?", view.ID).Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	var humans, replies int
	for _, message := range messages {
		if message.Role == "user" && message.ClientMessageID == "same-request" {
			humans++
		}
		if message.Role == "assistant" && message.TurnID == id {
			replies++
		}
	}
	if humans != 1 || replies != 1 {
		t.Fatalf("transcript identity lost: humans=%d replies=%d", humans, replies)
	}
}

func TestSoloDurableAdmissionDoesNotExecuteAfterFailedDatabaseReceipt(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "environment ready", func() bool {
		row, _ := solo.Require(view.ID, "alice")
		return row != nil && row.Status == models.SoloStatusRunning
	})
	chatter := newRestoreChat(false)
	solo.SetChatterForTest(chatter)
	solo.SetModelAlignerForTest(&recordAligner{})
	failed := errors.New("receipt storage failed")
	if err := db.Callback().Create().Before("gorm:create").Register("test:solo_receipt_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queue_turns" {
			_ = tx.AddError(failed)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SendWithClientMessageID(view.ID, "alice", "retry-after-storage", "must not execute", nil, nil); !errors.Is(err, failed) {
		t.Fatalf("failed admission was acknowledged: %v", err)
	}
	if len(chatter.seen()) != 0 {
		t.Fatal("native action executed without a committed receipt")
	}
	if err := db.Callback().Create().Remove("test:solo_receipt_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SendWithClientMessageID(view.ID, "alice", "retry-after-storage", "must not execute", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "retried request finished once", func() bool { return solo.acquire(view.ID).sess.Idle() })
	if len(chatter.seen()) != 1 {
		t.Fatalf("request was dropped/replayed: %q", chatter.seen())
	}
}

func TestSoloDurableUnknownNoticeRetriesEvenWhenQueueIsEmpty(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	row := models.SoloSession{ID: "notice-retry", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStopped}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	turn := soloTurn{ID: "interrupted", Text: "original native action"}
	payload, _ := json.Marshal(turn)
	if _, err := solo.durable.Admit(context.Background(), soloQueueScope(row.ID), chatsession.QueueIntent{ID: turn.ID, ActorID: "alice", ClientMessageID: "unknown-notice", Fingerprint: strings.Repeat("f", 64), Payload: payload}, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ChatQueueTurn{}).Where("id = ?", turn.ID).Updates(map[string]any{"state": chatsession.TurnUnknown, "reason": "interrupted", "finished_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	// A workflow result after this human is not evidence that this native turn
	// finished. Only its exact turn identity can settle the receipt.
	for _, message := range []models.SoloMessage{
		{ID: turn.ID, SessionID: row.ID, Role: "user", Content: turn.Text},
		{ID: "workflow-result", SessionID: row.ID, Role: "assistant", Content: "another task finished", Status: "ok"},
	} {
		if err := db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
	}
	failure := errors.New("notice storage unavailable")
	if err := db.Callback().Create().Before("gorm:create").Register("test:notice_failure", func(tx *gorm.DB) {
		id := ""
		switch message := tx.Statement.Dest.(type) {
		case *models.SoloMessage:
			id = message.ID
		case *soloMessageSQL:
			id = message.ID
		}
		if id == "reply:"+turn.ID {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	solo.RestoreQueuedTurns()
	receipt, err := solo.durable.Receipt(context.Background(), soloQueueScope(row.ID), turn.ID)
	if err != nil || receipt.State != chatsession.TurnUnknown || receipt.ResultID != "" {
		t.Fatalf("unknown receipt lost after failed notice: %+v %v", receipt, err)
	}
	if err := db.Callback().Create().Remove("test:notice_failure"); err != nil {
		t.Fatal(err)
	}
	solo.RestoreQueuedTurns()
	solo.RestoreQueuedTurns()
	receipt, err = solo.durable.Receipt(context.Background(), soloQueueScope(row.ID), turn.ID)
	if err != nil || receipt.State != chatsession.TurnUnknown || receipt.ResultID != "reply:"+turn.ID {
		t.Fatalf("empty queue did not retry notice: %+v %v", receipt, err)
	}
	var notice models.SoloMessage
	if err := db.First(&notice, "id = ?", receipt.ResultID).Error; err != nil || notice.Status != "unknown" || !strings.Contains(notice.Error, "执行结果待核对") {
		t.Fatalf("uncertain result was hidden as a normal stop: %+v %v", notice, err)
	}
	var replies int64
	if err := db.Model(&models.SoloMessage{}).Where("session_id = ? AND turn_id = ?", row.ID, turn.ID).Count(&replies).Error; err != nil || replies != 1 {
		t.Fatalf("notice duplicated: count=%d err=%v", replies, err)
	}
}

func TestSoloDurableDeleteFencesOldOwnerAndRemovesAcceptedBodies(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	row := models.SoloSession{ID: "delete-queue", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusError}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	turn := &soloTurn{ID: "old-owner-turn", ActorID: "alice", ClientMessageID: "delete-test", Text: "must not reappear"}
	if err := solo.queueFingerprint(turn, nil); err != nil {
		t.Fatal(err)
	}
	intent, err := solo.durableSoloIntent(turn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Admit(context.Background(), soloQueueScope(row.ID), intent, 0, nil); err != nil {
		t.Fatal(err)
	}
	lease, _, err := solo.durable.Acquire(context.Background(), soloQueueScope(row.ID), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Claim(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if err := solo.Delete(context.Background(), row.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := solo.durable.Fence(tx, lease, turn.ID); err != nil {
			return err
		}
		return tx.Create(&models.SoloMessage{ID: "late", SessionID: row.ID, Role: "assistant", Content: "late result"}).Error
	}); !errors.Is(err, chatsession.ErrQueueOwnerLost) {
		t.Fatalf("deleted conversation accepted a late result: %v", err)
	}
	for _, model := range []any{&models.ChatQueue{}, &models.ChatQueueTurn{}, &models.ChatPendingTurn{}, &models.SoloSession{}, &models.SoloMessage{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("deleted conversation retained/recreated SQL data: %T count=%d err=%v", model, count, err)
		}
	}
}

func TestSoloDurableFirstMessageFailureIsNotReportedAsAccepted(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	if err := db.Callback().Create().Before("gorm:create").Register("test:first_admission_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queue_turns" {
			_ = tx.AddError(errors.New("receipt unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	input := SoloCreateInput{ClientRequestID: "first-message-atomic", AgentName: "agentA", Text: "must retain the draft"}
	view, err := solo.Create(context.Background(), "alice", input)
	var failure *SoloError
	if view != nil || !errors.As(err, &failure) || failure.Status != 503 || failure.Code != "creation_not_confirmed" {
		t.Fatalf("first message was falsely accepted: %+v %v", view, err)
	}
	for _, model := range []any{&models.SoloSession{}, &models.SoloCreation{}, &models.Sandbox{}, &models.ChatQueueTurn{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("failed acceptance left partial state: %T count=%d err=%v", model, count, err)
		}
	}
	if err := db.Callback().Create().Remove("test:first_admission_failure"); err != nil {
		t.Fatal(err)
	}
	view, err = solo.Create(context.Background(), "alice", input)
	if err != nil || view.ClientRequestID != input.ClientRequestID || view.InitialTurnID == "" {
		t.Fatalf("same creation could not be retried: %+v %v", view, err)
	}
	waitFor(t, "retried admission's environment finishes startup", func() bool {
		current, err := solo.Require(view.ID, "alice")
		return err == nil && current.Status == models.SoloStatusRunning
	})
	if _, err := solo.Close(context.Background(), view.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}
