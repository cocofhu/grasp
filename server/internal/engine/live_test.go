package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"gorm.io/gorm"
)

func liveGraph(cfg map[string]any) models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "preview", Type: "app_preview", Label: "预览", Config: cfg},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "preview"},
			{ID: "e2", Source: "preview", Target: "output", When: "action == 'pass'"},
		},
	}
}

func setupLive(t *testing.T) (*Engine, *gorm.DB, *fakeProvider, string) {
	t.Helper()
	eng, db, p := setupEngineGraphP(t, liveGraph(map[string]any{"direct_preview": true, "live_variants": true}))
	p.skipOutcome = true
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	return eng, db, p, run.ID
}

func genEvent(sid string) models.LiveEvent {
	return models.LiveEvent{Op: models.LiveOpGenerate, SID: sid, Action: "bolder",
		Element: &models.LiveElement{Selector: "main > section", TagName: "section", Text: "Dispatch"}}
}

func waitLiveState(t *testing.T, eng *Engine, runID, sid, want string) *models.LiveSession {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, _ := eng.liveSession(runID, "preview", sid)
		if s != nil && s.State == want {
			return s
		}
		if time.Now().After(deadline) {
			st := "<nil>"
			if s != nil {
				st = s.State + " / " + s.Error
			}
			t.Fatalf("session %s state=%s, want %s", sid, st, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// agentReports makes the fake agent call live_update like the skill says.
func agentReports(eng *Engine, p *fakeProvider, report func(human string) *mcp.LiveReport) {
	p.mu.Lock()
	p.reviseHook = func(req runtime.NodeReq, human string) {
		if r := report(human); r != nil {
			_, _ = eng.ApplyLiveReport(req.RunID, req.NodeID, *r)
		}
	}
	p.mu.Unlock()
}

func TestLiveEnabledRequiresDirectAndSwitch(t *testing.T) {
	for _, c := range []struct {
		cfg  map[string]any
		want bool
	}{
		{map[string]any{"direct_preview": true, "live_variants": true}, true},
		{map[string]any{"direct_preview": true}, true},
		{map[string]any{"direct_preview": true, "live_variants": ""}, true},
		{map[string]any{"direct_preview": true, "live_variants": false}, false},
		{map[string]any{"live_variants": "true"}, false},
		{nil, false},
	} {
		eng, db, p := setupEngineGraphP(t, liveGraph(c.cfg))
		p.skipOutcome = true
		run, err := eng.StartRun("wf", nil, "test")
		if err != nil {
			t.Fatal(err)
		}
		waitRunStatus(t, db, run.ID, "waiting_human")
		if got := eng.LiveEnabled(run.ID, "preview"); got != c.want {
			t.Errorf("cfg %v: LiveEnabled=%v", c.cfg, got)
		}
		if eng.LiveEnabled(run.ID, "nope") || eng.LiveEnabled("no-run", "preview") {
			t.Error("unknown node/run must be disabled")
		}
		if !c.want {
			if _, err := eng.ReactLiveAs("user:a", run.ID, "preview", genEvent("sid001")); !errors.Is(err, ErrLiveDisabled) {
				t.Errorf("cfg %v: err=%v, want ErrLiveDisabled", c.cfg, err)
			}
		}
	}
}

func TestChatLiveRequiresChoicesAndDoesNotAutoComplete(t *testing.T) {
	eng, _, p, runID := setupLive(t)
	hold := make(chan struct{})
	p.mu.Lock()
	p.reviseHold = hold
	p.mu.Unlock()
	defer func() {
		close(hold)
		_ = eng.waitReviewReadyForTest(runID, "preview", 5*time.Second)
	}()
	ev := models.LiveEvent{Op: models.LiveOpGenerate, SID: "page01", Scope: "page", Prompt: "重新设计登录弹窗"}
	if _, err := eng.ReactLiveWithAttachmentsAs("user:a", runID, "preview", ev, []models.PromptImage{{Data: "invalid!"}}, nil); err == nil {
		t.Fatal("invalid attachment must fail before creating the session")
	}
	if sessions := eng.LiveSessions(runID, "preview", false); len(sessions) != 0 {
		t.Fatalf("invalid attachment left a blocking session: %+v", sessions)
	}
	sess, err := eng.ReactLiveAs("user:a", runID, "preview", ev)
	if err != nil || sess.Mode != "replace" || sess.Selector != "" || sess.Summary != "页面候选" {
		t.Fatalf("page session: %+v, %v", sess, err)
	}
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", ev); err != nil {
		t.Fatalf("duplicate generate: %v", err)
	}
	second := ev
	second.SID = "page02"
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", second); err == nil {
		t.Fatal("chat generation bypassed one-open-session limit")
	}
	for _, report := range []mcp.LiveReport{
		{SID: ev.SID, State: models.LiveStateDone},
		{SID: ev.SID, State: models.LiveStateAccepted},
		{SID: ev.SID, State: models.LiveStateReady},
		{SID: ev.SID, State: models.LiveStateReady, Variants: []models.LiveVariant{{N: 1}}},
	} {
		if _, err := eng.ApplyLiveReport(runID, "preview", report); err == nil {
			t.Fatalf("allowed completion without selectable candidates: %+v", report)
		}
	}
	if _, err := eng.ApplyLiveReport(runID, "preview", mcp.LiveReport{SID: ev.SID, State: models.LiveStateReady, Variants: []models.LiveVariant{{N: 1}, {N: 2}, {N: 3}}}); err != nil {
		t.Fatal(err)
	}
	if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveOpen) {
		t.Fatalf("ready page candidates must wait for user selection: %v", err)
	}
}

