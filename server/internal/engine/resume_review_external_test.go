package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

func extractShareToken(t *testing.T, url string) string {
	t.Helper()
	i := strings.Index(url, "#t=")
	if i < 0 {
		t.Fatalf("no fragment: %s", url)
	}
	return strings.TrimPrefix(url[i:], "#t=")
}

func TestResumeReviewExternalConfirmAndLoginRevoke(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	share := gateshare.NewService(db, nil)
	eng.SetShareRevoker(share)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	created, err := share.CreateReview(run.ID, "prop", "24h", "full", "tester", "http://example.test")
	if err != nil {
		t.Fatalf("create review share: %v", err)
	}
	token := extractShareToken(t, created.URL)

	res, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != nil {
		t.Fatalf("external confirm: %v", err)
	}
	if res == nil || res.Status != "confirmed" {
		t.Fatalf("status=%+v", res)
	}
	waitRunStatus(t, db, run.ID, "completed")
	lookup, st, err := share.LookupByToken(token)
	if err != nil || lookup == nil {
		t.Fatalf("lookup after confirm: %v", err)
	}
	if st != models.ShareLinkStateUsed {
		t.Fatalf("expected used, got %s", st)
	}
}

func TestResumeReviewExternalBusyDoesNotBurnLink(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	share := gateshare.NewService(db, nil)
	eng.SetShareRevoker(share)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	created, err := share.CreateReview(run.ID, "prop", "24h", "full", "tester", "http://example.test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	token := extractShareToken(t, created.URL)

	s := eng.getOrCreateReviewSession(run.ID, "prop", sessionKindReview)
	s.SeedForTest([]*reviewQueueItem{{ID: "q1", Text: "排队中"}})

	res, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != gateshare.ErrReviewBusy {
		t.Fatalf("want ErrReviewBusy, got %v res=%+v", err, res)
	}
	if res == nil || res.Status != "busy" {
		t.Fatalf("busy status: %+v", res)
	}
	_, st, lerr := share.LookupByToken(token)
	if lerr != nil || st != models.ShareLinkStateActive {
		t.Fatalf("link burned after busy: st=%s err=%v", st, lerr)
	}

	s.SeedForTest(nil)
	res2, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != nil {
		t.Fatalf("retry after ready: %v", err)
	}
	if res2.Status != "confirmed" {
		t.Fatalf("retry status: %+v", res2)
	}
}

func TestResumeReviewExternalValidationFailureDoesNotBurnLink(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	share := gateshare.NewService(db, nil)
	eng.SetShareRevoker(share)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	created, err := share.CreateReview(run.ID, "prop", "24h", "full", "tester", "http://example.test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	token := extractShareToken(t, created.URL)

	rpc := &countingRPC{accept: false, msg: "业务校验未通过:产物不完整"}
	eng.host.SetRPCOutcomeValidator(rpc)

	res, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != gateshare.ErrReviewValidation {
		t.Fatalf("want ErrReviewValidation, got %v res=%+v", err, res)
	}
	if res == nil || res.Status != "validation_failed" {
		t.Fatalf("validation status: %+v", res)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	_, st, lerr := share.LookupByToken(token)
	if lerr != nil || st != models.ShareLinkStateActive {
		t.Fatalf("link burned after validation: st=%s err=%v", st, lerr)
	}

	rpc.accept = true
	res2, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != nil {
		t.Fatalf("retry after accept: %v", err)
	}
	if res2.Status != "confirmed" {
		t.Fatalf("retry status: %+v", res2)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

func TestReviewLoginForceRevokesUnusedShareLink(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	share := gateshare.NewService(db, nil)
	eng.SetShareRevoker(share)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	created, err := share.CreateReview(run.ID, "prop", "24h", "full", "tester", "http://example.test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	token := extractShareToken(t, created.URL)

	if err := eng.ReactReply(run.ID, "prop", "确认并流转", nil, nil, true); err != nil {
		t.Fatalf("login force: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	_, st, lerr := share.LookupByToken(token)
	if lerr != nil {
		t.Fatalf("lookup: %v", lerr)
	}
	if st != models.ShareLinkStateRevoked && st != models.ShareLinkStateUsed {
		t.Fatalf("after login force st=%s", st)
	}
	if _, err := share.CreateReview(run.ID, "prop", "24h", "full", "tester", "http://example.test"); err != gateshare.ErrUsedReadonly && err != gateshare.ErrReviewNotPending && err != gateshare.ErrRunEnded {
		t.Fatalf("recreate after login force: %v", err)
	}
}

func TestResumeReviewExternalClarifyConfirmAndIncompleteRollback(t *testing.T) {
	eng, db, provider := setupEngineGraphP(t, reactOnlyGraph())
	share := gateshare.NewService(db, nil)
	eng.SetShareRevoker(share)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	waitRunStatus(t, db, run.ID, "waiting_human")

	created, err := share.CreateReview(run.ID, "clarify", "24h", "full", "tester", "http://example.test")
	if err != nil {
		t.Fatalf("create clarify share: %v", err)
	}
	token := extractShareToken(t, created.URL)

	provider.mu.Lock()
	provider.reactForceStayOpen = true
	provider.mu.Unlock()

	res, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != gateshare.ErrReviewValidation {
		t.Fatalf("want ErrReviewValidation on unfinished clarify, got %v res=%+v", err, res)
	}
	if res == nil || res.Status != "validation_failed" {
		t.Fatalf("unfinished status: %+v", res)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	_, st, lerr := share.LookupByToken(token)
	if lerr != nil || st != models.ShareLinkStateActive {
		t.Fatalf("unfinished confirm must not burn link: st=%s err=%v", st, lerr)
	}

	provider.mu.Lock()
	provider.reactForceStayOpen = false
	provider.mu.Unlock()
	res2, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != nil {
		t.Fatalf("retry after wrap-up allowed: %v", err)
	}
	if res2.Status != "confirmed" {
		t.Fatalf("retry status: %+v", res2)
	}
}

func TestResumeReviewExternalAlreadyProcessedBusyWhileWaiting(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, reactOnlyGraph())
	share := gateshare.NewService(db, nil)
	eng.SetShareRevoker(share)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	waitRunStatus(t, db, run.ID, "waiting_human")

	created, err := share.CreateReview(run.ID, "clarify", "24h", "full", "tester", "http://example.test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	token := extractShareToken(t, created.URL)

	consumed, used, err := share.ConsumeCAS(created.ID, "confirm")
	if err != nil || !consumed || used == nil {
		t.Fatalf("pre-consume: consumed=%v err=%v", consumed, err)
	}

	res, err := eng.ResumeReviewExternal(share, token, "confirm")
	if err != gateshare.ErrReviewBusy {
		t.Fatalf("want busy while still waiting_human, got %v res=%+v", err, res)
	}
	if res == nil || res.Status != "busy" || res.AlreadyProcessed {
		t.Fatalf("alreadyProcessed must not report confirmed while waiting: %+v", res)
	}
}

func TestCancelClarifyTurnKeepsQueueForPublicShare(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, reactOnlyGraph())
	share := gateshare.NewService(db, nil)
	eng.SetShareRevoker(share)

	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	waitRunStatus(t, db, run.ID, "waiting_human")

	created, err := share.CreateReview(run.ID, "clarify", "24h", "full", "tester", "http://example.test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	token := extractShareToken(t, created.URL)

	s := eng.getOrCreateReviewSession(run.ID, "clarify", sessionKindClarify)
	s.SeedForTest([]*reviewQueueItem{{ID: "q1", Text: "已排队回复"}, {ID: "q2", Text: "第二条"}})

	if err := eng.CancelClarifyTurn(run.ID, "clarify"); err != nil {
		t.Fatalf("cancel clarify: %v", err)
	}
	waiting, _ := eng.ReviewSessionState(run.ID, "clarify")
	if waiting != 2 {
		t.Fatalf("CancelClarifyTurn must keep FIFO, waiting=%d", waiting)
	}
	_, st, lerr := share.LookupByToken(token)
	if lerr != nil || st != models.ShareLinkStateActive {
		t.Fatalf("cancel must not burn link: st=%s err=%v", st, lerr)
	}
}
