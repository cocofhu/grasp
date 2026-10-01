package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestCarriedUsageFoldsIntoNextTurn(t *testing.T) {
	c := &acpProvider{}
	key := "run|node"
	c.carryChatUsage(key, nil)
	c.carryChatUsage(key, &sandbox.ChatResult{
		Usage:        &models.TokenUsage{InputTokens: 40},
		UsageByModel: models.TokenUsageByModel{"m": {InputTokens: 40}},
	})
	c.carryChatUsage(key, &sandbox.ChatResult{Usage: &models.TokenUsage{OutputTokens: 5}})

	out := ReactTurn{Usage: &models.TokenUsage{InputTokens: 1}}
	c.drainCarriedUsage(key, &out)
	if out.Usage == nil || out.Usage.InputTokens != 41 || out.Usage.OutputTokens != 5 {
		t.Fatalf("usage=%+v", out.Usage)
	}
	if out.UsageByModel["m"].InputTokens != 40 {
		t.Fatalf("byModel=%+v", out.UsageByModel)
	}

	again := ReactTurn{}
	c.drainCarriedUsage(key, &again)
	if again.Usage != nil {
		t.Fatalf("carry must be consumed once, got %+v", again.Usage)
	}
}
