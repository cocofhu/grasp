package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

func autoNode(id, label string, caps *models.AgentCapabilities) models.Node {
	return models.Node{ID: id, Type: "agent", Label: label, Caps: caps}
}

func TestCertainAutoSuccessor(t *testing.T) {
	clarify := autoNode("clarify", "需求澄清", capsClarify)
	impl := autoNode("impl", "实现", capsImplement)
	other := autoNode("other", "另一个", capsPlain)
	gate := models.Node{ID: "gate", Type: "human_gate", Label: "审批"}
	out := models.Node{ID: "out", Type: "output", Label: "输出"}
	branch := models.Node{ID: "branch", Type: "branch", Label: "分支"}
	gated := autoNode("ask", "门禁代理", capsTest)

	cases := []struct {
		name string
		node *models.Node
		g    models.Graph
		want string
	}{
		{
			name: "single automatic agent",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, impl},
				Edges: []models.Edge{{ID: "e", Source: "clarify", Target: "impl"}},
			},
			want: "impl",
		},
		{
			name: "skip output plumbing",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, out, impl},
				Edges: []models.Edge{
					{ID: "e1", Source: "clarify", Target: "out"},
					{ID: "e2", Source: "out", Target: "impl"},
				},
			},
			want: "impl",
		},
		{
			name: "skip a guard already ruled out",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, other, impl},
				Edges: []models.Edge{
					{ID: "e1", Source: "clarify", Target: "other", When: "false"},
					{ID: "e2", Source: "clarify", Target: "impl"},
				},
			},
			want: "impl",
		},
		{
			name: "unknown guard is not unique",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, impl, gate},
				Edges: []models.Edge{
					{ID: "e1", Source: "clarify", Target: "impl", When: "vars.ready"},
					{ID: "e2", Source: "clarify", Target: "gate"},
				},
			},
		},
		{
			name: "human successor",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, gate},
				Edges: []models.Edge{{ID: "e", Source: "clarify", Target: "gate"}},
			},
		},
		{
			name: "branch is not settled",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, branch, impl},
				Edges: []models.Edge{{ID: "e", Source: "clarify", Target: "branch"}},
			},
		},
		{
			name: "rollback is not settled",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, impl},
				Edges: []models.Edge{{ID: "e", Source: "clarify", Target: "impl", Kind: models.EdgeRollback}},
			},
		},
		{
			name: "verdict outlet is not known yet",
			node: &gated,
			g: models.Graph{
				Nodes: []models.Node{gated, impl},
				Edges: []models.Edge{{ID: "e", Source: "ask", Target: "impl", SourceHandle: "pass"}},
			},
		},
		{
			name: "named outlet is not the plain confirm outlet",
			node: &clarify,
			g: models.Graph{
				Nodes: []models.Node{clarify, impl},
				Edges: []models.Edge{{ID: "e", Source: "clarify", Target: "impl", SourceHandle: "pass"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := certainAutoSuccessor(tc.g, tc.node); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func clarifyImplementGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "clarify", Type: "agent", Label: "需求澄清", Caps: capsClarify, Config: map[string]any{"agent_profile": "pm", "prompt": "澄清"}},
			{ID: "impl", Type: "agent", Label: "实现", Caps: capsImplement, Config: map[string]any{"agent_profile": "dev", "prompt": "实现"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "clarify"},
			{ID: "e2", Source: "clarify", Target: "impl"},
			{ID: "e3", Source: "impl", Target: "output"},
		},
	}
}

// Confirm writes the successor as soon as the dialogue is marked done, while
// the wrap-up reply is still in flight and rememberHandoffTarget has not run.
func TestConfirmNotesPendingAutoBeforeReplyReturns(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, clarifyImplementGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	hold := make(chan struct{})
	p.reactHold = hold
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		close(hold)
	}
	defer release()
	done := make(chan error, 1)
	go func() {
		done <- eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, false)
	}()

	deadline := time.Now().Add(5 * time.Second)
	var stored models.Run
	for {
		if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		cp := stored.Checkpoints[services.PendingAutoCheckpoint]
		node, _ := cp["node"].(string)
		if node == "impl" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending auto not recorded before the reply returned: %#v", stored.Checkpoints)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if from, ok := eng.admissionFromNode(stored); ok && from == "impl" {
		t.Fatal("successor must be listed before rememberHandoffTarget writes the admission cursor")
	}
	cn := services.NewRunService(db).CurrentNodes([]models.Run{stored})[run.ID]
	if cn.ID != "impl" || cn.Name != "实现" || cn.Kind != services.CurrentKindAgent || cn.Mode != models.NodeModeAuto || cn.State != services.CurrentStateEntering {
		t.Fatalf("list current node: %+v", cn)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv).Error; err != nil || !conv.Done {
		t.Fatalf("dialogue should already be done: err=%v done=%v", err, conv.Done)
	}

	release()
	if err := <-done; err != nil {
		t.Fatalf("confirm: %v", err)
	}
}

