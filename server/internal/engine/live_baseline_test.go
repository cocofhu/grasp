package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

type livePreparingProvider struct {
	*fakeProvider
	eng          *Engine
	muBaseline   sync.Mutex
	started      chan struct{}
	release      chan struct{}
	repoReady    bool
	prepares     int
	generateRuns int
	prepareErr   error
}

func (p *livePreparingProvider) PrepareLiveBaseline(context.Context, string, string) error {
	p.muBaseline.Lock()
	defer p.muBaseline.Unlock()
	p.prepares++
	if !p.repoReady {
		return errors.New("baseline started before the preceding clone turn finished")
	}
	return p.prepareErr
}

func (p *livePreparingProvider) ReviseInPlace(_ context.Context, req runtime.NodeReq, _ []models.ReactMessage, human string, _ []models.PromptImage) runtime.ReactTurn {
	if human == "clone workspace" {
		close(p.started)
		<-p.release
		p.muBaseline.Lock()
		p.repoReady = true
		p.muBaseline.Unlock()
		return runtime.ReactTurn{Msg: "预览已就绪"}
	}
	r := mcp.LiveReport{SID: "page01", State: models.LiveStateReady, Variants: []models.LiveVariant{{N: 1}, {N: 2}, {N: 3}}}
	if strings.Contains(human, "op: discard") {
		r.State = models.LiveStateDiscarded
		p.setLiveMarkers()
	} else {
		p.muBaseline.Lock()
		p.generateRuns++
		prepared := p.prepares > 0
		p.muBaseline.Unlock()
		if !prepared {
			return runtime.ReactTurn{Err: errors.New("agent edited before baseline initialization")}
		}
		p.setLiveMarkers("page01")
	}
	_, err := p.eng.ApplyLiveReport(req.RunID, req.NodeID, r)
	return runtime.ReactTurn{Msg: "候选已更新", Err: err}
}

func (p *livePreparingProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, _ bool) runtime.ReactTurn {
	return p.ReviseInPlace(ctx, req, history, human, images)
}

func TestLiveBaselinePreparesAtExecutionAndFailureKeepsDialogue(t *testing.T) {
	for _, nodeType := range []string{"app_preview", "grasp", "approve"} {
		for _, fail := range []bool{false, true} {
			name := nodeType + "/success"
			if fail {
				name = nodeType + "/failure"
			}
			t.Run(name, func(t *testing.T) {
				graph := liveGraph(map[string]any{"direct_preview": true})
				graph.Nodes[1].Type = nodeType
				eng, db, base := setupEngineGraphP(t, graph)
				base.skipOutcome = true
				run, err := eng.StartRun("wf", nil, "test")
				if err != nil {
					t.Fatal(err)
				}
				waitRunStatus(t, db, run.ID, "waiting_human")
				p := &livePreparingProvider{fakeProvider: base, eng: eng, started: make(chan struct{}), release: make(chan struct{})}
				if fail {
					p.prepareErr = errors.New("sandbox source read failed")
				}
				eng.provider = p
				var release sync.Once
				defer release.Do(func() { close(p.release) })
				if err := eng.ReactReplyAs("user:a", run.ID, "preview", "clone workspace", nil, nil, false); err != nil {
					t.Fatal(err)
				}
				select {
				case <-p.started:
				case <-time.After(5 * time.Second):
					t.Fatal("preceding turn did not start")
				}
				if _, err := eng.ReactLiveAs("user:a", run.ID, "preview", models.LiveEvent{Op: models.LiveOpGenerate, SID: "page01", Scope: "page", Prompt: "给页面三个方案"}); err != nil {
					t.Fatal(err)
				}
				p.muBaseline.Lock()
				preparedEarly := p.prepares != 0
				p.muBaseline.Unlock()
				if preparedEarly {
					t.Fatal("baseline was captured at enqueue rather than execution")
				}
				release.Do(func() { close(p.release) })
				if err := eng.waitReviewReadyForTest(run.ID, "preview", 5*time.Second); err != nil {
					t.Fatal(err)
				}
				want, runs := models.LiveStateReady, 1
				if fail {
					want, runs = models.LiveStateFailed, 0
				}
				waitLiveState(t, eng, run.ID, "page01", want)
				p.muBaseline.Lock()
				prepares, generated := p.prepares, p.generateRuns
				p.muBaseline.Unlock()
				if prepares != 1 || generated != runs {
					t.Fatalf("prepares=%d agent generations=%d, want 1/%d", prepares, generated, runs)
				}
				var conv models.ReactConversation
				db.Where("run_id = ? AND node_id = ?", run.ID, "preview").First(&conv)
				if conv.Done {
					t.Fatal("baseline failure finished the dialogue")
				}
				waitRunStatus(t, db, run.ID, "waiting_human")
				if _, err := eng.ReactLiveAs("user:a", run.ID, "preview", models.LiveEvent{Op: models.LiveOpDiscard, SID: "page01"}); err != nil {
					t.Fatal(err)
				}
				if err := eng.waitReviewReadyForTest(run.ID, "preview", 5*time.Second); err != nil {
					t.Fatal(err)
				}
				waitLiveState(t, eng, run.ID, "page01", models.LiveStateDiscarded)
				p.muBaseline.Lock()
				defer p.muBaseline.Unlock()
				if p.prepares != 1 {
					t.Fatal("discard must not establish a new baseline")
				}
			})
		}
	}
}

func TestLiveBaselinePreparesSteerButNotCleanupOrRefine(t *testing.T) {
	p := &livePreparingProvider{fakeProvider: &fakeProvider{}, repoReady: true}
	e := &Engine{provider: p}
	if err := e.prepareLiveTurn(context.Background(), "run", "node", &reviewQueueItem{Live: &models.LiveRef{Op: models.LiveOpSteer}}); err != nil {
		t.Fatal(err)
	}
	if p.prepares != 1 {
		t.Fatal("first whole-page edit must freeze source baseline")
	}
	for _, op := range []string{models.LiveOpAccept, models.LiveOpRefine, models.LiveOpDiscard, models.LiveOpMountFailed} {
		if err := e.prepareLiveTurn(context.Background(), "run", "node", &reviewQueueItem{Live: &models.LiveRef{Op: op}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.prepareLiveTurn(context.Background(), "run", "node", &reviewQueueItem{Force: true}); err != nil {
		t.Fatal(err)
	}
	if p.prepares != 1 {
		t.Fatal("refine, cleanup and confirm must never replace the frozen baseline")
	}
}
