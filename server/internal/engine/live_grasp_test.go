package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// Unlike a review producer, Grasp must execute Live through ReactReply. This
// double exercises the real queue, durable transcript, reports and confirm gate.
type graspLiveProvider struct {
	*fakeProvider
	reply func(runtime.NodeReq, string) runtime.ReactTurn
	wrap  *runtime.ReactTurn
}

func (p *graspLiveProvider) OfferCommitOnConfirm(ctx context.Context, req runtime.NodeReq) runtime.ReactTurn {
	t := p.fakeProvider.OfferCommitOnConfirm(ctx, req)
	if p.wrap != nil {
		return *p.wrap
	}
	return t
}

func (p *graspLiveProvider) ReactReply(_ context.Context, req runtime.NodeReq, _ []models.ReactMessage, human string, _ []models.PromptImage, _ bool) runtime.ReactTurn {
	return p.reply(req, human)
}

func TestGraspLiveLifecycle(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, liveGraph(capsClarify))
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if !eng.LiveEnabled(run.ID, "preview") {
		t.Fatal("a clarify Agent granted set_preview must enable Live")
	}
	gp := &graspLiveProvider{fakeProvider: p}
	eng.provider = gp
	var reportErr error
	gp.reply = func(req runtime.NodeReq, human string) runtime.ReactTurn {
		sid := "grasp01"
		if strings.Contains(human, "grasp02") {
			sid = "grasp02"
		}
		r := mcp.LiveReport{SID: sid, State: models.LiveStateReady, Variants: []models.LiveVariant{{N: 1}, {N: 2}}}
		switch {
		case strings.Contains(human, "op: accept"):
			p.setLiveMarkers()
			r.State = models.LiveStateAccepted
		case strings.Contains(human, "op: discard"):
			p.setLiveMarkers()
			r.State = models.LiveStateDiscarded
		default:
			p.setLiveMarkers(sid)
		}
		_, reportErr = eng.ApplyLiveReport(req.RunID, req.NodeID, r)
		return runtime.ReactTurn{Msg: "Live done"}
	}
	send := func(ev models.LiveEvent, want string) {
		t.Helper()
		if _, err := eng.ReactLiveAs("user:test", run.ID, "preview", ev); err != nil {
			t.Fatal(err)
		}
		if err := eng.waitReviewReadyForTest(run.ID, "preview", 5*time.Second); err != nil {
			t.Fatal(err)
		}
		if reportErr != nil {
			t.Fatal(reportErr)
		}
		waitLiveState(t, eng, run.ID, ev.SID, want)
	}
	send(genEvent("grasp01"), models.LiveStateReady)
	if err := eng.ReactConfirmAs("user:test", run.ID, "preview", "", nil, nil, false); !errors.Is(err, ErrLiveOpen) {
		t.Fatalf("open confirm = %v", err)
	}
	send(models.LiveEvent{Op: models.LiveOpRefine, SID: "grasp01", Variant: 2, Prompt: "标题更大"}, models.LiveStateReady)
	send(models.LiveEvent{Op: models.LiveOpAccept, SID: "grasp01", Variant: 2}, models.LiveStateAccepted)
	if err := eng.checkLiveClosed(run.ID, "preview"); err != nil {
		t.Fatal(err)
	}
	// Even terminal sessions must not permit orphan preview markup to ship.
	p.setLiveMarkers("orphan1")
	if err := eng.ReactConfirmAs("user:test", run.ID, "preview", "", nil, nil, false); !errors.Is(err, ErrLiveOpen) {
		t.Fatalf("orphan confirm = %v", err)
	}
	p.setLiveMarkers()
	send(genEvent("grasp02"), models.LiveStateReady)
	send(models.LiveEvent{Op: models.LiveOpDiscard, SID: "grasp02"}, models.LiveStateDiscarded)
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&conv)
	if conv.Done {
		t.Fatal("adopting must not finish Grasp")
	}
	refs := 0
	for _, m := range conv.Messages {
		if m.Live != nil {
			refs++
		}
	}
	if refs != 5 {
		t.Fatalf("Live transcript refs = %d, want 5", refs)
	}
	p.mu.Lock()
	reviews := p.reviseCalls["preview"]
	p.mu.Unlock()
	if reviews != 0 {
		t.Fatal("Grasp was incorrectly routed through review producer")
	}
}

