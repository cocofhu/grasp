package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// Force wrap-up stays on the synchronous path, so the clarify confirm round
// is recorded there: the human's words paired with the agent's reply.
func TestClarifyConfirmRoundPairsHumanAndAgent(t *testing.T) {
	eng, db := setupEngine(t)

	run := runToGate(t, eng, db)

	if rounds := feedbackArtifactsOfKind(db, run.ID, "clarify"); len(rounds) != 1 {
		t.Fatalf("want one clarify product, got %d: %+v", len(rounds), rounds)
	}
	var row models.FeedbackEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindClarify).
		First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.Text != "就这样" {
		t.Fatalf("confirm round must keep the human's verbatim text, got %q", row.Text)
	}
	if len(row.Turns) != 2 || row.Turns[0].Role != "human" || row.Turns[1].Role != "agent" {
		t.Fatalf("confirm round must pair the human and agent turns: %+v", row.Turns)
	}
}

// A reviewer who confirms without typing anything has given no feedback: with
// no summary either, the round would be an empty shell.
func TestSilentClarifyConfirmRecordsNoRound(t *testing.T) {
	eng, db := setupEngine(t)

	run, err := eng.StartRun("clarify-to-design", map[string]any{"idea": "做个登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.ReactReply(run.ID, "clarify", "", nil, nil, true); err != nil {
		t.Fatalf("finish clarify: %v", err)
	}
	waitGatePending(t, db, run.ID, "approve")

	var n int64
	db.Model(&models.FeedbackEvent{}).Where("run_id = ?", run.ID).Count(&n)
	if n != 0 {
		t.Fatalf("silent confirm must not write a round, got %d", n)
	}
}

// Review confirm finalizes the products against the transcript BEFORE the git
// check commits anything, and the finalize narration is what the ledger keeps
// for the confirm round.
func TestReviewConfirmFinalizesBeforeGitCheck(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	provider.confirmMsg = "已按聊天记录把第 3 条结论补上原始链接。"
	provider.wrapUpMsg = "已提交 src/a.go,跳过 tmp.log"

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.ReactReply(run.ID, "prop", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("finish reply: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	if provider.confirmCalls["prop"] != 1 {
		t.Fatalf("expected one confirm finalize turn, got %d", provider.confirmCalls["prop"])
	}
	if provider.wrapUpBeforeConfirm {
		t.Fatal("git check must not run before the finalize turn")
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	finalizeAt, wrapUpAt := -1, -1
	for i, m := range conv.Messages {
		switch {
		case m.Role == "agent" && m.Text == provider.confirmMsg:
			finalizeAt = i
		case m.Role == "agent" && m.Text == provider.wrapUpMsg:
			wrapUpAt = i
		}
	}
	if finalizeAt < 0 || wrapUpAt < finalizeAt {
		t.Fatalf("finalize / git narration missing or out of order: %+v", conv.Messages)
	}

	var row models.FeedbackEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindReview).
		Order("seq desc").First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.Action != "revise" {
		t.Fatalf("review confirm round action = %q", row.Action)
	}
	if len(row.Turns) != 2 || row.Turns[1].Text != provider.confirmMsg {
		t.Fatalf("confirm round must pair the human with the finalize reply: %+v", row.Turns)
	}
}

func TestConfirmRoundKeepsWrittenFeedback(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.ReactReply(run.ID, "prop", "确认,顺便记一下下轮要补压测", nil, nil, true); err != nil {
		t.Fatalf("finish reply: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var row models.FeedbackEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindReview).
		Order("seq desc").First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if !strings.Contains(row.Text, "下轮要补压测") {
		t.Fatalf("written feedback must be kept, got %q", row.Text)
	}
}

func TestConfirmRoundFeedbackEventShape(t *testing.T) {
	eng, _ := setupEngine(t)
	human := models.ReactMessage{Role: "human", Text: "就这样", At: time.Now().Format(time.RFC3339)}
	agent := models.ReactMessage{Role: "agent", Text: "已核对产物。"}

	clarify := eng.confirmRoundFeedbackEvent("run-1", "c1", models.FeedbackKindClarify, 2,
		human, agent)
	if clarify.Action != "answer" || clarify.Iteration != 2 {
		t.Fatalf("clarify confirm round = %+v", clarify)
	}
	if clarify.Detail["confirm"] != true {
		t.Fatalf("confirm round must be marked as such: %+v", clarify.Detail)
	}
	if !clarify.HasSubstance() {
		t.Fatal("a written confirm has substance")
	}

	review := eng.confirmRoundFeedbackEvent("run-1", "p1", models.FeedbackKindReview, 1,
		human, agent)
	if review.Action != "revise" {
		t.Fatalf("review confirm round action = %q", review.Action)
	}

	// Silent confirm ⇒ zero value, which recordFeedback skips.
	silent := eng.confirmRoundFeedbackEvent("run-1", "c1", models.FeedbackKindClarify, 1,
		models.ReactMessage{Role: "human"}, agent)
	if silent.RunID != "" {
		t.Fatalf("silent confirm must yield the zero value, got %+v", silent)
	}
}
