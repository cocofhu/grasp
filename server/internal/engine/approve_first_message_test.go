package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

func waitFirstHumanTurn(t *testing.T, db *gorm.DB, runID, nodeID string) models.ReactMessage {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		var conv models.ReactConversation
		if err := db.Where("run_id = ? AND node_id = ?", runID, nodeID).First(&conv).Error; err == nil {
			for _, m := range conv.Messages {
				if m.Role == "human" {
					return m
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("first message was never delivered into the approve session")
	return models.ReactMessage{}
}

func hasHumanTurn(msgs []models.ReactMessage) bool {
	for _, m := range msgs {
		if m.Role == "human" {
			return true
		}
	}
	return false
}

// The launcher's opening message is delivered by the engine once the approve
// node parks — no client-side park polling / follow-up reply.
func TestApproveFirstMessageDeliveredOnPark(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	msg := &models.CompositeText{
		Text: "把登录做清楚",
		Images: []models.PromptImage{
			{Data: "QUJD", MimeType: "image/png", Name: "shot.png"},
		},
	}
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", msg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Inline image bytes are externalized before the run row is written.
	var stored models.Run
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if stored.FirstMessage == nil || stored.FirstMessage.Text != "把登录做清楚" {
		t.Fatalf("first message not persisted: %+v", stored.FirstMessage)
	}
	if len(stored.FirstMessage.Images) != 1 {
		t.Fatalf("first message images = %d, want 1", len(stored.FirstMessage.Images))
	}
	if img := stored.FirstMessage.Images[0]; img.Data != "" || !strings.HasPrefix(img.Ref, "blob:") {
		t.Fatalf("image not externalized: %+v", img)
	}

	turn := waitFirstHumanTurn(t, db, run.ID, "predev")
	if turn.Text != "把登录做清楚" {
		t.Fatalf("delivered text = %q", turn.Text)
	}
	if len(turn.Images) != 1 {
		t.Fatalf("delivered images = %d, want 1", len(turn.Images))
	}
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if stored.FirstMessageDeliveredAt == nil {
		t.Fatal("delivery latch must be set after delivery")
	}

	// Re-firing must not append a second human turn (the latch is held).
	c, err := eng.loadCtx(run.ID)
	if err != nil {
		t.Fatalf("loadCtx: %v", err)
	}
	eng.fireApproveFirstMessage(c, c.graph.FindNode("predev"))
	time.Sleep(80 * time.Millisecond)
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	humans := 0
	for _, m := range conv.Messages {
		if m.Role == "human" {
			humans++
		}
	}
	if humans != 1 {
		t.Fatalf("human turns = %d, want exactly 1", humans)
	}
}

// Without a first message the approve node parks with an empty transcript.
func TestApproveWithoutFirstMessageParksEmpty(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "predev")
	waitRunStatus(t, db, run.ID, "waiting_human")
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&conv).Error; err != nil {
		t.Fatalf("conv: %v", err)
	}
	if len(conv.Messages) != 0 {
		t.Fatalf("expected empty transcript, got %+v", conv.Messages)
	}
	var stored models.Run
	db.First(&stored, "id = ?", run.ID)
	if stored.FirstMessageDeliveredAt != nil {
		t.Fatal("latch must stay unset when there is nothing to deliver")
	}
}

// Cancel then ResumeFrom must re-deliver FirstMessage into the new approve visit,
// matching a fresh run — not leave the UI at "共 0 条".
func TestApproveFirstMessageRedeliveredAfterCancelResume(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, approveOnlyGraph())
	msg := &models.CompositeText{Text: "把登录做清楚"}
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", msg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	turn := waitFirstHumanTurn(t, db, run.ID, "predev")
	if turn.Text != "把登录做清楚" {
		t.Fatalf("first delivery text = %q", turn.Text)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")

	var stored models.Run
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if stored.FirstMessageDeliveredAt != nil {
		t.Fatal("cancel must release delivery latch")
	}

	if err := eng.ResumeFrom(run.ID, "predev"); err != nil {
		t.Fatalf("ResumeFrom: %v", err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")

	// Delivery runs after the node parks, so waiting_human can be observed
	// before the new visit holds its human turn.
	var conv models.ReactConversation
	deadline := time.Now().Add(waitPollTimeout)
	for {
		err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").
			Order("iteration desc").First(&conv).Error
		if err == nil && conv.Iteration >= 2 && hasHumanTurn(conv.Messages) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first message was never redelivered into the new visit (iteration=%d, err=%v)", conv.Iteration, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	humans := 0
	for _, m := range conv.Messages {
		if m.Role == "human" {
			humans++
			if m.Text != "把登录做清楚" {
				t.Fatalf("redelivered text = %q", m.Text)
			}
		}
	}
	if humans != 1 {
		t.Fatalf("new visit human turns = %d, want 1", humans)
	}
}

// The opening message runs on the session FIFO like a typed reply, so the run
// page shows the agent working instead of "waiting for a human reply".
func TestApproveFirstMessageTurnShowsBusy(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, approveOnlyGraph())
	hold := make(chan struct{})
	p.mu.Lock()
	p.reactHold = hold
	p.mu.Unlock()
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", &models.CompositeText{Text: "开始吧"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	deadline := time.Now().Add(waitPollTimeout)
	var snap ReviewSessionSnapshot
	for time.Now().Before(deadline) {
		if s, ok := eng.ReviewSessionSnapshotFor(run.ID, "predev"); ok && s.Busy {
			snap = s
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(hold)
	if !snap.Busy {
		t.Fatal("first-message turn must report busy while it runs")
	}
	waitFirstHumanTurn(t, db, run.ID, "predev")
}
