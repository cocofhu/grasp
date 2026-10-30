package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func storedDialogueTurns(t *testing.T, eng *Engine, runID string) []chatsession.PendingTurn {
	t.Helper()
	sessions, _, err := eng.pendingTurns.Load(pendingSurfaceClarify)
	if err != nil {
		t.Fatal(err)
	}
	return sessions[eng.reviewSessionKey(runID, "predev")]
}

func dialogueSummary(msgs []models.ReactMessage) string {
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		parts = append(parts, m.Role+":"+m.Text)
	}
	return strings.Join(parts, " | ")
}

// A restart while a clarify reply runs keeps the waiters behind it: the next
// process answers them in order, and the cut-off turn gets a retryable reply.
func TestClarifyQueuedTurnsSurviveRestart(t *testing.T) {
	eng, run, hold, _ := startHeldFirstMessage(t)
	db := eng.db
	for i, text := range []string{"第二条", "第三条"} {
		if _, err := eng.EnqueueClarifyTurnWithIDAs("user:a", run.ID, "predev", "cm-"+string(rune('2'+i)), text, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	stored := storedDialogueTurns(t, eng, run.ID)
	if len(stored) != 3 || !stored[0].Started || stored[1].Started || stored[2].Started {
		t.Fatalf("stored turns = %+v, want the active turn and two waiters", stored)
	}

	// A second engine on the same database stands in for the next process.
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	p := &fakeProvider{host: host, reactPending: 10}
	next := New(db, p, host, arts, 5)
	next.SetBlobStore(blob.NewMemory())
	t.Cleanup(next.Close)
	next.RestoreQueuedTurns()

	var conv models.ReactConversation
	deadline := time.Now().Add(waitPollTimeout)
	for {
		conv = models.ReactConversation{}
		db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&conv)
		snap, _ := next.ReviewSessionSnapshotFor(run.ID, "predev")
		if len(conv.Messages) >= 6 && !snap.Busy && snap.Waiting == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restored turns never answered: %s", dialogueSummary(conv.Messages))
		}
		time.Sleep(10 * time.Millisecond)
	}
	msgs := conv.Messages
	if len(msgs) != 6 || msgs[0].Role != "human" || msgs[0].Text != "把登录做清楚" ||
		msgs[1].Role != "agent" || msgs[1].Text != clarifyRestartStopped ||
		msgs[2].Text != "第二条" || msgs[2].ClientMessageID != "cm-2" || msgs[3].Role != "agent" ||
		msgs[4].Text != "第三条" || msgs[4].ClientMessageID != "cm-3" || msgs[5].Role != "agent" {
		t.Fatalf("dialogue = %s", dialogueSummary(msgs))
	}
	if !isRetryableEmptyOrFailedAgent(msgs[1]) {
		t.Fatal("the cut-off turn's reply must offer a retry")
	}
	if left := storedDialogueTurns(t, next, run.ID); len(left) != 0 {
		t.Fatalf("stored turns left = %+v", left)
	}
	if st := next.runStatus(run.ID); st != "waiting_human" {
		t.Fatalf("run status = %q, want waiting_human", st)
	}

	// Settle the old process's held turn without letting it run the waiters.
	eng.cancelReactSession(run.ID, "predev", true)
	close(hold)
}

func TestClarifyRestoreDropsTurnsOfAnEndedVisit(t *testing.T) {
	eng, run, hold, busy := startHeldFirstMessage(t)
	db := eng.db
	if _, err := eng.EnqueueClarifyTurnWithIDAs("user:a", run.ID, "predev", "cm-late", "晚到的消息", nil, nil); err != nil {
		t.Fatal(err)
	}
	payload := storedDialogueTurns(t, eng, run.ID)
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	eng.cancelReactSession(run.ID, "predev", true)
	close(hold)
	waitNotBusy(t, busy)
	if err := eng.pendingTurns.Save(pendingSurfaceClarify, eng.reviewSessionKey(run.ID, "predev"), payload[1:]); err != nil {
		t.Fatal(err)
	}

	eng.RestoreQueuedTurns()
	if s := eng.laneSession(run.ID, "predev", ""); s != nil && !s.Ready() {
		t.Fatal("a turn of an ended visit must not run")
	}
	if left := storedDialogueTurns(t, eng, run.ID); len(left) != 0 {
		t.Fatalf("stored turns left = %+v", left)
	}
}

