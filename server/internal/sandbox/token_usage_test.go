package sandbox

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestParsePromptDoneUsage(t *testing.T) {
	t.Parallel()
	if u, m := parsePromptDoneUsage(nil, ""); u != nil || m != nil {
		t.Fatal("nil raw should be nil")
	}
	if u, m := parsePromptDoneUsage(json.RawMessage(`null`), ""); u != nil || m != nil {
		t.Fatal("null should be nil")
	}
	empty, emptyM := parsePromptDoneUsage(json.RawMessage(`{}`), "")
	if empty == nil || emptyM == nil {
		t.Fatal("empty object should be present (reported 0)")
	}
	if empty.Total() != 0 || len(emptyM) != 0 {
		t.Fatalf("empty total = %d byModel=%+v", empty.Total(), emptyM)
	}

	raw := json.RawMessage(`{
		"model-a": {"inputTokens":10,"outputTokens":5,"cacheReadTokens":2,"cacheWriteTokens":1},
		"model-b": {"inputTokens":3,"outputTokens":7,"cacheReadTokens":0,"cacheWriteTokens":4}
	}`)
	u, by := parsePromptDoneUsage(raw, "")
	if u == nil {
		t.Fatal("expected usage")
	}
	if u.InputTokens != 13 || u.OutputTokens != 12 || u.CacheReadTokens != 2 || u.CacheWriteTokens != 5 {
		t.Fatalf("sum = %+v", u)
	}
	if u.Total() != 32 {
		t.Fatalf("total = %d", u.Total())
	}
	if by["model-a"].InputTokens != 10 || by["model-b"].OutputTokens != 7 {
		t.Fatalf("byModel = %+v", by)
	}
	if by["model-a"].Source != models.TokenUsageSourceUpstream || by["model-a"].Filled {
		t.Fatalf("real key source = %+v", by["model-a"])
	}
}

func TestParsePromptDoneUsageWeakKeyBackfill(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"default": {"inputTokens":10,"outputTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0},
		"unknown": {"inputTokens":5,"outputTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0},
		"": {"inputTokens":2,"outputTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0},
		"gpt-5.2": {"inputTokens":7,"outputTokens":1,"cacheReadTokens":0,"cacheWriteTokens":0}
	}`)
	u, by := parsePromptDoneUsage(raw, "claude-sonnet-4")
	if u == nil || u.Total() != 25 {
		t.Fatalf("total = %+v", u)
	}
	bridge := by["claude-sonnet-4"]
	if bridge.InputTokens != 17 || !bridge.Filled || bridge.Source != models.TokenUsageSourceBridge {
		t.Fatalf("bridge bucket = %+v", bridge)
	}
	real := by["gpt-5.2"]
	if real.InputTokens != 7 || real.Filled || real.Source != models.TokenUsageSourceUpstream {
		t.Fatalf("real bucket = %+v", real)
	}
	if _, ok := by[models.TokenUsageModelUnknown]; ok {
		t.Fatal("weak keys must not land in unknown when bridge set")
	}
}

func TestParsePromptDoneUsageWeakKeyNoBridge(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"default":{"inputTokens":4},"claude-sonnet-4":{"inputTokens":6}}`)
	u, by := parsePromptDoneUsage(raw, "")
	if u == nil || u.InputTokens != 10 {
		t.Fatalf("sum = %+v", u)
	}
	unk := by[models.TokenUsageModelUnknown]
	if unk.InputTokens != 4 || unk.Source != models.TokenUsageSourceUnknown || unk.Filled {
		t.Fatalf("unknown = %+v", unk)
	}
	if by["claude-sonnet-4"].InputTokens != 6 {
		t.Fatalf("real = %+v", by["claude-sonnet-4"])
	}
}

func TestParsePromptDoneUsageBridgeMergesWithRealKey(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{
		"default": {"inputTokens":3},
		"claude-sonnet-4": {"inputTokens":10}
	}`)
	_, by := parsePromptDoneUsage(raw, "claude-sonnet-4")
	got := by["claude-sonnet-4"]
	if got.InputTokens != 13 || !got.Filled || got.Source != models.TokenUsageSourceBridge {
		t.Fatalf("merged = %+v", got)
	}
}

func TestDispatchEventDataPromptDoneUsage(t *testing.T) {
	t.Parallel()
	c := NewACPClient("127.0.0.1", 1).WithBridgeModel("bridge-m")
	r := &ChatResult{}
	frame := json.RawMessage(`{"op":"event","data":{"type":"prompt_done","usage":{"default":{"inputTokens":1,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":4}}}}`)
	if !c.dispatchEventData(frame, r) {
		t.Fatal("prompt_done should signal done")
	}
	if r.Usage == nil || r.Usage.Total() != 10 {
		t.Fatalf("usage = %+v", r.Usage)
	}
	b := r.UsageByModel["bridge-m"]
	if b.Total() != 10 || !b.Filled {
		t.Fatalf("byModel = %+v", r.UsageByModel)
	}

	// No usage field → stay nil (not 0).
	r2 := &ChatResult{}
	if !c.dispatchEventData(json.RawMessage(`{"op":"event","data":{"type":"prompt_done"}}`), r2) {
		t.Fatal("prompt_done without usage should still finish")
	}
	if r2.Usage != nil || r2.UsageByModel != nil {
		t.Fatalf("missing usage must stay nil, got %+v / %+v", r2.Usage, r2.UsageByModel)
	}
}

func TestAddTokenUsageAcrossTurns(t *testing.T) {
	t.Parallel()
	var acc *models.TokenUsage
	acc = models.AddTokenUsage(acc, &models.TokenUsage{InputTokens: 10, OutputTokens: 1})
	acc = models.AddTokenUsage(acc, &models.TokenUsage{InputTokens: 5, CacheReadTokens: 2})
	if acc.InputTokens != 15 || acc.OutputTokens != 1 || acc.CacheReadTokens != 2 {
		t.Fatalf("acc = %+v", acc)
	}
	// Explicit zero still establishes presence when starting from nil.
	z := models.AddTokenUsage(nil, &models.TokenUsage{})
	if z == nil || z.Total() != 0 {
		t.Fatalf("zero report = %+v", z)
	}
}

func TestUsageModelFollowsTheTurnExecution(t *testing.T) {
	t.Parallel()
	c := NewACPClient("127.0.0.1", 1).WithBridgeModel("launch-model")
	if got := c.usageModel(); got != "launch-model" {
		t.Fatalf("without execution = %q", got)
	}
	c.WithExecution(&models.ChatExecution{ModelID: "routed-model"})
	if got := c.usageModel(); got != "routed-model" {
		t.Fatalf("with execution = %q", got)
	}
	c.WithExecution(&models.ChatExecution{ModelID: " "})
	if got := c.usageModel(); got != "launch-model" {
		t.Fatalf("blank execution model = %q", got)
	}
}
