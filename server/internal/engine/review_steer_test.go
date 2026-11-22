package engine

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatcmd"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func (f *fakeProvider) SteerSessionTurn(_ context.Context, _, _, _, text string, _ []models.PromptImage) (sandbox.SteerStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steered = append(f.steered, text)
	return f.steerStatus, nil
}

func (f *fakeProvider) reviseCount(nodeID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reviseCalls[nodeID]
}

func TestReviewQueueSteer(t *testing.T) {
	for _, tc := range []struct {
		status    sandbox.SteerStatus
		want      chatsession.SteerOutcome
		revisions int
	}{
		{sandbox.SteerDelivered, chatsession.SteerOutcomeJoined, 1},
		{sandbox.SteerUnsupported, chatsession.SteerOutcomeNext, 2},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			eng, db, provider := setupReviewEngine(t)
			hold := make(chan struct{})
			provider.reviseHold = hold
			provider.steerStatus = tc.status

			run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			waitReactPause(t, db, run.ID, "prop")
			waitRunStatus(t, db, run.ID, "waiting_human")
			if _, err := eng.SteerQueuedItem(context.Background(), run.ID, "prop", "x"); err == nil {
				t.Fatal("steer without a session")
			}
			if _, err := eng.EnqueueReviewTurnAs("", run.ID, "prop", "first", nil, nil, "node", ""); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, func() bool { _, thinking := eng.ReviewSessionState(run.ID, "prop"); return thinking })
			if _, err := eng.EnqueueReviewTurnAs("", run.ID, "prop", "second", nil, nil, "node", ""); err != nil {
				t.Fatal(err)
			}
			snap, _ := eng.ReviewSessionSnapshotFor(run.ID, "prop")
			if len(snap.Items) != 1 {
				t.Fatalf("pending = %+v", snap.Items)
			}
			id, _ := snap.Items[0]["id"].(string)
			if _, err := eng.SteerQueuedItem(context.Background(), run.ID, "prop", "missing"); err == nil {
				t.Fatal("unknown item")
			}
			out, err := eng.SteerQueuedItem(context.Background(), run.ID, "prop", id)
			if err != nil || out != tc.want {
				t.Fatalf("steer = %q %v", out, err)
			}
			if provider.steered[0] != "second" {
				t.Fatalf("steered = %v", provider.steered)
			}
			close(hold)
			waitUntil(t, func() bool { return provider.reviseCount("prop") == tc.revisions })
			if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
				t.Fatal(err)
			}
			if n := provider.reviseCount("prop"); n != tc.revisions {
				t.Fatalf("revisions = %d, want %d", n, tc.revisions)
			}
			if _, err := eng.SteerVisitorQueuedItem(context.Background(), run.ID, "prop", "guest", id); err == nil {
				t.Fatal("steer on a lane without a session")
			}
		})
	}
}

func TestOnlyPlainInputJoinsARunningTurn(t *testing.T) {
	for name, tc := range map[string]struct {
		item reviewQueueItem
		join bool
	}{
		"plain":             {reviewQueueItem{Text: "x"}, true},
		"comment-only user": {reviewQueueItem{Text: "x", LiveWritesDenied: true}, false},
		"command":           {reviewQueueItem{Text: "x", Command: chatcmd.Command{Name: "compact"}}, false},
		"live":              {reviewQueueItem{Text: "x", Live: &models.LiveRef{}}, false},
	} {
		if got := tc.item.joinsRunningTurn(); got != tc.join {
			t.Errorf("%s: joins = %v", name, got)
		}
	}
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
