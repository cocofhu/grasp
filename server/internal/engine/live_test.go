package engine

import (
	"errors"
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
		{models.LiveStateGenerating, "replace", 3, true, true, false, models.LiveStateReady},
		{models.LiveStateGenerating, "replace", 0, true, true, false, models.LiveStateFailed},
		{models.LiveStateGenerating, "replace", 3, false, true, true, models.LiveStateFailed},
		{models.LiveStateRefining, "replace", 3, false, true, false, models.LiveStateFailed},
		{models.LiveStateAccepting, "replace", 3, true, true, false, models.LiveStateFailed},
		{models.LiveStateAccepting, "replace", 3, false, true, false, models.LiveStateAccepted},
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
