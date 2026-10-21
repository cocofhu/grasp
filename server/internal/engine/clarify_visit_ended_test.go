package engine

import (
	"errors"
	"testing"
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
