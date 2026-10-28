package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

type replyMetadataProvider struct {
	*fakeProvider
	sequence int
}

func (p *replyMetadataProvider) metadata(turn runtime.ReactTurn) runtime.ReactTurn {
	p.sequence++
	turn.Execution = &models.ChatExecution{AgentName: "actual-agent", Backend: "opencode", ConfigurationID: fmt.Sprintf("group-%d", p.sequence), ModelID: "shared-raw-model", ReasoningEffort: "high", StartedAt: time.Now().UTC().Add(-time.Second)}
	turn.DurationMs = int64(1000 * p.sequence)
	turn.Usage = &models.TokenUsage{InputTokens: int64(100 * p.sequence), OutputTokens: int64(p.sequence)}
	turn.UsageByModel = models.TokenUsageByModel{"shared-raw-model": {InputTokens: turn.Usage.InputTokens, OutputTokens: turn.Usage.OutputTokens}}
	return turn
}

func (p *replyMetadataProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) runtime.ReactTurn {
	return p.metadata(p.fakeProvider.ReactReply(ctx, req, history, human, images, force))
}

func (p *replyMetadataProvider) ReviseInPlace(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) runtime.ReactTurn {
	return p.metadata(p.fakeProvider.ReviseInPlace(ctx, req, history, human, images))
}

func TestReactReplyPersistsIndividualMetadataWithoutVisitTotals(t *testing.T) {
	for _, mode := range []string{"clarify", "review"} {
		t.Run(mode, func(t *testing.T) {
			eng, db, fake := setupEngineGraphP(t, reactOnlyGraph())
			workflowID, nodeID := "wf", "clarify"
			if mode == "review" {
				eng, db, fake = setupReviewEngine(t)
				workflowID, nodeID = "review-wf", "prop"
			}
			fake.reactPending = 10
			eng.provider = &replyMetadataProvider{fakeProvider: fake}
			run, err := eng.StartRun(workflowID, map[string]any{"idea": "test"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			waitReactPause(t, db, run.ID, nodeID)
			for index := 0; index < 2; index++ {
				if err := eng.ReactReply(run.ID, nodeID, fmt.Sprintf("message-%d", index), nil, nil, false); err != nil {
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
				if message.Execution != nil {
					replies = append(replies, message)
				}
			}
			if len(replies) != 2 {
				t.Fatalf("per-reply metadata missing after DB reload: %+v", conv.Messages)
			}
			for index, reply := range replies {
				want := int64(index + 1)
				if reply.Execution.ConfigurationID != fmt.Sprintf("group-%d", want) || reply.Execution.ModelID != "shared-raw-model" || reply.Execution.ReasoningEffort != "high" || reply.Usage == nil || reply.Usage.InputTokens != 100*want || reply.UsageByModel["shared-raw-model"].OutputTokens != want || reply.DurationMs != 1000*want || reply.StartedAt == nil || reply.EndedAt == nil {
					t.Fatalf("reply borrowed another turn or visit metadata: %+v", reply)
				}
			}
			var state models.StateRun
			if err := db.Where("run_id = ? AND node_id = ?", run.ID, nodeID).First(&state).Error; err != nil {
				t.Fatal(err)
			}
			if state.Usage == nil || state.Usage.InputTokens != 300 || state.Usage.OutputTokens != 3 {
				t.Fatalf("visit ledger stopped accumulating per-call usage: %+v", state.Usage)
			}
		})
	}
}
