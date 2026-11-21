package provider

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestTurnEndsFollowTheCancelCause(t *testing.T) {
	usage := map[string]TokenUsage{"m": {InputTokens: 3}}
	for _, c := range []struct {
		cause  error
		stop   string
		err    error
		frames []string
	}{
		{ErrTurnRecover, "recover", ErrTurnRecover, nil},
		{ErrTurnTimeout, StopReasonTimeout, ErrTurnTimeout, []string{"error_text", "prompt_done"}},
		{ErrTurnStuck, StopReasonStuck, ErrTurnStuck, []string{"error_text", "prompt_done"}},
		{context.Canceled, "cancelled", context.Canceled, []string{"prompt_done"}},
	} {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(c.cause)
		var frames []string
		emit := func(f map[string]any) { frames = append(frames, f["type"].(string)) }
		res, err := EndInterrupted(ctx, emit, "s", usage)
		if !errors.Is(err, c.err) || res.StopReason != c.stop || !res.UsagePartial || !reflect.DeepEqual(frames, c.frames) {
			t.Fatalf("cause %v: %+v %v %v", c.cause, res, err, frames)
		}
		frames = nil
		if res, err := EndFailed(ctx, emit, "s", "boom"); res.StopReason != c.stop || !errors.Is(err, c.err) {
			t.Fatalf("failed start under cause %v: %+v %v", c.cause, res, err)
		}
	}
	var frames []map[string]any
	res, err := EndFailed(context.Background(), func(f map[string]any) { frames = append(frames, f) }, "s", "boom")
	if res.StopReason != "failed" || err == nil || err.Error() != "boom" || len(frames) != 2 || frames[0]["text"] != "boom" || frames[1]["stopReason"] != "failed" {
		t.Fatalf("%+v %v %v", res, err, frames)
	}
}

func TestUsageAddsPerModel(t *testing.T) {
	total := map[string]TokenUsage{"a": {InputTokens: 1, CacheReadTokens: 2}}
	AddUsage(total, map[string]TokenUsage{"a": {InputTokens: 1, OutputTokens: 4, CacheWriteTokens: 1}, "b": {OutputTokens: 1}})
	want := map[string]TokenUsage{"a": {InputTokens: 2, OutputTokens: 4, CacheReadTokens: 2, CacheWriteTokens: 1}, "b": {OutputTokens: 1}}
	if !reflect.DeepEqual(total, want) {
		t.Fatalf("%+v", total)
	}
	if CopyUsage(map[string]TokenUsage{}) != nil {
		t.Fatal("empty usage copied")
	}
	kept := CopyUsage(total)
	total["a"] = TokenUsage{}
	if kept["a"].InputTokens != 2 {
		t.Fatal("copy shares the map")
	}
}
