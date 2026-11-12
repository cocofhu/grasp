package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

// askChat raises a question and a form the way the ask_question / ask_form
// tools do while the turn runs.
type askChat struct {
	host  *mcp.Host
	runID string
}

func (c *askChat) ChatWithTimeout(_ context.Context, _ uint, _ string, _ []models.PromptImage, _ time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.host.SetPendingQuestions(c.runID, "solo", []models.ReactQuestion{{ID: "q1", Prompt: "Which database?", Options: []models.ReactOption{{ID: "pg", Label: "Postgres", Recommended: true}, {ID: "my", Label: "MySQL"}}}})
	c.host.SetPendingForms(c.runID, "solo", []models.ReactForm{{Fields: []models.ReactFormField{{Name: "url", Label: "URL"}}}})
	onEvent(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Pick one"}}`))
	return nil, nil, nil
}

func (*askChat) Cancel(uint) {}

func TestSoloReplyKeepsQuestionsRaisedThisTurn(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, _ := newSoloFixture(t, port, 3)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = solo.Close(context.Background(), view.ID, "alice") }()
	var runID string
	waitFor(t, "running Solo sandbox", func() bool {
		v, err := solo.Get(context.Background(), view.ID, "alice")
		if err != nil {
			return false
		}
		runID = v.ArtifactRunID
		return v.Status == models.SoloStatusRunning && runID != ""
	})
	// Left over from an earlier turn that never finished; must not resurface.
	sbx.host.SetPendingQuestions(runID, "solo", []models.ReactQuestion{{ID: "stale", Prompt: "Stale?"}})
	solo.SetChatterForTest(&askChat{host: sbx.host, runID: runID})
	solo.SetModelAlignerForTest(&recordAligner{})
	if _, err := solo.Send(view.ID, "alice", "set up storage", nil); err != nil {
		t.Fatal(err)
	}
	var reply models.SoloMessage
	waitFor(t, "persisted reply", func() bool {
		messages, _ := solo.Messages(view.ID, "alice")
		for _, m := range messages {
			if m.Role == "assistant" {
				reply = m
				return true
			}
		}
		return false
	})
	if len(reply.Questions) != 1 || reply.Questions[0].ID != "q1" || !reply.Questions[0].Options[0].Recommended {
		t.Fatalf("questions = %+v", reply.Questions)
	}
	if len(reply.Forms) != 1 || reply.Forms[0].Fields[0].Name != "url" {
		t.Fatalf("forms = %+v", reply.Forms)
	}
	if left := sbx.host.TakePendingQuestions(runID, "solo"); len(left) != 0 {
		t.Fatalf("pending questions left after the turn: %+v", left)
	}
}
