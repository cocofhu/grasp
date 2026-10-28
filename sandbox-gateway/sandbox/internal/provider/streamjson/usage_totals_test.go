package streamjson

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/internal/provider"
)

func TestTerminalUsageTotalsReplaceStreamedSamples(t *testing.T) {
	const primary = `{"type":"assistant","message":{"model":"gpt-5","content":[],"usage":{"input_tokens":128,"output_tokens":32}}}`
	const helper = `{"type":"assistant","message":{"model":"helper","content":[],"usage":{"input_tokens":64,"output_tokens":16}}}`
	const qualifiedTotal = `{"type":"result","subtype":"success","session_id":"S1","usage":{"input_tokens":256,"output_tokens":64},"modelUsage":{"custom-local:gpt-5":{"inputTokens":256,"outputTokens":64}}}`
	for _, test := range []struct {
		name    string
		lines   []string
		want    map[string]provider.TokenUsage
		stop    string
		trailer string
	}{
		{"qualified native model alias", []string{primary, primary, qualifiedTotal}, map[string]provider.TokenUsage{"custom-local:gpt-5": {InputTokens: 256, OutputTokens: 64}}, "end_turn", ""},
		{"multiple model totals", []string{primary, helper, `{"type":"result","subtype":"success","modelUsage":{"route:gpt-5":{"inputTokens":128,"outputTokens":32,"cacheReadInputTokens":8,"cacheCreationInputTokens":4},"route:helper":{"inputTokens":64,"outputTokens":16,"cacheReadInputTokens":2,"cacheCreationInputTokens":1}}}`}, map[string]provider.TokenUsage{"route:gpt-5": {InputTokens: 128, OutputTokens: 32, CacheReadTokens: 8, CacheWriteTokens: 4}, "route:helper": {InputTokens: 64, OutputTokens: 16, CacheReadTokens: 2, CacheWriteTokens: 1}}, "end_turn", ""},
		{"flat native total", []string{primary, primary, `{"type":"result","subtype":"success","model":"gpt-5","usage":{"input_tokens":256,"output_tokens":64}}`}, map[string]provider.TokenUsage{"gpt-5": {InputTokens: 256, OutputTokens: 64}}, "end_turn", ""},
		{"missing terminal total keeps partial models", []string{primary, helper, `{"type":"result","subtype":"success"}`}, map[string]provider.TokenUsage{"gpt-5": {InputTokens: 128, OutputTokens: 32}, "helper": {InputTokens: 64, OutputTokens: 16}}, "end_turn", ""},
		{"empty terminal total keeps partial models", []string{primary, helper, `{"type":"result","subtype":"success","usage":{},"modelUsage":{"route:gpt-5":{"inputTokens":0,"outputTokens":0}}}`}, map[string]provider.TokenUsage{"gpt-5": {InputTokens: 128, OutputTokens: 32}, "helper": {InputTokens: 64, OutputTokens: 16}}, "end_turn", ""},
		{"repeated total is idempotent", []string{primary, qualifiedTotal, qualifiedTotal}, map[string]provider.TokenUsage{"custom-local:gpt-5": {InputTokens: 256, OutputTokens: 64}}, "end_turn", ""},
		{"failed turn still reports consumed total", []string{primary, primary, `{"type":"result","is_error":true,"result":"request failed","modelUsage":{"route:gpt-5":{"inputTokens":256,"outputTokens":64}}}`}, map[string]provider.TokenUsage{"route:gpt-5": {InputTokens: 256, OutputTokens: 64}}, "failed", ""},
		{"process failure keeps reported total", []string{primary, qualifiedTotal}, map[string]provider.TokenUsage{"custom-local:gpt-5": {InputTokens: 256, OutputTokens: 64}}, "end_turn", "exit 7\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "usage-cli")
			script := "#!/bin/sh\n"
			for _, line := range test.lines {
				script += "printf '%s\\n' '" + strings.ReplaceAll(line, "'", "'\"'\"'") + "'\n"
			}
			script += test.trailer
			if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			session, err := New(Config{AgentName: provider.CodeBuddy, Bin: binary}).Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: root}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			for turn := 1; turn <= 2; turn++ {
				result, err := session.Prompt(context.Background(), "continue", nil)
				if (err != nil) != (test.trailer != "") || result.StopReason != test.stop || !reflect.DeepEqual(result.Usage, test.want) {
					t.Fatalf("turn=%d usage=%+v stop=%s err=%v, want=%+v", turn, result.Usage, result.StopReason, err, test.want)
				}
				cumulative := map[string]provider.TokenUsage{}
				for model, usage := range test.want {
					cumulative[model] = provider.TokenUsage{InputTokens: usage.InputTokens * int64(turn), OutputTokens: usage.OutputTokens * int64(turn), CacheReadTokens: usage.CacheReadTokens * int64(turn), CacheWriteTokens: usage.CacheWriteTokens * int64(turn)}
				}
				if !reflect.DeepEqual(session.CumulativeUsage(), cumulative) {
					t.Fatalf("turn-local totals leaked across turns: got=%+v want=%+v", session.CumulativeUsage(), cumulative)
				}
			}
		})
	}
}
