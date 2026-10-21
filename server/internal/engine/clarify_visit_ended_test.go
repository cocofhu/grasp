package engine

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestClarifyReplyAfterCancelReturnsVisitEnded(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, clarifyOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")

	err = eng.ReactReply(run.ID, "predev", "普通用户消息", nil, nil, false)
	if !errors.Is(err, ErrClarifyVisitEnded) {
		t.Fatalf("err=%v want ErrClarifyVisitEnded", err)
	}
	var visit *ClarifyVisitEndedError
	if !errors.As(err, &visit) || visit.AutoRetryOnResume {
		t.Fatalf("want AutoRetryOnResume=false, got %#v err=%v", visit, err)
	}
	if visit.Error() != "运行已结束，这条消息没有发送" {
		t.Fatalf("error text=%q", visit.Error())
	}
}

func TestClarifyFirstMessageAfterCancelAutoRetry(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, clarifyOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")

	_, err = eng.EnqueueClarifyFirstMessageAs("", run.ID, "predev", "系统首条", nil)
	if !errors.Is(err, ErrClarifyVisitEnded) {
		t.Fatalf("err=%v want ErrClarifyVisitEnded", err)
	}
	var visit *ClarifyVisitEndedError
	if !errors.As(err, &visit) || !visit.AutoRetryOnResume {
		t.Fatalf("want AutoRetryOnResume=true, got %#v err=%v", visit, err)
	}
}

// collectVisitEndedFrame subscribes to the run broker and returns the first
// review error frame carrying code clarify_visit_ended.
func collectVisitEndedFrame(t *testing.T, eng *Engine, runID string) func() map[string]any {
	t.Helper()
	ch, unsub := eng.Broker().Subscribe(runID)
	got := make(chan map[string]any, 1)
	go func() {
		for msg := range ch {
			var f map[string]any
			if json.Unmarshal(msg, &f) != nil {
				continue
			}
			if f["type"] == "review" && f["event"] == "error" && f["code"] == "clarify_visit_ended" {
				got <- f
				return
			}
		}
	}()
	return func() map[string]any {
		defer unsub()
		select {
		case f := <-got:
			return f
		case <-time.After(waitPollTimeout):
			t.Fatal("no clarify_visit_ended error frame on the run broker")
			return nil
		}
	}
}

// A user turn queued behind a held turn, then Cancel: the turn-start drop
// must publish the same contract as HTTP, echoing the dropped turn.
func TestClarifyTurnStartDropFrameUserMessage(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, clarifyOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	hold := make(chan struct{})
	p.mu.Lock()
	p.reactHold = hold
	p.mu.Unlock()
	if err := eng.ReactReply(run.ID, "predev", "第一条", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	waitClarifyBusy(t, eng, run.ID, "predev")
	ann := []models.ReactAnnotation{{Selector: "#login", Note: "按钮放大"}}
	img := []models.PromptImage{{Ref: "blob:abc", MimeType: "image/png", Name: "shot.png"}}
	if err := eng.ReactReply(run.ID, "predev", "排队的第二条", img, ann, false); err != nil {
		t.Fatal(err)
	}
	wait := collectVisitEndedFrame(t, eng, run.ID)
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	close(hold)
	f := wait()

	if f["message"] != ClarifyVisitEndedMessage || f["autoRetryOnResume"] != false || f["nodeId"] != "predev" {
		t.Fatalf("frame=%v", f)
	}
	if _, ok := f["iteration"].(float64); !ok {
		t.Fatalf("iteration missing: %v", f)
	}
	d, _ := f["dropped"].(map[string]any)
	if d == nil || d["text"] != "排队的第二条" {
		t.Fatalf("dropped=%v", f["dropped"])
	}
	if a, _ := d["attachments"].([]any); len(a) != 1 || a[0].(map[string]any)["name"] != "shot.png" {
		t.Fatalf("attachments=%v", d["attachments"])
	}
	if a, _ := d["annotations"].([]any); len(a) != 1 || a[0].(map[string]any)["note"] != "按钮放大" {
		t.Fatalf("annotations=%v", d["annotations"])
	}
}

// The system first message dropped at turn start broadcasts
// autoRetryOnResume:true with no dropped text (no client typed it).
func TestClarifyTurnStartDropFrameFirstMessage(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, clarifyOnlyGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	hold := make(chan struct{})
	p.mu.Lock()
	p.reactHold = hold
	p.mu.Unlock()
	if err := eng.ReactReply(run.ID, "predev", "第一条", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	waitClarifyBusy(t, eng, run.ID, "predev")
	if _, err := eng.EnqueueClarifyFirstMessageAs("", run.ID, "predev", "系统首条", nil); err != nil {
		t.Fatal(err)
	}
	wait := collectVisitEndedFrame(t, eng, run.ID)
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	close(hold)
	f := wait()
	if f["autoRetryOnResume"] != true || f["message"] != ClarifyVisitEndedMessage {
		t.Fatalf("frame=%v", f)
	}
	d, ok := f["dropped"].(map[string]any)
	if !ok {
		t.Fatalf("dropped missing: %v", f)
	}
	if _, has := d["text"]; has {
		t.Fatalf("first message must not echo text: %v", d)
	}
}

func waitClarifyBusy(t *testing.T, eng *Engine, runID, nodeID string) {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		if _, busy := eng.ReviewSessionState(runID, nodeID); busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("clarify turn never became active")
}
