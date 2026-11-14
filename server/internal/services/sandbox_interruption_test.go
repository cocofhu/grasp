package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSandboxChatReportsPartialDisconnect(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial reply then EOF", true: "prompt_done then EOF"}[complete], func(t *testing.T) {
			frames := providerTurnFrames("end_turn", "", "retained reply")
			if !complete {
				frames = frames[:len(frames)-1]
			}
			acp, port := fakeACPServerStream(t, nil, true, frames...)
			defer acp.Close()
			db := newTestDB(t)
			s := newSandboxService(t, db, &dockerState{acpPort: port})
			ctx := context.Background()
			row, err := s.Open(ctx, "agentA", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Destroy(ctx, row.ID) })
			if err := s.WaitReady(ctx, row.ID, nil); err != nil {
				t.Fatal(err)
			}
			var streamed strings.Builder
			_, _, err = s.ChatWithTimeout(ctx, row.ID, "hello", nil, 0, func(raw json.RawMessage) {
				streamed.Write(raw)
			})
			if complete && err != nil {
				t.Fatalf("completed reply incorrectly failed: %v", err)
			}
			if !complete && (!errors.Is(err, sandbox.ErrConnClosed) || !strings.Contains(err.Error(), "本轮未完成")) {
				t.Fatalf("partial disconnect reported success: %v", err)
			}
			if !strings.Contains(streamed.String(), "retained reply") {
				t.Fatal("partial output was lost")
			}
		})
	}
}

func TestSoloPersistsPartialDisconnectAsFailed(t *testing.T) {
	frames := providerTurnFrames("end_turn", "", "retained partial reply")
	acp, port := fakeACPServerStream(t, nil, true, frames[:len(frames)-1]...)
	defer acp.Close()
	solo, _, _, _ := newSoloFixture(t, port, 3)
	ctx := context.Background()
	v, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = solo.Close(ctx, v.ID, "alice") })
	var messages []models.SoloMessage
	waitFor(t, "partial disconnect persisted as failed", func() bool {
		messages, _ = solo.Messages(v.ID, "alice")
		return len(messages) == 2 && messages[0].Status == "failed" && messages[1].Status == "failed"
	})
	reply := messages[1]
	if reply.Content != "retained partial reply" || !strings.Contains(reply.Error, "本轮未完成") || len(reply.Events) == 0 {
		t.Fatalf("partial reply or interruption reason lost: %+v", reply)
	}
}