func TestGraspLiveSettlementAndUnusedConfirm(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, liveGraph(capsClarify))
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	// Missing scanner state must not strand ordinary, never-used Live chats.
	p.mu.Lock()
	p.liveScanErr = errors.New("sandbox unavailable")
	p.mu.Unlock()
	if err := eng.checkLiveClosed(run.ID, "preview"); err != nil {
		t.Fatal(err)
	}
	confirmCalled := false
	eng.provider = &graspLiveProvider{fakeProvider: p, reply: func(runtime.NodeReq, string) runtime.ReactTurn {
		confirmCalled = true
		return runtime.ReactTurn{Msg: "仍需写入澄清与计划"}
	}}
	if err := eng.ReactConfirmAs("user:test", run.ID, "preview", "", nil, nil, false); err == nil || errors.Is(err, ErrLiveScanFailed) || !confirmCalled {
		t.Fatalf("ordinary confirmation should reach normal deliverable checks: %v called=%v", err, confirmCalled)
	}
	gp := &graspLiveProvider{fakeProvider: p, reply: func(runtime.NodeReq, string) runtime.ReactTurn {
		return runtime.ReactTurn{Msg: "timeout", Interrupted: true}
	}}
	eng.provider = gp
	if _, err := eng.ReactLiveAs("user:test", run.ID, "preview", genEvent("failed01")); err != nil {
		t.Fatal(err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	waitLiveState(t, eng, run.ID, "failed01", models.LiveStateFailed)
	db.Model(&models.LiveSession{}).Where("id = ?", "failed01").Update("state", models.LiveStateDiscarded)
	if err := eng.checkLiveClosed(run.ID, "preview"); !errors.Is(err, ErrLiveScanFailed) {
		t.Fatalf("used Live without scanner must fail closed: %v", err)
	}
}

func TestGraspLiveFailedAcceptKeepsSnapshot(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, liveGraph(capsClarify))
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	db.Create(&models.LiveSession{ID: "accept01", RunID: run.ID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady, Variants: []models.LiveVariant{{N: 1}, {N: 2}}})
	eng.provider = &graspLiveProvider{fakeProvider: p, reply: func(runtime.NodeReq, string) runtime.ReactTurn {
		p.setLiveMarkers() // provider fails after removing the wrapper
		return runtime.ReactTurn{Msg: "(澄清回复失败:TLS)", Err: errors.New("TLS")}
	}}
	if _, err := eng.ReactLiveAs("user:test", run.ID, "preview", models.LiveEvent{Op: models.LiveOpAccept, SID: "accept01", Variant: 2, Params: map[string]any{"gap": "24px"}}); err != nil {
		t.Fatal(err)
	}
	if err := eng.waitReviewReadyForTest(run.ID, "preview", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	s := waitLiveState(t, eng, run.ID, "accept01", models.LiveStateFailed)
	if !s.RetryAccept || s.Selected != 2 || s.FinalParams["gap"] != "24px" {
		t.Fatalf("lost accept recovery snapshot: %+v", s)
	}
	if _, err := eng.EnqueueClarifyRetryLast(run.ID, "preview"); err == nil || !strings.Contains(err.Error(), "Live 卡片") {
		t.Fatalf("generic retry must not replay Live display text: %v", err)
	}
}

func TestGraspLiveConfirmWrapsEditsBeforeFinishing(t *testing.T) {
	for _, used := range []bool{false, true} {
		t.Run(fmt.Sprintf("used=%v", used), func(t *testing.T) {
			eng, db, p := setupEngineGraphP(t, liveGraph(capsClarify))
			run, err := eng.StartRun("wf", nil, "test")
			if err != nil {
				t.Fatal(err)
			}
			waitRunStatus(t, db, run.ID, "waiting_human")
			if used {
				db.Create(&models.LiveSession{ID: "adopted1", RunID: run.ID, NodeID: "preview", State: models.LiveStateAccepted})
			}
			p.wrapUpMsg = "已提交并推送 Live 改动"
			called := false
			eng.provider = &graspLiveProvider{fakeProvider: p, reply: func(runtime.NodeReq, string) runtime.ReactTurn {
				called = true
				p.mu.Lock()
				count := p.wrapUpCalls["preview"]
				retired := p.wrapUpAfterRetire
				p.mu.Unlock()
				want := 0
				if used {
					want = 1
				}
				if count != want || retired {
					t.Errorf("wrap-up must precede forced finish only when Live was used: count=%d want=%d retired=%v", count, want, retired)
				}
				// Missing required deliverables must still keep Grasp open.
				return runtime.ReactTurn{Msg: "需补齐澄清与计划", Done: false}
			}}
			if err := eng.ReactConfirmAs("user:test", run.ID, "preview", "", nil, nil, false); err == nil || !called {
				t.Fatalf("normal force contract bypassed: %v called=%v", err, called)
			}
			var conv models.ReactConversation
			db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&conv)
			found := false
			for _, m := range conv.Messages {
				if m.Text == p.wrapUpMsg {
					found = true
				}
			}
			if found != used || conv.Done {
				t.Fatalf("wrap-up narration/normal confirmation state: found=%v used=%v done=%v", found, used, conv.Done)
			}
		})
	}
}

