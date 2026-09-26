package oneshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/provider"
)

// hangFake models the stuck turn from run-9857091b: the CLI prints some output,
// then blocks forever on a foreground command and never exits. With finished
// set it first reports its final result (the "done but lingering" variant).
type hangFake struct {
	baseFake
	finished bool
	spawns   *atomic.Int32
}

func (f hangFake) Args(_ provider.OpenOptions, _, _ string) []string {
	f.spawns.Add(1)
	lines := `'sid:S1' 'text:working'`
	if f.finished {
		lines += ` 'done'`
	}
	return []string{"sh", "-c", `printf '%s\n' ` + lines + `; exec sleep 30`}
}

func (hangFake) ParseLine(line []byte) ParseResult {
	s := string(line)
	switch {
	case strings.HasPrefix(s, "sid:"):
		return ParseResult{SessionID: strings.TrimPrefix(s, "sid:")}
	case strings.HasPrefix(s, "text:"):
		return ParseResult{Msgs: []Msg{{Kind: KindText, Text: strings.TrimPrefix(s, "text:")}}}
	case s == "done":
		return ParseResult{StopReason: "end_turn"}
	}
	return ParseResult{}
}

func runTimedOutTurn(t *testing.T, fake hangFake, resumeID string) (provider.TurnResult, error, []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var frames []map[string]any
	onEvent := func(b json.RawMessage) {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			mu.Lock()
			frames = append(frames, m)
			mu.Unlock()
		}
	}
	sess, err := NewProvider(fake).Open(context.Background(), context.Background(),
		provider.OpenOptions{Cwd: t.TempDir(), ResumeSessionID: resumeID}, onEvent, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	ctx, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(300*time.Millisecond, func() {
		cancel(fmt.Errorf("%w: idle", provider.ErrTurnTimeout))
	})
	start := time.Now()
	res, perr := sess.Prompt(ctx, "restart the server", nil)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timed-out turn did not end promptly: %s", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	return res, perr, frames
}

func lastPromptDone(frames []map[string]any) (string, bool) {
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i]["type"] == "prompt_done" {
			s, _ := frames[i]["stopReason"].(string)
			return s, true
		}
	}
	return "", false
}

func TestOneShotWatchdogCauseReportsTimeout(t *testing.T) {
	spawns := &atomic.Int32{}
	// A resume pointer is set so a misclassified failure would trigger the
	// "retry from scratch" fallback and spawn a second hanging CLI.
	res, err, frames := runTimedOutTurn(t, hangFake{spawns: spawns}, "S0")
	if !errors.Is(err, provider.ErrTurnTimeout) {
		t.Fatalf("err=%v, want ErrTurnTimeout", err)
	}
	if res.StopReason != provider.StopReasonTimeout {
		t.Fatalf("stop=%q want timeout", res.StopReason)
	}
	if got, ok := lastPromptDone(frames); !ok || got != provider.StopReasonTimeout {
		t.Fatalf("prompt_done stopReason=%q ok=%v", got, ok)
	}
	if n := spawns.Load(); n != 1 {
		t.Fatalf("timeout must not fall back to a fresh session: spawned %d CLIs", n)
	}
}

func TestOneShotWatchdogAfterFinalResultIsNotAFailure(t *testing.T) {
	res, err, _ := runTimedOutTurn(t, hangFake{finished: true, spawns: &atomic.Int32{}}, "")
	if err != nil {
		t.Fatalf("err=%v, want nil once the CLI reported its result", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop=%q want end_turn", res.StopReason)
	}
}
