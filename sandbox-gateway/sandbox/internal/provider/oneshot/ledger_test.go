package oneshot

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	"backend/internal/provider"
)

// sessionTotalCodec reports the running session total the script prints:
// each run prints the next value of a counter kept in a file.
type sessionTotalCodec struct {
	baseFake
	counter string
}

func (c sessionTotalCodec) Args(provider.OpenOptions, string, string) []string {
	return []string{"sh", "-c", fmt.Sprintf("n=$(cat %[1]q 2>/dev/null || echo 0); n=$((n+1)); echo $n > %[1]q; echo $n", c.counter)}
}

func (sessionTotalCodec) ParseLine(line []byte) ParseResult {
	n, _ := strconv.ParseInt(string(line), 10, 64)
	return ParseResult{SessionID: "native", StopReason: "end_turn", UsageIsTotal: true, UsageIsSessionTotal: true,
		Usage: map[string]provider.TokenUsage{"m": {InputTokens: 8 * n, OutputTokens: 2 * n}}}
}

func TestSessionTotalsBecomeTurnUsageAcrossProcessReplacement(t *testing.T) {
	p := NewProvider(sessionTotalCodec{counter: filepath.Join(t.TempDir(), "n")})
	want := provider.TokenUsage{InputTokens: 8, OutputTokens: 2}
	resume := ""
	for open := range 2 {
		session, err := p.Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir(), ResumeSessionID: resume}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		for turn := range 2 {
			result, err := session.Prompt(context.Background(), "continue", nil)
			if err != nil || result.Usage["m"] != want || result.UsagePartial {
				t.Fatalf("open %d turn %d: usage=%+v partial=%v err=%v", open, turn, result.Usage, result.UsagePartial, err)
			}
		}
		resume = session.SessionID()
		session.Close()
	}
}

func TestSessionTotalThatGoesBackwardsIsTakenAsTheTurn(t *testing.T) {
	var l sessionLedger
	if _, reset := l.modelDelta("s", map[string]provider.TokenUsage{"m": {InputTokens: 10}}); reset {
		t.Fatal("first total flagged")
	}
	usage, reset := l.modelDelta("s", map[string]provider.TokenUsage{"m": {InputTokens: 4}})
	if !reset || usage["m"].InputTokens != 4 {
		t.Fatalf("usage=%+v reset=%v", usage, reset)
	}
	if usage, reset := l.modelDelta("", map[string]provider.TokenUsage{"m": {InputTokens: 4}}); reset || usage["m"].InputTokens != 4 {
		t.Fatalf("sessionless usage=%+v reset=%v", usage, reset)
	}
}

func TestSessionLedgerForgetsTheOldestSessions(t *testing.T) {
	var l sessionLedger
	for i := range ledgerSessions + 1 {
		l.modelDelta(strconv.Itoa(i), map[string]provider.TokenUsage{"m": {InputTokens: 1}})
	}
	if _, kept := l.sessions["0"]; kept || len(l.sessions) != ledgerSessions {
		t.Fatalf("kept %d sessions, oldest kept=%v", len(l.sessions), kept)
	}
}
