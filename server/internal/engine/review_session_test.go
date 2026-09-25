package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TestReviewSessionQueueAndCancel: FIFO enqueue + Cancel clears pending and
// interrupts the held active turn (plan g1/g3 evidence).
func TestReviewSessionQueueAndCancel(t *testing.T) {
	eng, db, provider := setupReviewEngine(t, true)
	hold := make(chan struct{})
	provider.reviseHold = hold

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "第一条", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue1: %v", err)
	}
	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "第二条", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue2: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w, thinking := eng.ReviewSessionState(run.ID, "prop")
		if thinking && w >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	w, thinking := eng.ReviewSessionState(run.ID, "prop")
	if !thinking || w < 1 {
		t.Fatalf("expected active turn + pending queue, got waiting=%d thinking=%v", w, thinking)
	}
	if eng.ReviewSessionReady(run.ID, "prop") {
		t.Fatal("expected not ready while queued/thinking")
	}

	if err := eng.CancelReviewSession(run.ID, "prop"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	close(hold)
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatalf("wait after cancel: %v", err)
	}
	w, thinking = eng.ReviewSessionState(run.ID, "prop")
	if w != 0 || thinking {
		t.Fatalf("after cancel want idle, got waiting=%d thinking=%v", w, thinking)
	}
	// At most one ReviseInPlace started (the active one); queued item dropped.
	if provider.reviseCalls["prop"] > 1 {
		t.Fatalf("Cancel should drop pending; reviseCalls=%d", provider.reviseCalls["prop"])
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("load conv: %v", err)
	}
	var sawInterrupted bool
	for _, m := range conv.Messages {
		if m.Role == "agent" && m.Interrupted {
			sawInterrupted = true
		}
	}
	if !sawInterrupted {
		t.Fatalf("expected interrupted agent turn after Cancel: %+v", conv.Messages)
	}
}

// TestReviewEnqueueCapacity: platform FIFO rejects beyond MaxReviewQueueItems.
func TestReviewEnqueueCapacity(t *testing.T) {
	eng, db, provider := setupReviewEngine(t, true)
	hold := make(chan struct{})
	provider.reviseHold = hold

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	// Start one held active turn, then fill pending up to MaxReviewQueueItems.
	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "active", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue active: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, thinking := eng.ReviewSessionState(run.ID, "prop"); thinking {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < MaxReviewQueueItems; i++ {
		if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "m", nil, nil, "node", ""); err != nil {
			t.Fatalf("enqueue pending %d: %v", i, err)
		}
	}
	_, err = eng.EnqueueReviewTurn(run.ID, "prop", "overflow", nil, nil, "node", "")
	if err == nil || !strings.Contains(err.Error(), "已满") {
		t.Fatalf("expected capacity error on overflow, got %v", err)
	}
	close(hold)
	_ = eng.CancelReviewSession(run.ID, "prop")
	_ = eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second)
}

// TestReviewQueueRemoveAndReorder: item-level cancel/reorder on waiting FIFO only.
func TestReviewQueueRemoveAndReorder(t *testing.T) {
	eng, db, provider := setupReviewEngine(t, true)
	hold := make(chan struct{})
	provider.reviseHold = hold

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "第一条", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue1: %v", err)
	}
	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "第二条", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue2: %v", err)
	}
	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "第三条", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue3: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w, thinking := eng.ReviewSessionState(run.ID, "prop")
		if thinking && w >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	snap, ok := eng.ReviewSessionSnapshotFor(run.ID, "prop")
	if !ok || len(snap.Items) < 2 {
		t.Fatalf("expected pending items, snap=%+v", snap)
	}
	removeID, _ := snap.Items[0]["id"].(string)
	reorderIDs := make([]string, 0, len(snap.Items))
	for _, it := range snap.Items {
		if id, ok := it["id"].(string); ok {
			reorderIDs = append(reorderIDs, id)
		}
	}
	if removeID == "" || len(reorderIDs) < 2 {
		t.Fatalf("missing item ids: %+v", snap.Items)
	}

	if err := eng.RemoveQueuedItem(run.ID, "prop", removeID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	snap, _ = eng.ReviewSessionSnapshotFor(run.ID, "prop")
	if snap.Waiting != 1 {
		t.Fatalf("after remove waiting=%d", snap.Waiting)
	}
	for _, it := range snap.Items {
		if id, _ := it["id"].(string); id == removeID {
			t.Fatalf("removed item still present")
		}
	}

	// Reverse remaining order.
	if len(reorderIDs) >= 2 {
		reorderIDs = reorderIDs[1:] // drop removed head
		for i, j := 0, len(reorderIDs)-1; i < j; i, j = i+1, j-1 {
			reorderIDs[i], reorderIDs[j] = reorderIDs[j], reorderIDs[i]
		}
		if err := eng.ReorderQueuedItems(run.ID, "prop", reorderIDs); err != nil {
			t.Fatalf("reorder: %v", err)
		}
		snap, _ = eng.ReviewSessionSnapshotFor(run.ID, "prop")
		if got, _ := snap.Items[0]["id"].(string); got != reorderIDs[0] {
			t.Fatalf("reorder head=%s want %s", got, reorderIDs[0])
		}
	}

	close(hold)
	_ = eng.CancelReviewSession(run.ID, "prop")
	_ = eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second)
}

