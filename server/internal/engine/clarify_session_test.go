package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TestClarifySessionQueueAndCancelKeepNext: clarify Cancel keeps pending FIFO
// and pumps the next item (Demo semantics; NOT review clear-queue).
func TestClarifySessionQueueAndCancelKeepNext(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	hold := make(chan struct{})
	provider.reactHold = hold
	provider.reactPending = 10 // keep dialogue open so Cancel path is exercised

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")

	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "第一条", nil, nil); err != nil {
		t.Fatalf("enqueue1: %v", err)
	}
	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "第二条", nil, nil); err != nil {
		t.Fatalf("enqueue2: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w, thinking := eng.ReviewSessionState(run.ID, "clarify")
		if thinking && w >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	w, thinking := eng.ReviewSessionState(run.ID, "clarify")
	if !thinking || w < 1 {
		t.Fatalf("expected active + pending, got waiting=%d thinking=%v", w, thinking)
	}

	if err := eng.CancelClarifyTurn(run.ID, "clarify"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// Clear hold BEFORE the pump starts item 2 (item 1 still has a local hold
	// ref and unblocks via ctx cancel). Otherwise item 2 blocks forever.
	provider.mu.Lock()
	provider.reactHold = nil
	provider.mu.Unlock()
	close(hold)

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		n := provider.reactReplyCalls["clarify"]
		provider.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	provider.mu.Lock()
	calls := provider.reactReplyCalls["clarify"]
	provider.mu.Unlock()
	if calls < 2 {
		t.Fatalf("clarify Cancel must keep queue and pump next; reactReplyCalls=%d", calls)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait after cancel+next: %v", err)
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	var sawInterrupted, sawSecondHuman bool
	for _, m := range conv.Messages {
		if m.Role == "agent" && m.Interrupted {
			sawInterrupted = true
		}
		if m.Role == "human" && strings.Contains(m.Text, "第二条") {
			sawSecondHuman = true
		}
	}
	if !sawInterrupted {
		t.Fatalf("expected interrupted agent after Cancel: %+v", conv.Messages)
	}
	if !sawSecondHuman {
		t.Fatalf("expected second human turn after keep-queue Cancel: %+v", conv.Messages)
	}
}

// TestClarifyReactReplyEnqueues: classic ReactReply(!force) returns before the
// turn finishes and exposes waiting via ReviewSessionState.
func TestClarifyReactReplyEnqueues(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	hold := make(chan struct{})
	provider.reactHold = hold
	provider.reactPending = 1

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")

	done := make(chan error, 1)
	go func() {
		done <- eng.ReactReply(run.ID, "clarify", "异步入队", nil, nil, false)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("enqueue reply: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReactReply(!force) should return immediately after enqueue")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, thinking := eng.ReviewSessionState(run.ID, "clarify"); thinking {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, thinking := eng.ReviewSessionState(run.ID, "clarify")
	if !thinking {
		t.Fatal("expected thinking after enqueue")
	}
	close(hold)
	provider.reactHold = nil
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestClarifyChoiceReplyDedupedPerRound(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	hold := make(chan struct{})
	provider.reactHold = hold
	provider.reactPending = 10

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")

	choice := "我的选择:\n- 请补充关键信息。 → 选项A"
	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", choice, nil, nil); err != nil {
		t.Fatalf("first choice: %v", err)
	}
	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", choice, nil, nil); err == nil {
		t.Fatal("second choice in the same round must be rejected")
	} else if !strings.Contains(err.Error(), "本轮选择题已提交") {
		t.Fatalf("reject message: %v", err)
	}
	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "第一条", nil, nil); err != nil {
		t.Fatalf("free text 1: %v", err)
	}
	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "第二条", nil, nil); err != nil {
		t.Fatalf("free text 2: %v", err)
	}

	provider.mu.Lock()
	provider.reactHold = nil
	provider.mu.Unlock()
	close(hold)
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestClarifyChoiceReplyAllowedAfterNewAsk(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	provider.reactPending = 1

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")

	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "我的选择:\n- 请补充关键信息。 → 选项A", nil, nil); err != nil {
		t.Fatalf("round1 choice: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait round1: %v", err)
	}

	if _, err := eng.EnqueueClarifyTurn(run.ID, "clarify", "我的选择:\n- 还需要一点信息。 → 继续", nil, nil); err != nil {
		t.Fatalf("round2 choice after new ask_question: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait round2: %v", err)
	}
}

func countHumanTurns(msgs []models.ReactMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "human" {
			n++
		}
	}
	return n
}

func TestLastRetryableHuman(t *testing.T) {
	t.Parallel()
	_, _, _, ok := lastRetryableHuman(nil)
	if ok {
		t.Fatal("empty transcript")
	}
	text, _, _, ok := lastRetryableHuman([]models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: ""},
	})
	if !ok || text != "做登录" {
		t.Fatalf("empty agent: ok=%v text=%q", ok, text)
	}
	text, _, _, ok = lastRetryableHuman([]models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: "(澄清回复失败:timeout)"},
	})
	if !ok || text != "做登录" {
		t.Fatalf("fail banner: ok=%v text=%q", ok, text)
	}
	text, _, _, ok = lastRetryableHuman([]models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: "(澄清开场失败:quota exhausted)"},
	})
	if !ok || text != "做登录" {
		t.Fatalf("open fail banner: ok=%v text=%q", ok, text)
	}
	_, _, _, ok = lastRetryableHuman([]models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: "正常正文"},
	})
	if ok {
		t.Fatal("success agent must not be retryable")
	}
	_, _, _, ok = lastRetryableHuman([]models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: "(已中断)", Interrupted: true},
	})
	if ok {
		t.Fatal("interrupted must not be retryable")
	}
}

