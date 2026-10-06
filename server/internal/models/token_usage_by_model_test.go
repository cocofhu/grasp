package models

import "testing"

func TestIsWeakModelKey(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"", " ", "default", "DEFAULT", "unknown", "Unknown"} {
		if !IsWeakModelKey(k) {
			t.Fatalf("expected weak: %q", k)
		}
	}
	if IsWeakModelKey("claude-sonnet-4") {
		t.Fatal("real modelID must not be weak")
	}
}

func TestAddTokenUsageByModelMergeFilled(t *testing.T) {
	t.Parallel()
	a := TokenUsageByModel{
		"claude-sonnet-4": {InputTokens: 10, Source: TokenUsageSourceUpstream},
	}
	b := TokenUsageByModel{
		"claude-sonnet-4": {InputTokens: 5, Source: TokenUsageSourceBridge, Filled: true},
	}
	out := AddTokenUsageByModel(a, b)
	got := out["claude-sonnet-4"]
	if got.InputTokens != 15 || !got.Filled || got.Source != TokenUsageSourceBridge {
		t.Fatalf("merged = %+v", got)
	}
}

func TestSumTokenUsageByModel(t *testing.T) {
	t.Parallel()
	if sumTokenUsageByModel(nil) != nil {
		t.Fatal("nil")
	}
	s := sumTokenUsageByModel(TokenUsageByModel{
		"a": {InputTokens: 1, OutputTokens: 2},
		"b": {CacheReadTokens: 3, CacheWriteTokens: 4},
	})
	if s == nil || s.Total() != 10 {
		t.Fatalf("sum = %+v", s)
	}
}