// TestQueueSnapshotIncludesAnnotationsAndImages: waiting queue_state items must
// carry annotations + images (plan g1.1) so clients can refill chips after edit.
func TestQueueSnapshotIncludesAnnotationsAndImages(t *testing.T) {
	eng, db, provider := setupReviewEngine(t, true)
	hold := make(chan struct{})
	provider.reviseHold = hold

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	// Occupy active slot so the next enqueue stays waiting.
	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "active-hold", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue active: %v", err)
	}
	anns := []models.ReactAnnotation{
		{JSONPath: "summary", Label: "摘要", Selector: "#hero", Quote: "摘录"},
	}
	// Images need blob store in this harness; assert empty images key + annotations.
	if _, err := eng.EnqueueReviewTurn(run.ID, "prop", "带标注排队", nil, anns, "node", ""); err != nil {
		t.Fatalf("enqueue annotated: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap, ok := eng.ReviewSessionSnapshotFor(run.ID, "prop")
		if ok && snap.Waiting >= 1 && len(snap.Items) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	snap, ok := eng.ReviewSessionSnapshotFor(run.ID, "prop")
	if !ok || len(snap.Items) < 1 {
		t.Fatalf("expected waiting items, snap=%+v", snap)
	}
	var found map[string]any
	for _, it := range snap.Items {
		if text, _ := it["text"].(string); text == "带标注排队" {
			found = it
			break
		}
	}
	if found == nil {
		t.Fatalf("annotated waiting item missing: %+v", snap.Items)
	}
	gotAnns, ok := found["annotations"].([]models.ReactAnnotation)
	if !ok || len(gotAnns) != 1 || gotAnns[0].JSONPath != "summary" || gotAnns[0].Selector != "#hero" {
		t.Fatalf("annotations not in snapshot: %+v (type %T)", found["annotations"], found["annotations"])
	}
	gotImgs, ok := found["images"].([]models.PromptImage)
	if !ok || gotImgs == nil {
		t.Fatalf("images key missing or wrong type: %+v (type %T)", found["images"], found["images"])
	}
	if len(gotImgs) != 0 {
		t.Fatalf("expected empty images slice, got %+v", gotImgs)
	}

	close(hold)
	_ = eng.CancelReviewSession(run.ID, "prop")
	_ = eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second)
}

// TestActivePageTurnOwner: page tools route by who sent the running turn, and
// the turn's done channel closes on Cancel so pending page commands stop.
func TestActivePageTurnOwner(t *testing.T) {
	eng, db, provider := setupReviewEngine(t, true)
	hold := make(chan struct{})
	provider.reviseHold = hold

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if _, _, ok := eng.ActivePageTurn(run.ID, "prop"); ok {
		t.Fatal("no turn yet")
	}
	if _, err := eng.EnqueueReviewTurnAs("user:alice", run.ID, "prop", "帮我登录", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var owner string
	var done <-chan struct{}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		if owner, done, ok = eng.ActivePageTurn(run.ID, "prop"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if owner != "user:alice" || done == nil {
		t.Fatalf("owner=%q done=%v", owner, done)
	}
	snap, _ := eng.ReviewSessionSnapshotFor(run.ID, "prop")
	if b, _ := json.Marshal(snap); strings.Contains(string(b), "alice") {
		t.Fatalf("owner leaked into snapshot: %s", b)
	}
	if err := eng.CancelReviewSession(run.ID, "prop"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("done not closed on cancel")
	}
	close(hold)
	_ = eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second)
	if _, _, ok := eng.ActivePageTurn(run.ID, "prop"); ok {
		t.Fatal("turn should be gone")
	}
}
