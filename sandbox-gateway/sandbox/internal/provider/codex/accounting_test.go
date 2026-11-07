package codex

import "testing"

func TestNativeUsageBucketsAreDisjointAndAuthoritative(t *testing.T) {
	for _, kind := range []string{"turn.completed", "turn.failed"} {
		result := (codec{}).ParseLine([]byte(`{"type":"` + kind + `","usage":{"input_tokens":100,"cached_input_tokens":60,"cache_write_input_tokens":10,"output_tokens":15}}`))
		u := result.Usage["default"]
		if !result.UsageIsTotal || u.InputTokens != 30 || u.CacheReadTokens != 60 || u.CacheWriteTokens != 10 || u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens != 115 {
			t.Fatalf("%s usage=%+v total=%v", kind, u, result.UsageIsTotal)
		}
	}
}

func TestMissingUsageIsNotAnExplicitZero(t *testing.T) {
	c := codec{}
	missing := c.ParseLine([]byte(`{"type":"turn.completed","usage":{}}`))
	zero := c.ParseLine([]byte(`{"type":"turn.completed","usage":{"input_tokens":0,"output_tokens":0}}`))
	nested := c.ParseLine([]byte(`{"msg":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100}}}}`))
	if len(missing.Usage) > 0 || len(nested.Usage) > 0 || len(zero.Usage) != 1 || zero.UsagePartial {
		t.Fatalf("missing=%+v nested=%+v zero=%+v", missing, nested, zero)
	}
}