func TestClarifyRestoreRerunsATurnThatNeverLanded(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, clarifyOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	item, _ := json.Marshal(&reviewQueueItem{ID: "never", Text: "没写进去", Effective: "没写进去", Source: "node", ClientMessageID: "cm-never"})
	live, _ := json.Marshal(&reviewQueueItem{ID: "live", Text: "Live", Source: "node", Live: &models.LiveRef{SID: "gone"}})
	if err := eng.pendingTurns.Save(pendingSurfaceClarify, eng.reviewSessionKey(run.ID, "predev"), []chatsession.PendingTurn{
		{ID: "never", Started: true, Payload: item},
		{ID: "live", Payload: live},
		{ID: "broken", Payload: []byte("{")},
	}); err != nil {
		t.Fatal(err)
	}
	eng.RestoreQueuedTurns()
	if err := eng.waitReviewReadyForTest(run.ID, "predev", waitPollTimeout); err != nil {
		t.Fatal(err)
	}
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&conv)
	humans := 0
	for _, m := range conv.Messages {
		if m.Role == "human" {
			humans++
			if m.Text != "没写进去" || m.ClientMessageID != "cm-never" {
				t.Fatalf("unexpected human turn: %+v", m)
			}
		}
	}
	if humans != 1 {
		t.Fatalf("dialogue = %s, want the unwritten turn run once and the Live request settled", dialogueSummary(conv.Messages))
	}
}

func TestReviewTurnCutOffByRestartGetsRetryableReply(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, clarifyOnlyGraph())
	enqueued := time.Now().Add(-time.Second)
	conv := models.ReactConversation{RunID: "run-review", NodeID: "design", Iteration: 1, Messages: []models.ReactMessage{
		{Role: "agent", Text: "初稿"},
		{Role: "human", Text: "改一下标题", At: time.Now().Format(time.RFC3339), GateNodeID: "gate1"},
	}}
	if err := db.Create(&conv).Error; err != nil {
		t.Fatal(err)
	}
	it := &reviewQueueItem{ID: "r1", Text: "改一下标题", Source: "gate", GateNodeID: "gate1"}
	if !eng.settleInterruptedReviewTurn("run-review", "design", sessionKindReview, it, enqueued) {
		t.Fatal("a landed review turn must be settled, not run again")
	}
	db.First(&conv, conv.ID)
	last := conv.Messages[len(conv.Messages)-1]
	if len(conv.Messages) != 3 || last.Text != reviewRestartStopped || last.GateNodeID != "gate1" || !isRetryableEmptyOrFailedAgent(last) {
		t.Fatalf("dialogue = %s (last %+v)", dialogueSummary(conv.Messages), last)
	}
	if eng.settleInterruptedReviewTurn("run-none", "design", sessionKindReview, it, enqueued) {
		t.Fatal("a turn without a conversation never landed")
	}
}

func TestLandedHumanIndex(t *testing.T) {
	at := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	msgs := []models.ReactMessage{
		{Role: "human", Text: "继续", At: at.Add(-time.Hour).Format(time.RFC3339)},
		{Role: "agent", Text: "好"},
		{Role: "human", Text: "继续", At: at.Add(2 * time.Second).Format(time.RFC3339)},
	}
	if got := landedHumanIndex(msgs, &reviewQueueItem{Text: "继续"}, at); got != 2 {
		t.Fatalf("landed = %d, want the human written after the enqueue", got)
	}
	if got := landedHumanIndex(msgs[:2], &reviewQueueItem{Text: "继续"}, at); got != -1 {
		t.Fatalf("an older human with the same text must not count: %d", got)
	}
	if got := landedHumanIndex(msgs, &reviewQueueItem{Text: "继续", ClientMessageID: "other"}, at); got != -1 {
		t.Fatalf("a different client message id must not count: %d", got)
	}
	if got := landedHumanIndex(msgs, &reviewQueueItem{RetryLast: true}, at); got != 2 {
		t.Fatalf("retry reuses the trailing human: %d", got)
	}
	if got := landedHumanIndex(msgs[:2], &reviewQueueItem{RetryLast: true}, at); got != -1 {
		t.Fatalf("retry before its failed reply was dropped never started: %d", got)
	}
}
