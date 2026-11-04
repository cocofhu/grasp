package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/models"
)

func TestPreviewReplyKeepsRecordedStatisticsWithoutPrivateConfiguration(t *testing.T) {
	usage := &models.TokenUsage{InputTokens: 123, OutputTokens: 45}
	execution := &models.ChatExecution{AgentName: "Recorded Agent", Backend: "opencode", ModelID: "recorded-model",
		ConfigurationID: "private-configuration", ReasoningEffort: "high", StartedAt: time.Now().Add(-34 * time.Second)}
	row := previewSoloRow(models.SoloMessage{ID: "reply", Role: "assistant", Content: "Reply", CreatedAt: time.Now(),
		Execution: execution, Usage: usage, DurationMs: 34000,
		UsageByModel: models.TokenUsageByModel{"recorded-model": {InputTokens: 123, OutputTokens: 45, Source: models.TokenUsageSourceUpstream}}})
	wire, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Recorded Agent", "opencode", "recorded-model", `"inputTokens":123`, `"durationMs":34000`} {
		if !strings.Contains(string(wire), value) {
			t.Fatalf("missing recorded statistic %s: %s", value, wire)
		}
	}
	if strings.Contains(string(wire), "private-configuration") || strings.Contains(string(wire), "configurationId") {
		t.Fatalf("private execution field leaked: %s", wire)
	}
	legacy := previewSoloRow(models.SoloMessage{ID: "legacy", Role: "assistant", Content: "Old reply"})
	if legacy.Execution != nil || legacy.Usage != nil || legacy.UsageByModel != nil {
		t.Fatal("legacy reply acquired fabricated metadata")
	}
	zero := previewSoloRow(models.SoloMessage{Role: "assistant", Usage: &models.TokenUsage{}})
	if zero.Usage == nil {
		t.Fatal("explicitly reported zero usage became unreported")
	}
}

func TestPreviewWorkflowQueueFollowsServerPositionAndRedactsAttachments(t *testing.T) {
	first, second := 0, 1
	state := engine.DialogueState{Turns: []engine.DialogueTurn{
		{TurnID: "running", Phase: "running", Item: map[string]any{"text": "Not queued"}},
		{TurnID: "second", Phase: "queued", Position: &second, Item: map[string]any{"text": "Second"}},
		{TurnID: "first", Phase: "queued", Position: &first, Item: map[string]any{"text": "First",
			"images":      []models.PromptImage{{MimeType: "image/png", Ref: "blob:private", Data: "private-data", Name: "private-name"}},
			"annotations": []models.ReactAnnotation{{Selector: "#heading", URL: "private-url", OuterHTML: "private-html"}}}},
	}}
	queue := previewWorkflowQueue(engine.PublicDialogue(state))
	if len(queue) != 2 || queue[0].ID != "first" || queue[1].ID != "second" || len(queue[0].Images) != 1 || len(queue[0].Annotations) != 1 {
		t.Fatalf("queue: %+v", queue)
	}
	wire, err := json.Marshal(queue)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "private-") || strings.Contains(string(wire), "blob:") || strings.Contains(string(wire), "Not queued") {
		t.Fatalf("queue leaked private data: %s", wire)
	}
	solo := previewPending(models.SoloMessage{ID: "queued", Content: "Queued", Images: []models.PromptImage{{MimeType: "image/png", Ref: "blob:private", Name: "private-name"}}})
	if len(solo.Images) != 1 || solo.Images[0].Ref != "" || solo.Images[0].Name != "" {
		t.Fatal("Solo queue exposed attachment addresses or names")
	}
}

func TestPreviewQueueOnlyTailCanReorderClearAndSkipUnchangedQueue(t *testing.T) {
	seed, err := newPreviewMainSnapshot(&previewMainHistory{Rows: []sharedPreviewRow{}, Queue: []previewQueuedItem{{ID: "a", Text: "First"}, {ID: "b", Text: "Second"}}})
	if err != nil {
		t.Fatal(err)
	}
	next, err := newPreviewMainSnapshot(&previewMainHistory{Rows: []sharedPreviewRow{}, Queue: []previewQueuedItem{{ID: "b", Text: "Second"}, {ID: "a", Text: "First"}}})
	if err != nil {
		t.Fatal(err)
	}
	frame := next.frameAfter(seed)
	if frame["start"] != 0 || len(frame["rows"].([]json.RawMessage)) != 0 || frame["queue"].([]previewQueuedItem)[0].ID != "b" {
		t.Fatalf("queue-only update: %+v", frame)
	}
	if _, present := next.frameAfter(next)["queue"]; present {
		t.Fatal("unchanged queue was retransmitted")
	}
	empty, err := newPreviewMainSnapshot(&previewMainHistory{Rows: []sharedPreviewRow{}, Queue: []previewQueuedItem{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.frameAfter(next)["queue"].([]previewQueuedItem)) != 0 {
		t.Fatal("empty queue was not sent explicitly")
	}
}
