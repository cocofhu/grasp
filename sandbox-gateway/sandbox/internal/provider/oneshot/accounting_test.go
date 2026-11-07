package oneshot

import (
	"context"
	"testing"

	"backend/internal/provider"
)

type accountingRetryCodec struct{ baseFake }

func (accountingRetryCodec) Args(_ provider.OpenOptions, _, resume string) []string {
	if resume != "" {
		return []string{"sh", "-c", "printf 'spent\\n'; exit 1"}
	}
	return []string{"sh", "-c", "printf 'fresh\\n'"}
}
func (accountingRetryCodec) ParseLine(line []byte) ParseResult {
	if string(line) == "spent" {
		return ParseResult{Usage: map[string]provider.TokenUsage{"m": {InputTokens: 5}}}
	}
	return ParseResult{SessionID: "s", StopReason: "end_turn", Usage: map[string]provider.TokenUsage{"m": {InputTokens: 7}}}
}

func TestResumeFallbackIncludesConsumptionFromFailedAttempt(t *testing.T) {
	session, err := NewProvider(accountingRetryCodec{}).Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir()}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Prompt(context.Background(), "first", nil); err != nil {
		t.Fatal(err)
	}
	result, err := session.Prompt(context.Background(), "second", nil)
	if err != nil || result.Usage["m"].InputTokens != 12 || session.CumulativeUsage()["m"].InputTokens != 19 {
		t.Fatalf("usage lost on fallback: result=%+v cumulative=%+v err=%v", result, session.CumulativeUsage(), err)
	}
}

type cumulativeCodec struct{ baseFake }

func (cumulativeCodec) Args(_ provider.OpenOptions, _, resume string) []string {
	if resume != "" {
		return []string{"sh", "-c", "printf 'resumed\\nresumed\\n'"}
	}
	return []string{"sh", "-c", "printf 'fresh\\n'"}
}
func (cumulativeCodec) ParseLine(line []byte) ParseResult {
	n := int64(1)
	if string(line) == "resumed" {
		n = 2
	}
	return ParseResult{SessionID: "native", StopReason: "end_turn", UsageIsTotal: true, UsageIsSessionTotal: true, Usage: map[string]provider.TokenUsage{"m": {InputTokens: 8 * n, OutputTokens: 2 * n, CacheReadTokens: 2 * n}}}
}
func TestNativeSessionCumulativeUsageBecomesTurnDeltas(t *testing.T) {
	session, err := NewProvider(cumulativeCodec{}).Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir()}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for range 2 {
		result, err := session.Prompt(context.Background(), "continue", nil)
		if err != nil || result.Usage["m"] != (provider.TokenUsage{InputTokens: 8, OutputTokens: 2, CacheReadTokens: 2}) {
			t.Fatalf("turn=%+v err=%v", result, err)
		}
	}
	if session.CumulativeUsage()["m"] != (provider.TokenUsage{InputTokens: 16, OutputTokens: 4, CacheReadTokens: 4}) {
		t.Fatalf("cumulative=%+v", session.CumulativeUsage())
	}
}
