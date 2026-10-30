package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// The sandbox snapshot keeps prior turns unbracketed, followed by this reply's
// prompt brackets. Each new reply receives the complete historical event log.
type replyToolSnapshotProvider struct {
	*fakeProvider
	mu        sync.Mutex
	sequence  int
	history   []models.AcpEvent
	snapshots [][]models.AcpEvent
}

func (p *replyToolSnapshotProvider) snapshot(turn runtime.ReactTurn) runtime.ReactTurn {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sequence++
	turn.Msg = fmt.Sprintf("reply %d", p.sequence)
	var current []models.AcpEvent
	parts := []models.AcpPart{{Kind: "message", Text: turn.Msg}}
	if p.sequence < 3 {
		title := fmt.Sprintf("tool-%d", p.sequence)
		current = append(current, models.AcpEvent{Kind: "tool_call", Title: title, Status: "completed"})
		parts = append([]models.AcpPart{{Kind: "tool", Title: title, Status: "completed"}}, parts...)
	}
	current = append(current, models.AcpEvent{Kind: "message", Text: turn.Msg}, models.AcpEvent{Kind: models.AcpKindTimeline, Parts: parts})
	turn.Events = append(append([]models.AcpEvent(nil), p.history...), models.AcpEvent{Kind: models.AcpKindPrompt, Text: "current request"})
	turn.Events = append(turn.Events, current...)
	turn.Events = append(turn.Events, models.AcpEvent{Kind: models.AcpKindTurnEnd})
	p.history = append(p.history, current...)
	p.snapshots = append(p.snapshots, turn.Events)
	return turn
}

func (p *replyToolSnapshotProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) runtime.ReactTurn {
	return p.snapshot(p.fakeProvider.ReactReply(ctx, req, history, human, images, force))
}

func (p *replyToolSnapshotProvider) ReviseInPlace(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) runtime.ReactTurn {
	return p.snapshot(p.fakeProvider.ReviseInPlace(ctx, req, history, human, images))
}

func TestChatToolsStayOnIndividualRepliesAfterHistoryReload(t *testing.T) {
	for _, mode := range []string{"clarify", "review", "gate revise"} {
		t.Run(mode, func(t *testing.T) {
			eng, db, fake := setupEngineGraphP(t, reactOnlyGraph())
			workflowID, nodeID := "wf", "clarify"
			if mode == "review" {
				eng, db, fake = setupReviewEngine(t)
				workflowID, nodeID = "review-wf", "prop"
			} else if mode == "gate revise" {
				eng, db, fake = setupGateReactEngine(t)
				workflowID, nodeID = "gate-react-wf", "prop"
			}
			fake.reactPending = 10
			provider := &replyToolSnapshotProvider{fakeProvider: fake}
			eng.provider = provider
			run, err := eng.StartRun(workflowID, map[string]any{"idea": "test"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "gate revise" {
				waitGatePending(t, db, run.ID, "select")
			} else {
				waitReactPause(t, db, run.ID, nodeID)
			}
			waitRunStatus(t, db, run.ID, "waiting_human")
			for i := 0; i < 3; i++ {
				if mode == "gate revise" {
					err = eng.GateReactRevise(run.ID, "select", "same request", nil, nil)
				} else {
					err = eng.ReactReply(run.ID, nodeID, "same request", nil, nil, false)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := eng.waitReviewReadyForTest(run.ID, nodeID, 5*time.Second); err != nil {
					t.Fatal(err)
				}
			}
			var conv models.ReactConversation
			if err := db.Where("run_id = ? AND node_id = ?", run.ID, nodeID).First(&conv).Error; err != nil {
				t.Fatal(err)
			}
			var replies []models.ReactMessage
			for _, message := range conv.Messages {
				if message.Role == "agent" && strings.HasPrefix(message.Text, "reply ") {
					replies = append(replies, message)
				}
			}
			if len(replies) != 3 {
				t.Fatalf("reply history = %+v", conv.Messages)
			}
			for i, reply := range replies {
				if i < 2 {
					if len(reply.Tools) != 1 || reply.Tools[0] != (models.ReactTool{Title: fmt.Sprintf("tool-%d", i+1), Status: "completed"}) {
						t.Fatalf("reply %d borrowed historical tools: %+v", i+1, reply.Tools)
					}
					if len(reply.Parts) != 2 || reply.Parts[0].Title != reply.Tools[0].Title || reply.Parts[1].Text != reply.Text {
						t.Fatalf("reply %d borrowed historical steps: %+v", i+1, reply.Parts)
					}
				} else if len(reply.Tools) != 0 {
					t.Fatalf("tool-free reply borrowed historical tools: %+v", reply.Tools)
				} else if len(reply.Parts) != 1 || reply.Parts[0].Kind != "message" || reply.Parts[0].Text != reply.Text {
					t.Fatalf("tool-free reply borrowed historical steps: %+v", reply.Parts)
				}
				if mode == "gate revise" && reply.GateNodeID != "select" {
					t.Fatalf("gate attribution lost: %+v", reply)
				}
			}
			provider.mu.Lock()
			defer provider.mu.Unlock()
			var historicalTools int
			for _, event := range provider.snapshots[2] {
				if event.Kind == "tool_call" {
					historicalTools++
				}
			}
			if historicalTools != 2 {
				t.Fatalf("per-reply projection removed the complete event history: %+v", provider.snapshots[2])
			}
		})
	}
}