// TestClarifyRetryLastDoesNotInsertHuman locks plan g2.1: cover-retry reuses the
// last human and does not append a duplicate human row.
func TestClarifyRetryLastDoesNotInsertHuman(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	provider.reactPending = 5

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")

	if err := eng.ReactReply(run.ID, "clarify", "做登录", nil, nil, false); err != nil {
		t.Fatalf("first reply: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait first: %v", err)
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	beforeHumans := countHumanTurns(conv.Messages)
	if beforeHumans < 1 {
		t.Fatalf("expected at least one human, got %d msgs=%+v", beforeHumans, conv.Messages)
	}
	last := &conv.Messages[len(conv.Messages)-1]
	if last.Role != "agent" {
		t.Fatalf("expected trailing agent, got %+v", last)
	}
	last.Text = ""
	last.Questions = nil
	if err := db.Save(&conv).Error; err != nil {
		t.Fatalf("save empty agent: %v", err)
	}

	if err := eng.ReactReplyRetryLast(run.ID, "clarify"); err != nil {
		t.Fatalf("retryLast: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait retry: %v", err)
	}

	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		t.Fatalf("reload conv: %v", err)
	}
	afterHumans := countHumanTurns(conv.Messages)
	if afterHumans != beforeHumans {
		t.Fatalf("retryLast must not insert human: before=%d after=%d msgs=%+v",
			beforeHumans, afterHumans, conv.Messages)
	}
	if conv.Done {
		t.Fatal("session must stay open after empty-fail retry (plan g2.2)")
	}
}

func TestClarifyRetryLastRejectsSuccessAgent(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	provider.reactPending = 5

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")

	if err := eng.ReactReply(run.ID, "clarify", "做登录", nil, nil, false); err != nil {
		t.Fatalf("reply: %v", err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}

	err = eng.ReactReplyRetryLast(run.ID, "clarify")
	if err == nil {
		t.Fatal("retryLast on success agent must fail")
	}
	if !strings.Contains(err.Error(), "没有可重试") {
		t.Fatalf("reject message: %v", err)
	}
}
