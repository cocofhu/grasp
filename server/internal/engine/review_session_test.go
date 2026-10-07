package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pagebridge"
	"github.com/cocofhu/grasp/internal/runtime"
)

// TestReviewSessionQueueAndCancel: FIFO enqueue + Cancel clears pending and
// interrupts the held active turn (plan g1/g3 evidence).
func TestReviewSessionQueueAndCancel(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
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
	eng, db, provider := setupReviewEngine(t)
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
	eng, db, provider := setupReviewEngine(t)
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
	eng, db, provider := setupReviewEngine(t)
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

// openPreview attaches a direct-preview drawer for owner and sets the page
// control toggle and tab visibility.
func openPreview(hub *pagebridge.Hub, runID, nodeID, owner string, on, visible bool) *pagebridge.Conn {
	c := hub.Attach(pagebridge.Key{RunID: runID, NodeID: nodeID, Owner: owner}, func([]byte) error { return nil })
	c.SetControl(on, visible)
	return c
}

// TestPageSessionRoutesToSender: a turn sent while that person is on the
// direct preview with page control open carries a page session id in its
// prompt only; page tools resolve it to that sender until the turn is
// cancelled.
func TestPageSessionRoutesToSender(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	hold := make(chan struct{})
	provider.reviseHold = hold
	prompts := make(chan string, 4)
	provider.reviseHook = func(_ runtime.NodeReq, human string) { prompts <- human }
	hub := pagebridge.NewHub()
	eng.SetPageHub(hub)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	openPreview(hub, run.ID, "prop", "user:alice", true, true)
	if _, err := eng.EnqueueReviewTurnAs("user:alice", run.ID, "prop", "帮我登录", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var prompt string
	select {
	case prompt = <-prompts:
	case <-time.After(3 * time.Second):
		t.Fatal("turn never reached the provider")
	}
	sid := pageSessionPattern.FindString(prompt)
	if sid == "" || !strings.Contains(prompt, "帮我登录") {
		t.Fatalf("prompt lacks a page session id: %q", prompt)
	}
	owner, done, ok := eng.PageTurn(run.ID, "prop", sid)
	if !ok || owner != "user:alice" || done == nil {
		t.Fatalf("owner=%q ok=%v", owner, ok)
	}
	if _, _, ok := eng.PageTurn(run.ID, "other", sid); ok {
		t.Fatal("session id must be bound to its node")
	}
	if _, _, ok := eng.PageTurn(run.ID, "prop", ""); ok {
		t.Fatal("missing session id resolved")
	}
	snap, _ := eng.ReviewSessionSnapshotFor(run.ID, "prop")
	if b, _ := json.Marshal(snap); strings.Contains(string(b), "alice") || strings.Contains(string(b), sid) {
		t.Fatalf("owner or session leaked into snapshot: %s", b)
	}
	if err := eng.CancelReviewSession(run.ID, "prop"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("done not closed on cancel")
	}
	if _, _, ok := eng.PageTurn(run.ID, "prop", sid); ok {
		t.Fatal("cancelled turn's session still valid")
	}
	close(hold)
	_ = eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second)
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").Order("iteration desc").First(&conv).Error; err == nil {
		if b, _ := json.Marshal(conv.Messages); strings.Contains(string(b), sid) {
			t.Fatalf("session id persisted: %s", b)
		}
	}
}

// TestPageSessionSkippedUnlessOnline: a sender is not enough. The prompt
// stays the user's text, and page_* cannot resolve a session, unless that
// sender's preview is online.
func TestPageSessionSkippedUnlessOnline(t *testing.T) {
	eng, db, provider := setupReviewEngine(t)
	prompts := make(chan string, 4)
	provider.reviseHook = func(_ runtime.NodeReq, human string) { prompts <- human }
	hub := pagebridge.NewHub()
	eng.SetPageHub(hub)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	await := func(text string) string {
		t.Helper()
		select {
		case p := <-prompts:
			if !strings.Contains(p, text) {
				t.Fatalf("prompt %q missing %q", p, text)
			}
			return p
		case <-time.After(3 * time.Second):
			t.Fatalf("turn %q never reached the provider", text)
			return ""
		}
	}
	assertPlain := func(prompt string) {
		t.Helper()
		if strings.Contains(prompt, "本轮页面操作 session_id:") || pageSessionPattern.FindString(prompt) != "" {
			t.Fatalf("prompt must stay the user's text: %q", prompt)
		}
	}
	send := func(text string) string {
		t.Helper()
		if _, err := eng.EnqueueReviewTurnAs("user:alice", run.ID, "prop", text, nil, nil, "node", ""); err != nil {
			t.Fatalf("enqueue %q: %v", text, err)
		}
		p := await(text)
		assertPlain(p)
		if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
			t.Fatal(err)
		}
		return p
	}

	send("没有预览")

	conn := openPreview(hub, run.ID, "prop", "user:alice", false, true)
	send("开关关闭")

	conn.SetControl(true, false)
	send("人已离开")

	bob := openPreview(hub, run.ID, "prop", "user:bob", true, true)
	defer bob.Detach()
	send("别人开着")

	conn.SetControl(true, true)
	if _, err := eng.EnqueueReviewTurnAs("user:alice", run.ID, "prop", "在预览页", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue online: %v", err)
	}
	online := await("在预览页")
	sid := pageSessionPattern.FindString(online)
	if sid == "" || !strings.HasPrefix(online, "本轮页面操作 session_id:") {
		t.Fatalf("online prompt should carry this turn's session id: %q", online)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := eng.PageTurn(run.ID, "prop", sid); ok {
		t.Fatal("previous turn's session id still valid")
	}

	conn.SetControl(true, false)
	hold := make(chan struct{})
	provider.reviseHold = hold
	if _, err := eng.EnqueueReviewTurnAs("user:alice", run.ID, "prop", "这一轮不能操作", nil, nil, "node", ""); err != nil {
		t.Fatalf("enqueue paused: %v", err)
	}
	paused := await("这一轮不能操作")
	assertPlain(paused)
	eng.pageMu.Lock()
	n := len(eng.pageSessions)
	eng.pageMu.Unlock()
	if n != 0 {
		t.Fatalf("unminted turn stored %d page sessions", n)
	}
	if _, _, ok := eng.PageTurn(run.ID, "prop", sid); ok {
		t.Fatal("previous turn's id accepted on a turn that did not mint")
	}
	if _, _, ok := eng.PageTurn(run.ID, "prop", "ps_"+strings.Repeat("a", 43)); ok {
		t.Fatal("forged session id accepted")
	}
	close(hold)
	_ = eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second)
}