func TestGraspLiveSuccessfulConfirmPreservesWrapupAccounting(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, liveGraph(capsClarify))
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	db.Create(&models.LiveSession{ID: "adopted1", RunID: run.ID, NodeID: "preview", State: models.LiveStateAccepted})
	gp := &graspLiveProvider{fakeProvider: p, wrap: &runtime.ReactTurn{
		Msg: "pushed Live edits", Events: []models.AcpEvent{
			{Kind: "tool_call", Title: "git_push", Status: "completed"},
			{Kind: "message", Text: "live commit push"},
		},
		Usage:        &models.TokenUsage{InputTokens: 11, OutputTokens: 7},
		UsageByModel: models.TokenUsageByModel{"model": {InputTokens: 11, OutputTokens: 7}},
	}}
	gp.reply = func(req runtime.NodeReq, human string) runtime.ReactTurn {
		reply := p.ReactReply(context.Background(), req, nil, human, nil, true)
		reply.Events = append([]models.AcpEvent{{Kind: "tool_call", Title: "write", Status: "completed"}}, reply.Events...)
		reply.Result.Events = []models.AcpEvent{{Kind: "message", Text: "force confirm"}}
		reply.Result.Usage = &models.TokenUsage{InputTokens: 5, OutputTokens: 2}
		reply.Result.UsageByModel = models.TokenUsageByModel{"model": {InputTokens: 5, OutputTokens: 2}}
		return reply
	}
	eng.provider = gp
	if err := eng.ReactConfirmAs("user:test", run.ID, "preview", "", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if sr.Usage == nil || sr.Usage.InputTokens != 16 || sr.Usage.OutputTokens != 9 || sr.UsageByModel["model"].Total() != 25 {
		t.Fatalf("lost/doubled wrapup accounting: %+v %v", sr.Usage, sr.UsageByModel)
	}
	found := false
	for _, event := range sr.Events {
		if event.Text == "live commit push" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing wrap-up events in final state: %+v", sr.Events)
	}
	var conv models.ReactConversation
	db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&conv)
	var wrapTools, replyTools []models.ReactTool
	for _, m := range conv.Messages {
		if m.Role != "agent" {
			continue
		}
		if m.Text == "pushed Live edits" {
			wrapTools = m.Tools
		} else {
			replyTools = m.Tools
		}
	}
	if len(wrapTools) != 1 || wrapTools[0].Title != "git_push" {
		t.Fatalf("wrap-up tools: %+v", wrapTools)
	}
	if len(replyTools) != 1 || replyTools[0].Title != "write" {
		t.Fatalf("reply must not repeat the persisted wrap-up tools: %+v", replyTools)
	}
}
