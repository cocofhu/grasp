package services

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// Usage the coding tool names keeps its model. Codex reports "default"; that
// is recorded under the turn's model instead of the unknown bucket.
func TestSoloLedgersUsageUnderTheTurnModel(t *testing.T) {
	for _, tc := range []struct{ name, key, want, source string }{
		{name: "named by the tool", key: "gpt-6.2-astra", want: "gpt-6.2-astra", source: models.TokenUsageSourceUpstream},
		// The fixture resolver runs every turn on fixture-model.
		{name: "unnamed usage", key: "default", want: "fixture-model", source: models.TokenUsageSourceBridge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acp, port := fakeACPServerFrames(t, map[string]any{
				"type": "prompt_done", "stopReason": "end_turn",
				"usage": map[string]any{tc.key: map[string]any{"inputTokens": 7, "outputTokens": 3}},
			})
			defer acp.Close()
			solo, _, _, db := newSoloFixture(t, port, 3)
			ctx := context.Background()
			v, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", Text: "hello"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = solo.Close(ctx, v.ID, "alice") })
			var entry models.TokenUsageEvent
			waitFor(t, "solo usage ledger", func() bool {
				return db.Where("thread_id = ?", v.ID).First(&entry).Error == nil
			})
			if entry.Source != models.TokenLedgerSourceSolo || entry.ModelKey != tc.want || entry.ModelSource != tc.source || entry.InputTokens != 7 || entry.OutputTokens != 3 {
				t.Fatalf("ledger = %+v, want model %q", entry, tc.want)
			}
			var msgs []models.SoloMessage
			waitFor(t, "solo reply", func() bool {
				msgs, _ = solo.Messages(v.ID, "alice")
				return len(msgs) == 2
			})
			if byModel := msgs[1].UsageByModel; len(byModel) != 1 || byModel[tc.want].InputTokens != 7 {
				t.Fatalf("reply usage = %+v, want model %q", byModel, tc.want)
			}
			var n int64
			db.Model(&models.TokenUsageEvent{}).Where("thread_id = ?", v.ID).Count(&n)
			if n != 1 {
				t.Fatalf("turn ledgered %d times", n)
			}
		})
	}
}