func TestLiveGenerateAcceptFlow(t *testing.T) {
	eng, db, p, runID := setupLive(t)
	agentReports(eng, p, func(human string) *mcp.LiveReport {
		switch {
		case strings.Contains(human, "op: generate"):
			p.setLiveMarkers("sid001")
			return &mcp.LiveReport{SID: "sid001", State: "ready", File: "src/App.vue",
				Variants: []models.LiveVariant{{N: 1, Label: "层级"}, {N: 2, Label: "紧凑"}, {N: 3, Label: "强调色"}}}
		case strings.Contains(human, "op: accept"):
			if !strings.Contains(human, "采用变体: 2") || !strings.Contains(human, "gap=24") {
				return &mcp.LiveReport{SID: "sid001", State: "failed", Error: "bad prompt"}
			}
			p.setLiveMarkers()
			return &mcp.LiveReport{SID: "sid001", State: "accepted"}
		}
		return nil
	})

	ch, unsub := eng.broker.Subscribe(runID)
	defer unsub()

	sess, err := eng.ReactLiveAs("user:a", runID, "preview", genEvent("sid001"))
	if err != nil {
		t.Fatal(err)
	}
	if sess.State != models.LiveStateGenerating || sess.Mode != "replace" || sess.Summary == "" {
		t.Fatalf("new session = %+v", sess)
	}
	ready := waitLiveState(t, eng, runID, "sid001", models.LiveStateReady)
	if len(ready.Variants) != 3 || ready.File != "src/App.vue" {
		t.Fatalf("ready = %+v", ready)
	}
	if p.liveGuardCalls == 0 {
		time.Sleep(50 * time.Millisecond)
	}

	// Confirm is blocked while the session is open.
	if err := eng.ReactConfirmAs("user:a", runID, "preview", "", nil, nil, false); !errors.Is(err, ErrLiveOpen) {
		t.Fatalf("confirm err=%v, want ErrLiveOpen", err)
	}

	acc := models.LiveEvent{Op: models.LiveOpAccept, SID: "sid001", Variant: 2, Params: map[string]any{"gap": 24}}
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", acc); err != nil {
		t.Fatal(err)
	}
	// A repeated accept (refresh / double click) is a no-op, not a new turn.
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", acc); err != nil {
		t.Fatalf("duplicate accept: %v", err)
	}
	done := waitLiveState(t, eng, runID, "sid001", models.LiveStateAccepted)
	if done.Selected != 2 {
		t.Fatalf("selected = %d", done.Selected)
	}
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	calls := p.reviseCalls["preview"]
	p.mu.Unlock()
	if calls != 2 {
		t.Fatalf("revise calls = %d, want 2 (duplicate accept must not queue)", calls)
	}

	// Human turns carry the Live ref.
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", runID, "preview").Order("id desc").First(&conv)
	n := 0
	for _, m := range conv.Messages {
		if m.Live != nil && m.Live.SID == "sid001" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("live human turns = %d", n)
	}

	// Frames were published.
	sawLive := false
	for len(ch) > 0 {
		if strings.Contains(string(<-ch), `"type":"live"`) {
			sawLive = true
		}
	}
	if !sawLive {
		t.Fatal("expected live frames")
	}

	// Closed session no longer blocks; accept after the end is rejected.
	if err := eng.checkLiveClosed(runID, "preview"); err != nil {
		t.Fatalf("checkLiveClosed: %v", err)
	}
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpDiscard, SID: "sid001"}); err == nil {
		t.Fatal("discard after accept should fail")
	}
}