func TestConfirmClearsPendingAutoWhenReplyFails(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, clarifyImplementGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	p.reactChatErr = errors.New("chat down")
	if err := eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, false); err == nil {
		t.Fatal("expected chat error")
	}
	var stored models.Run
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := stored.Checkpoints[services.PendingAutoCheckpoint]; ok {
		t.Fatalf("failed confirm must clear the mark: %#v", stored.Checkpoints[services.PendingAutoCheckpoint])
	}
	cn := services.NewRunService(db).CurrentNodes([]models.Run{stored})[run.ID]
	if cn.ID != "clarify" || cn.Kind != services.CurrentKindClarify || cn.Mode != models.NodeModeManual || cn.State != "waiting_human" {
		t.Fatalf("clarify must be current again: %+v", cn)
	}
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv).Error; err != nil || conv.Done {
		t.Fatalf("dialogue must be open again: err=%v done=%v", err, conv.Done)
	}
}

type confirmBarrierProvider struct {
	*fakeProvider
	entered chan struct{}
}

func (p *confirmBarrierProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) runtime.ReactTurn {
	close(p.entered)
	return p.fakeProvider.ReactReply(ctx, req, history, human, images, force)
}

func TestConfirmDoesNotMarkAnUnsettledSuccessor(t *testing.T) {
	g := clarifyImplementGraph()
	g.Nodes = append(g.Nodes, models.Node{ID: "gate", Type: "human_gate", Label: "审批"})
	g.Edges = []models.Edge{
		{ID: "e1", Source: "input", Target: "clarify"},
		{ID: "e2", Source: "clarify", Target: "impl", When: "vars.ready"},
		{ID: "e3", Source: "clarify", Target: "gate"},
	}
	for _, beforePause := range []bool{false, true} {
		name := "after run pause"
		if beforePause {
			name = "before run pause"
		}
		t.Run(name, func(t *testing.T) {
			eng, db, p := setupEngineGraphP(t, g)
			openingPaused, releaseOpening := make(chan struct{}), make(chan struct{})
			replyEntered, releaseReply := make(chan struct{}), make(chan struct{})
			var openingOnce, releaseOpeningOnce, releaseReplyOnce sync.Once
			unblockOpening := func() { releaseOpeningOnce.Do(func() { close(releaseOpening) }) }
			unblockReply := func() { releaseReplyOnce.Do(func() { close(releaseReply) }) }
			var done chan error
			var runID string
			defer func() {
				unblockOpening()
				unblockReply()
				if done != nil {
					select {
					case err := <-done:
						if err == nil {
							waitGatePending(t, db, runID, "gate")
						}
					case <-time.After(waitPollTimeout):
						t.Error("confirm did not finish during cleanup")
					}
				}
				waitDriversIdle(t, eng)
			}()
			await := func(ch <-chan struct{}, phase string) {
				t.Helper()
				select {
				case <-ch:
				case <-time.After(waitPollTimeout):
					t.Fatalf("timed out waiting for %s", phase)
				}
			}

			// Park after the node pause commits, before the opening driver
			// checks Done and decides whether to pause the run itself.
			if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:park-clarify-pause", func(tx *gorm.DB) {
				if sr, ok := tx.Statement.Dest.(*models.StateRun); ok && sr.NodeID == "clarify" && sr.Status == "waiting_human" {
					openingOnce.Do(func() {
						close(openingPaused)
						<-releaseOpening
					})
				}
			}); err != nil {
				t.Fatal(err)
			}
			// Configure the reply barrier before any provider calls begin.
			p.reactHold = releaseReply
			eng.provider = &confirmBarrierProvider{fakeProvider: p, entered: replyEntered}
			run, err := eng.StartRun("wf", nil, "test")
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			runID = run.ID
			await(openingPaused, "opening node pause")
			if !beforePause {
				unblockOpening()
				waitDriversIdle(t, eng)
			}
			done = make(chan error, 1)
			go func() {
				done <- eng.ReactConfirmAs("", run.ID, "clarify", "确认并流转", nil, nil, false)
			}()
			await(replyEntered, "confirm reply")
			// The early Done write now makes pauseStillPending false in the
			// before-pause case, leaving the run running during wrap-up.
			unblockOpening()
			waitDriversIdle(t, eng)
			var stored models.Run
			if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus := "waiting_human"
			if beforePause {
				wantStatus = "running"
			}
			if stored.Status != wantStatus {
				t.Fatalf("run status = %q, want %q", stored.Status, wantStatus)
			}
			if _, ok := stored.Checkpoints[services.PendingAutoCheckpoint]; ok {
				t.Fatalf("unsettled successor must not be marked: %#v", stored.Checkpoints[services.PendingAutoCheckpoint])
			}
			var conv models.ReactConversation
			if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv).Error; err != nil || !conv.Done {
				t.Fatalf("dialogue must be done during wrap-up: err=%v done=%v", err, conv.Done)
			}
			cn := services.NewRunService(db).CurrentNodes([]models.Run{stored})[run.ID]
			if cn.ID != "clarify" || cn.Kind != services.CurrentKindClarify || cn.Mode != models.NodeModeManual || cn.State != "waiting_human" {
				t.Fatalf("clarify stays current: %+v", cn)
			}
			unblockReply()
			select {
			case err := <-done:
				done = nil
				if err != nil {
					t.Fatalf("confirm: %v", err)
				}
			case <-time.After(waitPollTimeout):
				t.Fatal("confirm did not finish after releasing reply")
			}
			waitGatePending(t, db, run.ID, "gate")
		})
	}
}
