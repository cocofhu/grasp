package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
)

type replyIdentityProvider struct{ *fakeProvider }

func (p *replyIdentityProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) runtime.ReactTurn {
	turn := p.fakeProvider.ReactReply(ctx, req, history, human, images, force)
	turn.Handoffs = []string{"intermediate reply"}
	return turn
}

func (p *replyIdentityProvider) ReviseInPlace(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) runtime.ReactTurn {
	turn := p.fakeProvider.ReviseInPlace(ctx, req, history, human, images)
	turn.Handoffs = []string{"intermediate reply"}
	return turn
}

func TestChatIdentitySeparatesConcurrentSameTextRequestsAfterHistoryReload(t *testing.T) {
	for _, mode := range []string{"clarify", "review"} {
		t.Run(mode, func(t *testing.T) {
			eng, db, fake := setupEngineGraphP(t, reactOnlyGraph())
			workflowID, nodeID := "wf", "clarify"
			if mode == "review" {
				eng, db, fake = setupReviewEngine(t)
				workflowID, nodeID = "review-wf", "prop"
			}
			fake.reactPending = 10
			eng.provider = &replyIdentityProvider{fakeProvider: fake}
			run, err := eng.StartRun(workflowID, map[string]any{"idea": "test"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			waitReactPause(t, db, run.ID, nodeID)
			start := make(chan struct{})
			errs := make(chan error, 2)
			var sends sync.WaitGroup
			for _, id := range []string{"client-a", "client-b"} {
				sends.Add(1)
				go func() {
					defer sends.Done()
					<-start
					errs <- eng.reactReply("", run.ID, nodeID, "same request", nil, nil, false, false, false, id)
				}()
			}
			close(start)
			sends.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := eng.waitReviewReadyForTest(run.ID, nodeID, 5*time.Second); err != nil {
				t.Fatal(err)
			}

			// Run/history responses use these reloaded conversations and Turns().
			// Round-trip their JSON too, so an in-memory-only or non-JSON id fails.
			history := services.NewRunService(db).Conversations(run.ID)
			if len(history) != 1 {
				t.Fatalf("history conversations = %d", len(history))
			}
			encoded, err := json.Marshal(history[0].Turns())
			if err != nil {
				t.Fatal(err)
			}
			var turns []map[string]any
			if err := json.Unmarshal(encoded, &turns); err != nil {
				t.Fatal(err)
			}
			groups := map[string][]map[string]any{}
			for _, turn := range turns {
				id, _ := turn["clientMessageId"].(string)
				if id != "" {
					groups[id] = append(groups[id], turn)
				}
			}
			if len(groups) != 2 {
				t.Fatalf("same-text requests lost distinct identities: %s", encoded)
			}
			for _, id := range []string{"client-a", "client-b"} {
				group := groups[id]
				if len(group) != 3 || group[0]["role"] != "human" || group[0]["text"] != "same request" ||
					group[1]["role"] != "agent" || group[1]["handoff"] != true || group[2]["role"] != "agent" || group[2]["handoff"] == true {
					t.Fatalf("request %s did not retain its human, handoff and final reply: %s", id, encoded)
				}
			}
			if turns[0]["role"] == "agent" {
				if _, hasID := turns[0]["clientMessageId"]; hasID {
					t.Fatalf("opening historical message gained an unrelated request id: %s", encoded)
				}
			}
		})
	}
}

func TestChatIdentityRetryKeepsOriginalHumanGroupWithoutBackfillingLegacy(t *testing.T) {
	for _, id := range []string{"original-client-id", ""} {
		t.Run("id="+id, func(t *testing.T) {
			eng, db, fake := setupEngineGraphP(t, reactOnlyGraph())
			fake.reactPending = 10
			run, err := eng.StartRun("wf", nil, "test")
			if err != nil {
				t.Fatal(err)
			}
			waitReactPause(t, db, run.ID, "clarify")
			if _, err := eng.EnqueueClarifyTurnWithIDAs("", run.ID, "clarify", id, "retry me", nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			var conv models.ReactConversation
			if err := db.Where("run_id = ? AND node_id = ?", run.ID, "clarify").First(&conv).Error; err != nil {
				t.Fatal(err)
			}
			last := &conv.Messages[len(conv.Messages)-1]
			last.Text, last.Questions = "", nil
			if err := db.Save(&conv).Error; err != nil {
				t.Fatal(err)
			}
			if err := eng.ReactReplyRetryLast(run.ID, "clarify"); err != nil {
				t.Fatal(err)
			}
			if err := eng.waitReviewReadyForTest(run.ID, "clarify", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			convs := services.NewRunService(db).Conversations(run.ID)
			if len(convs) != 1 {
				t.Fatalf("history conversations = %d", len(convs))
			}
			turns := convs[0].Turns()
			if countHumanTurns(turns) != 1 || len(turns) < 2 {
				t.Fatalf("retry duplicated or lost its human: %+v", turns)
			}
			if turns[len(turns)-2].ClientMessageID != id || turns[len(turns)-1].ClientMessageID != id {
				t.Fatalf("retry reply moved to another human group: %+v", turns)
			}
			if id == "" {
				for _, turn := range turns {
					encoded, err := json.Marshal(turn)
					if err != nil {
						t.Fatal(err)
					}
					var wire map[string]any
					if err := json.Unmarshal(encoded, &wire); err != nil {
						t.Fatal(err)
					}
					if _, hasID := wire["clientMessageId"]; hasID {
						t.Fatalf("legacy request was backfilled: %s", encoded)
					}
				}
			}
		})
	}
}