func TestLiveSettleWithoutReport(t *testing.T) {
	eng, _, p, runID := setupLive(t)
	// Agent writes markers but never calls live_update: no variants known → failed.
	agentReports(eng, p, func(human string) *mcp.LiveReport {
		if strings.Contains(human, "op: generate") {
			p.setLiveMarkers("sid002")
		}
		if strings.Contains(human, "op: discard") {
			p.setLiveMarkers()
		}
		return nil
	})
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", genEvent("sid002")); err != nil {
		t.Fatal(err)
	}
	failed := waitLiveState(t, eng, runID, "sid002", models.LiveStateFailed)
	if failed.Error == "" {
		t.Fatal("failed session needs an error")
	}
	// Leftover marker blocks confirm even though the row is failed (open).
	if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveOpen) {
		t.Fatalf("err=%v", err)
	}
	// Discard all → settle by scan (markers gone) → discarded.
	if n, err := eng.DiscardAllLiveAs("user:a", runID, "preview"); err != nil || n != 1 {
		t.Fatalf("discard all n=%d err=%v", n, err)
	}
	waitLiveState(t, eng, runID, "sid002", models.LiveStateDiscarded)
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := eng.checkLiveClosed(runID, "preview"); err != nil {
		t.Fatalf("after discard: %v", err)
	}
	// Stray marker from elsewhere still blocks.
	p.setLiveMarkers("zzz999")
	if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveOpen) {
		t.Fatalf("stray marker err=%v", err)
	}
}

func TestLiveOneOpenSessionAndIDReuse(t *testing.T) {
	eng, db, p, runID := setupLive(t)
	hold := make(chan struct{})
	p.mu.Lock()
	p.reviseHold = hold
	p.mu.Unlock()
	defer close(hold)
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", genEvent("sid003")); err != nil {
		t.Fatal(err)
	}
	// Same sid again while generating: duplicate → unchanged.
	if s, err := eng.ReactLiveAs("user:a", runID, "preview", genEvent("sid003")); err != nil || s.State != models.LiveStateGenerating {
		t.Fatalf("dup generate s=%+v err=%v", s, err)
	}
	// Another element while one is open: rejected.
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", genEvent("sid004")); err == nil || !strings.Contains(err.Error(), "未完成") {
		t.Fatalf("second session err=%v", err)
	}
	// Steer is allowed alongside.
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpSteer, SID: "steer01", Prompt: "紧凑一些"}); err != nil {
		t.Fatalf("steer: %v", err)
	}
	// Accept before ready is rejected.
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpAccept, SID: "sid003", Variant: 1}); err == nil {
		t.Fatal("accept while generating should fail")
	}
	// sid owned by another run cannot be reused.
	db.Create(&models.LiveSession{ID: "other01", RunID: "run-x", NodeID: "preview", State: models.LiveStateReady})
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpSteer, SID: "other01", Prompt: "x"}); err == nil {
		t.Fatal("foreign sid should be rejected")
	}
	if got := len(eng.LiveSessions(runID, "preview", true)); got != 2 {
		t.Fatalf("open sessions = %d", got)
	}
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: "bad", SID: "sid003"}); err == nil {
		t.Fatal("bad op should fail")
	}
}

