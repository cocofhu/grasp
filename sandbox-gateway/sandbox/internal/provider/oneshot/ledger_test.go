package oneshot

import (
	"context"
	"fmt"
	"os"
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
	if _, reset := l.modelDelta("", "s", map[string]provider.TokenUsage{"m": {InputTokens: 10}}); reset {
		t.Fatal("first total flagged")
	}
	usage, reset := l.modelDelta("", "s", map[string]provider.TokenUsage{"m": {InputTokens: 4}})
	if !reset || usage["m"].InputTokens != 4 {
		t.Fatalf("usage=%+v reset=%v", usage, reset)
	}
	if usage, reset := l.modelDelta("", "", map[string]provider.TokenUsage{"m": {InputTokens: 4}}); reset || usage["m"].InputTokens != 4 {
		t.Fatalf("sessionless usage=%+v reset=%v", usage, reset)
	}
}

func TestSessionLedgerForgetsTheOldestSessions(t *testing.T) {
	var l sessionLedger
	for i := range ledgerSessions + 1 {
		l.modelDelta("", strconv.Itoa(i), map[string]provider.TokenUsage{"m": {InputTokens: 1}})
	}
	if _, kept := l.sessions["0"]; kept || len(l.sessions) != ledgerSessions {
		t.Fatalf("kept %d sessions, oldest kept=%v", len(l.sessions), kept)
	}
}

func TestSessionTotalsStayPerTurnAcrossBackendRestart(t *testing.T) {
	counter, state := filepath.Join(t.TempDir(), "n"), t.TempDir()
	want := provider.TokenUsage{InputTokens: 8, OutputTokens: 2}
	resume := ""
	for backend := range 2 {
		p := NewProvider(sessionTotalCodec{counter: counter})
		session, err := p.Open(context.Background(), context.Background(), provider.OpenOptions{Cwd: t.TempDir(), ResumeSessionID: resume, StateDir: state}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := session.Prompt(context.Background(), "continue", nil)
		if err != nil || result.Usage["m"] != want || result.UsagePartial {
			t.Fatalf("backend %d: usage=%+v partial=%v err=%v", backend, result.Usage, result.UsagePartial, err)
		}
		resume = session.SessionID()
		session.Close()
	}
}

func TestFlatSessionBaselineSurvivesBackendRestart(t *testing.T) {
	state := t.TempDir()
	var before, after sessionLedger
	before.reconcileFlat(state, "s", &provider.TokenUsage{InputTokens: 8}, nil)
	usage, uncertain := after.reconcileFlat(state, "s", &provider.TokenUsage{InputTokens: 16}, nil)
	if uncertain || usage["default"].InputTokens != 8 {
		t.Fatalf("usage=%+v uncertain=%v", usage, uncertain)
	}
}

func TestSavedBaselinesAreBounded(t *testing.T) {
	state := t.TempDir()
	var l sessionLedger
	for i := range ledgerSessions + 3 {
		l.modelDelta(state, strconv.Itoa(i), map[string]provider.TokenUsage{"m": {InputTokens: 1}})
	}
	entries, err := os.ReadDir(filepath.Join(state, "usage"))
	if err != nil || len(entries) != ledgerSessions {
		t.Fatalf("saved %d baselines (%v), want %d", len(entries), err, ledgerSessions)
	}
}

func TestUnreadableBaselineStartsOver(t *testing.T) {
	state := t.TempDir()
	path := baselinePath(state, "s")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	var l sessionLedger
	if usage, _ := l.modelDelta(state, "s", map[string]provider.TokenUsage{"m": {InputTokens: 5}}); usage["m"].InputTokens != 5 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestALedgerEntryInUseIsNotEvicted(t *testing.T) {
	l := &sessionLedger{}
	held, unlock := l.lock("", "held")
	for i := range ledgerSessions {
		_, u := l.lock("", fmt.Sprintf("s%d", i))
		u()
	}
	unlock()
	again, unlock := l.lock("", "held")
	defer unlock()
	if again != held {
		t.Fatal("an entry in use was evicted; its session now has two baselines")
	}
	if len(l.sessions) > ledgerSessions+1 {
		t.Fatalf("ledger grew to %d entries", len(l.sessions))
	}
}
