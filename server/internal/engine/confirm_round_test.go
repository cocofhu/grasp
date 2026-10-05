package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// Ordinary ReAct turns are pure narration now, so「确认并流转」is the only round
// that can carry an induction of the dialogue — which makes recording that round
// the single place the ledger summary can land.
func TestClarifyConfirmRoundCarriesAgentSummary(t *testing.T) {
	eng, db := setupEngine(t)
	provider := eng.provider.(*fakeProvider)
	provider.reactConfirmSummary = "用户确认按截图保留视觉,去掉下拉与紫色选中。"

	run := runToGate(t, eng, db)

	rounds := feedbackArtifactsOfKind(db, run.ID, "clarify")
	if len(rounds) != 1 {
		t.Fatalf("want one clarify product, got %d: %+v", len(rounds), rounds)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(rounds[0].Content), &doc); err != nil {
		t.Fatalf("clarify product is not valid JSON: %v", err)
	}
	if doc["agentSummary"] != provider.reactConfirmSummary {
		t.Fatalf("agentSummary = %v, want %q", doc["agentSummary"], provider.reactConfirmSummary)
	}

	var row models.FeedbackEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindClarify).
		First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.AgentSummary != provider.reactConfirmSummary {
		t.Fatalf("row AgentSummary = %q", row.AgentSummary)
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

// The same silent confirm DOES get recorded once the summary turn produced
// something: the induction has nowhere else to live.
func TestSilentClarifyConfirmKeptWhenSummaryExists(t *testing.T) {
	eng, db := setupEngine(t)
	provider := eng.provider.(*fakeProvider)
	provider.reactConfirmSummary = "用户全程未提出异议,按开场目标定稿。"

	run, err := eng.StartRun("clarify-to-design", map[string]any{"idea": "做个登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.ReactReply(run.ID, "clarify", "", nil, nil, true); err != nil {
		t.Fatalf("finish clarify: %v", err)
	}
	waitGatePending(t, db, run.ID, "approve")

	var row models.FeedbackEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindClarify).
		First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.AgentSummary != provider.reactConfirmSummary {
		t.Fatalf("row AgentSummary = %q", row.AgentSummary)
	}
}

// Review force reconciles the products against the transcript BEFORE the git
// wrap-up commits anything, and the reconcile narration plus its summary are
// what the ledger keeps for the confirm round.
func TestReviewConfirmReconcilesBeforeWrapUpAndRecordsSummary(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	provider.reconcileMsg = "已按聊天记录把第 3 条结论补上原始链接。"
	provider.reconcileSummary = "用户要求补齐证据链,已落到 proposals.json。"
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

	if provider.reconcileCalls["prop"] != 1 {
		t.Fatalf("expected one ReconcileOnConfirm, got %d", provider.reconcileCalls["prop"])
	}
	if provider.wrapUpBeforeReconcile {
		t.Fatal("git wrap-up must not run before the product reconcile")
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	var sawReconcile, sawWrapUp bool
	for _, m := range conv.Messages {
		switch {
		case m.Role == "agent" && m.Text == provider.reconcileMsg:
			sawReconcile = true
		case m.Role == "agent" && m.Text == provider.wrapUpMsg:
			sawWrapUp = true
		}
	}
	if !sawReconcile || !sawWrapUp {
		t.Fatalf("reconcile / wrap-up narration missing from the transcript: %+v", conv.Messages)
	}

	var row models.FeedbackEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindReview).
		Order("seq desc").First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.AgentSummary != provider.reconcileSummary {
		t.Fatalf("row AgentSummary = %q", row.AgentSummary)
	}
	if row.Action != "revise" {
		t.Fatalf("review confirm round action = %q", row.Action)
	}
	product := feedbackArtifactsOfKind(db, run.ID, "review")
	if len(product) != 1 || !strings.Contains(product[0].Content, provider.reconcileSummary) {
		t.Fatalf("cumulative review product missing the summary: %+v", product)
	}
}

// The confirm round is not a place to invent content: an agent that answered
// with prose instead of the JSON contract yields no summary at all.
func TestConfirmRoundWithoutSummaryStillKeepsWrittenFeedback(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	provider.reconcileSummary = ""

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
	if row.AgentSummary != "" {
		t.Fatalf("unparseable summary turn must not be substituted, got %q", row.AgentSummary)
	}
	if !strings.Contains(row.Text, "下轮要补压测") {
		t.Fatalf("written feedback must survive without a summary, got %q", row.Text)
	}
}

func TestConfirmRoundFeedbackEventShape(t *testing.T) {
	eng, _ := setupEngine(t)
	human := models.ReactMessage{Role: "human", Text: "就这样", At: time.Now().Format(time.RFC3339)}
	agent := models.ReactMessage{Role: "agent", Text: "已核对产物。"}

	clarify := eng.confirmRoundFeedbackEvent("run-1", "c1", models.FeedbackKindClarify, 2,
		human, agent, "  归纳  ")
	if clarify.Action != "answer" || clarify.Iteration != 2 || clarify.AgentSummary != "归纳" {
		t.Fatalf("clarify confirm round = %+v", clarify)
	}
	if clarify.Detail["confirm"] != true {
		t.Fatalf("confirm round must be marked as such: %+v", clarify.Detail)
	}
	if !clarify.HasSubstance() {
		t.Fatal("a written confirm has substance")
	}

	review := eng.confirmRoundFeedbackEvent("run-1", "p1", models.FeedbackKindReview, 1,
		human, agent, "")
	if review.Action != "revise" {
		t.Fatalf("review confirm round action = %q", review.Action)
	}

	// Silent confirm with nothing to induce ⇒ zero value, which recordFeedback skips.
	silent := eng.confirmRoundFeedbackEvent("run-1", "c1", models.FeedbackKindClarify, 1,
		models.ReactMessage{Role: "human"}, agent, " ")
	if silent.RunID != "" {
		t.Fatalf("silent confirm must yield the zero value, got %+v", silent)
	}

	// Silent confirm whose only payload is the hidden summary must still pass
	// HasSubstance — even when reconcile narration is empty too.
	summaryOnly := eng.confirmRoundFeedbackEvent("run-1", "c1", models.FeedbackKindClarify, 1,
		models.ReactMessage{Role: "human"}, models.ReactMessage{Role: "agent"}, "用户全程未提出异议。")
	if summaryOnly.RunID == "" || summaryOnly.AgentSummary != "用户全程未提出异议。" {
		t.Fatalf("summary-only confirm must be kept: %+v", summaryOnly)
	}
	if !summaryOnly.HasSubstance() {
		t.Fatal("AgentSummary alone must qualify as substance for the confirm round")
	}
}