func TestLiveApplyReportValidation(t *testing.T) {
	eng, db, _, runID := setupLive(t)
	db.Create(&models.LiveSession{ID: "sid005", RunID: runID, NodeID: "preview", Mode: "replace", State: models.LiveStateGenerating})
	for _, r := range []mcp.LiveReport{
		{SID: "x", State: "ready"},
		{SID: "missing1", State: "ready"},
		{SID: "sid005", State: "accepted"},
		{SID: "sid005", State: "ready"},
		{SID: "sid005", State: "ready", Variants: []models.LiveVariant{{N: 0}}},
	} {
		if _, err := eng.ApplyLiveReport(runID, "preview", r); err == nil {
			t.Errorf("report %+v should fail", r)
		}
	}
	s, err := eng.ApplyLiveReport(runID, "preview", mcp.LiveReport{SID: "sid005", State: "failed"})
	if err != nil || s.Error == "" {
		t.Fatalf("failed report s=%+v err=%v", s, err)
	}
	s, err = eng.ApplyLiveReport(runID, "preview", mcp.LiveReport{SID: "sid005", State: "ready", File: " a.vue ", Variants: []models.LiveVariant{{N: 2, Label: "b"}, {N: 1}}})
	if err != nil || s.File != "a.vue" || s.Variants[0].N != 1 || s.Error != "" {
		t.Fatalf("ready s=%+v err=%v", s, err)
	}
}

func TestLiveReplyWithCtx(t *testing.T) {
	eng, db, p, runID := setupLive(t)
	var prompts []string
	p.mu.Lock()
	p.reviseHook = func(_ runtime.NodeReq, human string) { prompts = append(prompts, human) }
	p.mu.Unlock()
	db.Create(&models.LiveSession{ID: "sid006", RunID: runID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady,
		Variants: []models.LiveVariant{{N: 2, Label: "紧凑"}}})
	if err := eng.ReactReplyLiveCtxAs("user:a", runID, "preview", "标题再大点", nil, nil, &models.LiveCtx{SID: "sid006", Current: 2}); err != nil {
		t.Fatal(err)
	}
	// Unknown session: falls back to a plain reply.
	if err := eng.ReactReplyLiveCtxAs("user:a", runID, "preview", "hi", nil, nil, &models.LiveCtx{SID: "nosuch1", Current: 1}); err != nil {
		t.Fatal(err)
	}
	if err := eng.ReactReplyLiveCtxAs("user:a", runID, "preview", "  ", nil, nil, &models.LiveCtx{SID: "sid006", Current: 2}); err == nil {
		t.Fatal("empty text should fail")
	}
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[0], "变体 2(紧凑)") || !strings.Contains(prompts[0], "标题再大点") || strings.Contains(prompts[1], "Live 上下文") {
		t.Fatalf("prompts=%q", prompts)
	}
}

func TestSettleLiveState(t *testing.T) {
	type tc struct {
		state, mode         string
		variants            int
		present, scan, intr bool
		want                string
	}
	for _, c := range []tc{
		{models.LiveStateGenerating, "replace", 3, true, false, false, models.LiveStateFailed},
		{models.LiveStateGenerating, "steer", 0, false, true, false, models.LiveStateDone},
		{models.LiveStateGenerating, "steer", 0, false, true, true, models.LiveStateFailed},
		{models.LiveStateGenerating, "replace", 3, true, true, false, models.LiveStateReady},
		{models.LiveStateGenerating, "replace", 0, true, true, false, models.LiveStateFailed},
		{models.LiveStateGenerating, "replace", 3, false, true, true, models.LiveStateFailed},
		{models.LiveStateRefining, "replace", 3, false, true, false, models.LiveStateFailed},
		{models.LiveStateAccepting, "replace", 3, true, true, false, models.LiveStateFailed},
		{models.LiveStateAccepting, "replace", 3, false, true, false, models.LiveStateAccepted},
		{models.LiveStateAccepting, "replace", 3, false, true, true, models.LiveStateFailed},
		{models.LiveStateDiscarding, "replace", 3, true, true, false, models.LiveStateFailed},
		{models.LiveStateDiscarding, "replace", 3, false, true, false, models.LiveStateDiscarded},
	} {
		s := &models.LiveSession{State: c.state, Mode: c.mode, Variants: make([]models.LiveVariant, c.variants)}
		if got, _ := settleLiveState(s, c.present, c.scan, c.intr); got != c.want {
			t.Errorf("%+v → %s", c, got)
		}
	}
}

