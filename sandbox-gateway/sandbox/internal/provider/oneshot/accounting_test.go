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
