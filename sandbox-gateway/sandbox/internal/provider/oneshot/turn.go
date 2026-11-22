package oneshot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"backend/internal/provider"
)

// turnAccumulator owns everything one turn learns from the CLI's output:
// session id, usage, stop reason, diagnostics. apply runs on the stdout
// consumer goroutine and, for LogFile codecs, once more on the turn goroutine
// after that consumer has been joined; it is never called concurrently.
type turnAccumulator struct {
	e     *engine
	bg    *backgroundShells
	input *turnInput

	usage               map[string]provider.TokenUsage
	messageUsage        map[string]map[string]provider.TokenUsage
	usagePartial        bool
	usageIsSessionTotal bool
	tracksSessionUsage  bool
	sessionUsage        *provider.TokenUsage
	newSID              string
	stop                string
	diagnostics         []Msg
	hasText             bool

	// resultSeen closes on the final stop reason; resultStop is written before
	// it closes, so a reader that observed the close may read it. With turn
	// input a result is final only once no steer is left unanswered.
	resultSeen chan struct{}
	resultOnce sync.Once
	resultStop string
}

func newTurnAccumulator(e *engine, bg *backgroundShells, input *turnInput) *turnAccumulator {
	return &turnAccumulator{e: e, bg: bg, input: input, usage: map[string]provider.TokenUsage{}, resultSeen: make(chan struct{})}
}

func (a *turnAccumulator) apply(pr ParseResult) {
	a.bg.event()
	a.tracksSessionUsage = a.tracksSessionUsage || pr.TracksSessionUsage
	if pr.SessionUsage != nil {
		copy := *pr.SessionUsage
		a.sessionUsage = &copy
	}
	if pr.SessionID != "" && a.newSID == "" {
		a.newSID = pr.SessionID
		a.e.mu.Lock()
		a.e.sessionID = pr.SessionID
		a.e.mu.Unlock()
	}
	if pr.UsageIsTotal && len(pr.Usage) > 0 {
		if zeroUsage(pr.Usage) && !zeroUsage(a.usage) {
			// A terminal zero cannot invalidate already observed nonzero
			// consumption. Retain it as a provisional measurement.
			pr.Usage = nil
			a.usagePartial = true
		} else {
			a.usage = make(map[string]provider.TokenUsage, len(pr.Usage))
			a.messageUsage = nil
			a.usagePartial = pr.UsagePartial
			a.usageIsSessionTotal = pr.UsageIsSessionTotal
		}
	} else if len(pr.Usage) > 0 {
		a.usagePartial = a.usagePartial || pr.UsagePartial
		if id := pr.UsageMessageID; id != "" {
			for model, prev := range a.messageUsage[id] {
				a.usage[model] = minusUsage(a.usage[model], prev)
			}
			if a.messageUsage == nil {
				a.messageUsage = map[string]map[string]provider.TokenUsage{}
			}
			a.messageUsage[id] = provider.CopyUsage(pr.Usage)
		}
	}
	provider.AddUsage(a.usage, pr.Usage)
	for _, m := range pr.Msgs {
		switch m.Kind {
		case KindText:
			a.hasText = a.hasText || strings.TrimSpace(m.Text) != ""
		case KindError:
			a.diagnostics = append(a.diagnostics, m)
			continue
		case KindToolUse:
			a.bg.toolStarted(m.ToolCallID)
		case KindToolResult:
			a.bg.toolFinished(m.ToolCallID, m.BackgroundPID)
		}
		a.e.emitUpdate(m)
	}
	if a.input != nil && pr.UserMessagesConsumed > 0 {
		a.input.consume(pr.UserMessagesConsumed)
	}
	if pr.StopReason != "" {
		a.stop = pr.StopReason
		if a.input != nil && !a.input.settle() {
			return
		}
		a.resultOnce.Do(func() {
			a.resultStop = pr.StopReason
			close(a.resultSeen)
		})
	}
}

// settleUsage turns what the CLI reported into this turn's own usage
// (reconciling native session counters against the previous turn) and adds it
// to the engine's cumulative usage.
func (a *turnAccumulator) settleUsage(resumeID string) {
	sid := a.newSID
	if sid == "" {
		sid = resumeID
	}
	if a.usageIsSessionTotal {
		var uncertain bool
		a.usage, uncertain = a.e.ledger.modelDelta(a.e.opts.StateDir, sid, a.usage)
		a.usagePartial = a.usagePartial || uncertain
	}
	if a.tracksSessionUsage {
		var uncertain bool
		a.usage, uncertain = a.e.ledger.reconcileFlat(a.e.opts.StateDir, sid, a.sessionUsage, a.usage)
		a.usagePartial = a.usagePartial || uncertain
	}
	a.e.mergeCumulative(a.usage)
}

// outcome decides the turn's stop reason and error. A stop reason the CLI
// reported wins; otherwise any wait or stream error means failed.
func (a *turnAccumulator) outcome(waitErr, streamErr error, tail *provider.OutputTail) (turnOutcome, error) {
	stop := a.stop
	if stop == "" {
		if waitErr != nil || streamErr != nil {
			stop = "failed"
		} else {
			stop = "end_turn"
		}
	}
	out := turnOutcome{stopReason: stop, usage: nonEmptyUsage(a.usage), usagePartial: a.usagePartial, newSessionID: a.newSID, diagnostics: a.diagnostics, hasText: a.hasText}
	if waitErr != nil {
		if streamErr != nil && !errors.Is(waitErr, context.Canceled) {
			return out, fmt.Errorf("%v; %w; stderr: %s", streamErr, waitErr, tail.String())
		}
		return out, fmt.Errorf("%w; stderr: %s", waitErr, tail.String())
	}
	if streamErr != nil {
		return out, fmt.Errorf("%w; stderr: %s", streamErr, tail.String())
	}
	return out, nil
}

// consumeStdout parses a turn's stdout with the codec and hands each result to
// apply. It returns the read error, if any.
func (e *engine) consumeStdout(r io.Reader, apply func(ParseResult)) error {
	if whole, ok := e.c.(WholeOutputCodec); ok {
		// Whole-document mode: drain stdout, parse once (pretty-printed JSON).
		buf, rerr := io.ReadAll(r)
		if len(buf) > 0 {
			apply(whole.ParseAll(buf))
		}
		if rerr != nil {
			return fmt.Errorf("oneshot: read stdout: %w", rerr)
		}
		return nil
	}
	// Per-turn parser: stateful codecs get a fresh instance each turn.
	var parser LineParser = e.c
	if sf, ok := e.c.(StatefulCodec); ok {
		parser = sf.NewTurnParser(e.opts)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		apply(parser.ParseLine(line))
	}
	if serr := sc.Err(); serr != nil {
		return fmt.Errorf("oneshot: scan stdout: %w", serr)
	}
	return nil
}