func TestLiveScanUnavailable(t *testing.T) {
	eng, _, p, runID := setupLive(t)
	p.mu.Lock()
	p.liveNotParked = true
	p.mu.Unlock()
	if present, scanned := eng.liveMarkerPresent(runID, "preview", "sid"); present || scanned {
		t.Fatal("not parked → not scanned")
	}
	p.mu.Lock()
	p.liveNotParked = false
	p.liveScanErr = errors.New("ssh down")
	p.mu.Unlock()
	if _, scanned := eng.liveMarkerPresent(runID, "preview", "sid"); scanned {
		t.Fatal("scan error → not scanned")
	}
	if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveScanFailed) {
		t.Fatalf("scan error must block confirm: %v", err)
	}
	p.mu.Lock()
	p.liveScanErr = nil
	p.liveNotParked = true
	p.mu.Unlock()
	if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveScanFailed) {
		t.Fatalf("not parked must block confirm: %v", err)
	}
	type scanless struct{ runtime.ExecProvider }
	eng.provider = scanless{p}
	p.mu.Lock()
	p.liveNotParked = false
	p.mu.Unlock()
	if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveScanFailed) {
		t.Fatalf("missing scanner must block confirm: %v", err)
	}
	if clipString("abcdef", 3) != "abc" || clipString("ab", 3) != "ab" {
		t.Fatal("clipString")
	}
}

func TestCheckLiveClosedOpenSteer(t *testing.T) {
	eng, db, p, runID := setupLive(t)
	p.setLiveMarkers()
	db.Create(&models.LiveSession{
		ID: "steer01", RunID: runID, NodeID: "preview", Mode: "steer", State: models.LiveStateGenerating,
	})
	if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveOpen) {
		t.Fatalf("open steer err=%v, want ErrLiveOpen", err)
	}
}

func TestLiveChatAcceptFreezesVariantAndParams(t *testing.T) {
	for _, reportFinal := range []bool{true, false} {
		t.Run(fmt.Sprintf("final_report_%v", reportFinal), func(t *testing.T) {
			eng, db, p, runID := setupLive(t)
			db.Create(&models.LiveSession{ID: "chat01", RunID: runID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady,
				Variants: []models.LiveVariant{{N: 1}, {N: 2, Label: "紧凑"}}})
			p.setLiveMarkers("chat01")
			start, release := make(chan struct{}), make(chan struct{})
			results := make(chan error, 1)
			p.mu.Lock()
			p.reviseHook = func(req runtime.NodeReq, human string) {
				close(start)
				<-release
				if !strings.Contains(human, "变体 2(紧凑)") || !strings.Contains(human, "gap=24px") {
					results <- fmt.Errorf("incorrect snapshot prompt: %s", human)
					return
				}
				for i := 0; i < 2; i++ { // repeated begin is idempotent
					if _, err := eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "chat01", State: models.LiveStateAccepting}); err != nil {
						results <- err
						return
					}
				}
				p.setLiveMarkers()
				var err error
				if reportFinal {
					_, err = eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "chat01", State: models.LiveStateAccepted})
				}
				results <- err
			}
			p.mu.Unlock()
			ctx := &models.LiveCtx{SID: "chat01", Current: 2, Params: map[string]any{"gap": "24px", "tone": "soft"}}
			if err := eng.ReactReplyLiveCtxAs("user:a", runID, "preview", "就用这个", nil, nil, ctx); err != nil {
				t.Fatal(err)
			}
			<-start
			ctx.Current = 1
			ctx.Params["gap"] = "48px" // later tuning must not mutate the queued request
			close(release)
			if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := <-results; err != nil {
				t.Fatal(err)
			}
			sess := waitLiveState(t, eng, runID, "chat01", models.LiveStateAccepted)
			if sess.Selected != 2 || sess.FinalParams["gap"] != "24px" {
				t.Fatalf("adoption changed with later page state: %+v", sess)
			}
			if err := eng.checkLiveClosed(runID, "preview"); err != nil {
				t.Fatalf("completed Chat acceptance still blocks confirm: %v", err)
			}
			var conv models.ReactConversation
			db.Where("run_id = ? AND node_id = ?", runID, "preview").Order("id desc").First(&conv)
			var ref *models.LiveRef
			for _, msg := range conv.Messages {
				if msg.Role == "human" && msg.Text == "就用这个" {
					ref = msg.Live
				}
			}
			if ref == nil || ref.SID != "chat01" || ref.Variant != 2 {
				t.Fatalf("Chat turn lost its Live context: %+v", ref)
			}
		})
	}
}

