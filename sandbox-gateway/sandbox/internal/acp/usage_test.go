package acp

import (
	"reflect"
	"testing"

	"backend/internal/provider"
)

func TestPromptResultUsage(t *testing.T) {
	for _, test := range []struct {
		name   string
		result string
		want   map[string]provider.TokenUsage
	}{
		// agy 1.5.14 against a fake Gemini endpoint: two calls of 1000 prompt
		// (600 cached) / 20 candidates / 5 thoughts each.
		{"antigravity disjoint", `{"stopReason":"end_turn","usage":{"inputTokens":800,"outputTokens":50,"cachedReadTokens":1200,"cachedWriteTokens":0,"thoughtTokens":10,"totalTokens":2050}}`,
			map[string]provider.TokenUsage{"m": {InputTokens: 800, OutputTokens: 50, CacheReadTokens: 1200}}},
		{"antigravity cancelled", `{"stopReason":"cancelled","usage":{"inputTokens":400,"outputTokens":25,"cachedReadTokens":600,"cachedWriteTokens":0,"thoughtTokens":5,"totalTokens":1025}}`,
			map[string]provider.TokenUsage{"m": {InputTokens: 400, OutputTokens: 25, CacheReadTokens: 600}}},
		// Antigravity 1.4.5 (t3code #37686): input includes cache, output excludes thoughts.
		{"antigravity inclusive", `{"usage":{"inputTokens":42,"outputTokens":3,"thoughtTokens":9,"cachedReadTokens":24,"totalTokens":54}}`,
			map[string]provider.TokenUsage{"m": {InputTokens: 18, OutputTokens: 12, CacheReadTokens: 24}}},
		// grok 1.0.92 against a fake Responses endpoint: 1000 input (600 cached), 20 output.
		{"grok meta model usage", `{"stopReason":"end_turn","_meta":{"totalTokens":1020,"usage":{"inputTokens":1000,"outputTokens":20,"totalTokens":1020,"cachedReadTokens":600,"cacheCreationTokens":0,"reasoningTokens":5,"modelUsage":{"grok-4.6":{"inputTokens":1000,"outputTokens":20,"totalTokens":1020,"cachedReadTokens":600,"cacheCreationTokens":0,"reasoningTokens":5}}}}}`,
			map[string]provider.TokenUsage{"grok-4.6": {InputTokens: 400, OutputTokens: 20, CacheReadTokens: 600}}},
		{"meta without breakdown", `{"_meta":{"usage":{"inputTokens":10,"outputTokens":2}}}`,
			map[string]provider.TokenUsage{"m": {InputTokens: 10, OutputTokens: 2}}},
		{"thought separate without cache", `{"usage":{"inputTokens":10,"outputTokens":2,"thoughtTokens":3,"totalTokens":15}}`,
			map[string]provider.TokenUsage{"m": {InputTokens: 10, OutputTokens: 5}}},
		{"thought separate and disjoint cache", `{"usage":{"inputTokens":10,"outputTokens":2,"thoughtTokens":3,"cachedWriteTokens":4,"totalTokens":19}}`,
			map[string]provider.TokenUsage{"m": {InputTokens: 10, OutputTokens: 5, CacheWriteTokens: 4}}},
		{"no usage", `{"stopReason":"end_turn"}`, nil},
		{"cancelled grok", `{"stopReason":"cancelled","_meta":{"sessionId":"s"}}`, nil},
		{"zero usage", `{"usage":{"inputTokens":0,"outputTokens":0}}`, nil},
		{"not json", `nope`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := PromptResultUsage([]byte(test.result), "m"); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
}