func TestLiveChatBeginRequiresActivePermissionAndContext(t *testing.T) {
	for _, withCtx := range []bool{true, false} {
		t.Run(fmt.Sprintf("react_only_context_%v", withCtx), func(t *testing.T) {
			eng, db, p, runID := setupLive(t)
			db.Create(&models.LiveSession{ID: "chat02", RunID: runID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady,
				Variants: []models.LiveVariant{{N: 1}}})
			results := make(chan error, 4)
			p.mu.Lock()
			p.reviseHook = func(req runtime.NodeReq, human string) {
				for _, state := range []string{models.LiveStateAccepting, models.LiveStateRefining, models.LiveStateAccepted, models.LiveStateDiscarded} {
					_, err := eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "chat02", State: state})
					results <- err
				}
			}
			p.mu.Unlock()
			var ctx *models.LiveCtx
			if withCtx {
				ctx = &models.LiveCtx{SID: "chat02", Current: 1}
			}
			if err := eng.ReactReplyLiveCtxWithPermissionAs("share:read-only", runID, "preview", "就用这个", nil, nil, ctx, false); err != nil {
				t.Fatal(err)
			}
			if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 4; i++ {
				if err := <-results; err == nil || !strings.Contains(err.Error(), "权限") {
					t.Fatalf("react_only Live mutation was not denied: %v", err)
				}
			}
			sess, _ := eng.liveSession(runID, "preview", "chat02")
			if sess.State != models.LiveStateReady || sess.Selected != 0 {
				t.Fatalf("denied comment changed session: %+v", sess)
			}
			for _, state := range []string{models.LiveStateAccepting, models.LiveStateRefining} {
				if _, err := eng.ApplyLiveReport(runID, "preview", mcp.LiveReport{SID: "chat02", State: state}); err == nil {
					t.Fatalf("%s should require an active Chat context", state)
				}
			}
		})
	}
}

func TestLiveChatRefineAndInvalidVariant(t *testing.T) {
	eng, db, p, runID := setupLive(t)
	db.Create(&models.LiveSession{ID: "chat03", RunID: runID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady,
		Variants: []models.LiveVariant{{N: 1}, {N: 2}}})
	p.setLiveMarkers("chat03")
	if err := eng.ReactReplyLiveCtxAs("user:a", runID, "preview", "标题大一点", nil, nil, &models.LiveCtx{SID: "chat03", Current: 3}); err == nil {
		t.Fatal("nonexistent viewed variant should be rejected")
	}
	results := make(chan error, 1)
	p.mu.Lock()
	p.reviseHook = func(req runtime.NodeReq, human string) {
		if _, err := eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "chat03", State: models.LiveStateAccepting, Variant: 2}); err == nil || !strings.Contains(err.Error(), "重新发送") {
			results <- fmt.Errorf("adopting an unviewed candidate should request switch/resend: %v", err)
			return
		}
		s, err := eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "chat03", State: models.LiveStateRefining, Variant: 2})
		if err == nil && s.Selected != 2 {
			err = fmt.Errorf("explicit refine ordinal lost: %+v", s)
		}
		results <- err
		// No final ready report: the attached LiveRef must settle the turn.
	}
	p.mu.Unlock()
	if err := eng.ReactReplyLiveCtxAs("user:a", runID, "preview", "2 的标题大一点", nil, nil, &models.LiveCtx{SID: "chat03", Current: 1}); err != nil {
		t.Fatal(err)
	}
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	waitLiveState(t, eng, runID, "chat03", models.LiveStateReady)
}

func TestFailedSteerRetryOrDismiss(t *testing.T) {
	for _, action := range []string{"retry", "discard", "discard-all"} {
		t.Run(action, func(t *testing.T) {
			eng, _, p, runID := setupLive(t)
			first := true
			agentReports(eng, p, func(human string) *mcp.LiveReport {
				if first {
					first = false
					return &mcp.LiveReport{SID: "steer01", State: models.LiveStateFailed, Error: "could not complete adjustment"}
				}
				return &mcp.LiveReport{SID: "steer01", State: models.LiveStateDone}
			})
			if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpSteer, SID: "steer01", Prompt: "紧凑一些"}); err != nil {
				t.Fatal(err)
			}
			waitLiveState(t, eng, runID, "steer01", models.LiveStateFailed)
			if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := eng.checkLiveClosed(runID, "preview"); !errors.Is(err, ErrLiveOpen) {
				t.Fatalf("failed request must await explicit recovery: %v", err)
			}
			want := models.LiveStateDiscarded
			switch action {
			case "retry":
				want = models.LiveStateDone
				if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpSteer, SID: "steer01", Prompt: "再试一次紧凑布局"}); err != nil {
					t.Fatal(err)
				}
			case "discard":
				if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpDiscard, SID: "steer01"}); err != nil {
					t.Fatal(err)
				}
			case "discard-all":
				if n, err := eng.DiscardAllLiveAs("user:a", runID, "preview"); err != nil || n != 1 {
					t.Fatalf("discard-all failed steer: n=%d err=%v", n, err)
				}
			}
			waitLiveState(t, eng, runID, "steer01", want)
			if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := eng.checkLiveClosed(runID, "preview"); err != nil {
				t.Fatalf("recovered steer blocks confirm: %v", err)
			}
		})
	}
}

func TestInterruptedSteerDoesNotInferSuccessFromNoMarkers(t *testing.T) {
	eng, _, p, runID := setupLive(t)
	p.mu.Lock()
	p.reviseErr = errors.New("turn interrupted")
	p.mu.Unlock()
	p.setLiveMarkers()
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpSteer, SID: "steer02", Prompt: "紧凑一些"}); err != nil {
		t.Fatal(err)
	}
	waitLiveState(t, eng, runID, "steer02", models.LiveStateFailed)
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if n, err := eng.DiscardAllLiveAs("user:a", runID, "preview"); err != nil || n != 1 {
		t.Fatalf("dismiss interrupted steer: n=%d err=%v", n, err)
	}
	if err := eng.checkLiveClosed(runID, "preview"); err != nil {
		t.Fatal(err)
	}
}

func TestCancelQueuedSteerCanBeDismissed(t *testing.T) {
	eng, _, p, runID := setupLive(t)
	hold := make(chan struct{})
	p.mu.Lock()
	p.reviseHold = hold
	p.mu.Unlock()
	if err := eng.ReactReplyAs("user:a", runID, "preview", "先检查页面", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpSteer, SID: "steer03", Prompt: "紧凑一些"}); err != nil {
		t.Fatal(err)
	}
	if err := eng.CancelReviewSession(runID, "preview"); err != nil {
		t.Fatal(err)
	}
	waitLiveState(t, eng, runID, "steer03", models.LiveStateFailed)
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if n, err := eng.DiscardAllLiveAs("user:a", runID, "preview"); err != nil || n != 1 {
		t.Fatalf("dismiss cancelled queued request: n=%d err=%v", n, err)
	}
	if err := eng.checkLiveClosed(runID, "preview"); err != nil {
		t.Fatal(err)
	}
}

func TestLiveMarksReachQueuedAgentPrompt(t *testing.T) {
	for _, op := range []string{models.LiveOpGenerate, models.LiveOpInsert, models.LiveOpRefine} {
		t.Run(op, func(t *testing.T) {
			eng, db, p, runID := setupLive(t)
			if op == models.LiveOpRefine {
				db.Create(&models.LiveSession{ID: "marks01", RunID: runID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady,
					Variants: []models.LiveVariant{{N: 1}}})
			}
			prompts := make(chan string, 1)
			p.mu.Lock()
			p.reviseHook = func(req runtime.NodeReq, human string) {
				p.setLiveMarkers("marks01")
				_, _ = eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "marks01", State: models.LiveStateReady, Variants: []models.LiveVariant{{N: 1}, {N: 2}}})
				prompts <- human
			}
			p.mu.Unlock()
			ev := models.LiveEvent{Op: op, SID: "marks01", Position: "before", Variant: 1,
				Element: &models.LiveElement{Selector: "#hero", TagName: "section"},
				Marks: []models.LiveMark{
					{Kind: "draw", Points: []models.LivePoint{{X: 0.1, Y: 0.2}, {X: 0.8, Y: 0.7}}, Targets: []models.LiveMarkTarget{{Selector: "#hero button", Text: "Start"}}},
					{Kind: "note", Points: []models.LivePoint{{X: 0.5, Y: 0.4}}, Text: "放大这里的按钮"},
				}}
			if _, err := eng.ReactLiveAs("user:a", runID, "preview", ev); err != nil {
				t.Fatal(err)
			}
			if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			prompt := <-prompts
			for _, want := range []string{"圈画路径", "(10.0%, 20.0%) → (80.0%, 70.0%)", "#hero button", "Start", "定位注释", "(50.0%, 40.0%)", "放大这里的按钮", "不可信页面信息"} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("queued Effective omitted mark %q: %s", want, prompt)
				}
			}
		})
	}
}

func TestInterruptedChatAcceptanceRetriesFrozenRequestWithoutLiveCtx(t *testing.T) {
	eng, db, p, runID := setupLive(t)
	db.Create(&models.LiveSession{ID: "accept01", RunID: runID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady,
		Variants: []models.LiveVariant{{N: 1}, {N: 2}}})
	results := make(chan error, 1)
	p.mu.Lock()
	p.reviseErr = errors.New("turn interrupted")
	p.reviseHook = func(req runtime.NodeReq, _ string) {
		_, err := eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "accept01", State: models.LiveStateAccepting})
		p.setLiveMarkers() // wrapper cleaned just before the interruption
		results <- err
	}
	p.mu.Unlock()
	if err := eng.ReactReplyLiveCtxAs("user:a", runID, "preview", "就用这个", nil, nil, &models.LiveCtx{SID: "accept01", Current: 2, Params: map[string]any{"gap": "24px"}}); err != nil {
		t.Fatal(err)
	}
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	failed := waitLiveState(t, eng, runID, "accept01", models.LiveStateFailed)
	if !failed.RetryAccept || failed.Selected != 2 || failed.FinalParams["gap"] != "24px" {
		t.Fatalf("interrupted acceptance lost recovery snapshot: %+v", failed)
	}
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpAccept, SID: "accept01", Variant: 1}); err == nil {
		t.Fatal("recovery must not switch to a different target")
	}
	p.mu.Lock()
	p.reviseErr = nil
	p.reviseHook = func(req runtime.NodeReq, human string) {
		if !strings.Contains(human, "采用重试") || !strings.Contains(human, "gap=24px") || strings.Contains(human, "999px") {
			results <- fmt.Errorf("retry did not use frozen request: %s", human)
			return
		}
		_, err := eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "accept01", State: models.LiveStateAccepted})
		results <- err
	}
	p.mu.Unlock()
	// This is the card's explicit recovery API, even after page current=0 and
	// LiveCtx are cleared. Later/stale parameter values cannot replace the snapshot.
	if _, err := eng.ReactLiveAs("user:a", runID, "preview", models.LiveEvent{Op: models.LiveOpAccept, SID: "accept01", Variant: 2, Params: map[string]any{"gap": "999px"}}); err != nil {
		t.Fatal(err)
	}
	if err := eng.waitReviewReadyForTest(runID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	accepted := waitLiveState(t, eng, runID, "accept01", models.LiveStateAccepted)
	if accepted.RetryAccept || accepted.Selected != 2 || accepted.FinalParams["gap"] != "24px" {
		t.Fatalf("retried acceptance result: %+v", accepted)
	}
	if err := eng.checkLiveClosed(runID, "preview"); err != nil {
		t.Fatalf("retried cleanup still blocks confirm: %v", err)
	}
}
