package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestAbsorbChatWrapsTurnWithPromptAndTurnEnd(t *testing.T) {
	start := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	res := &sandbox.ChatResult{
		Narration: "done", Thought: "hmm",
		Usage:  &models.TokenUsage{InputTokens: 7, OutputTokens: 3},
		Prompt: "do the thing", ImageCount: 2,
		StartedAt: start, EndedAt: start.Add(3 * time.Second),
	}
	var usage *models.TokenUsage
	var byModel models.TokenUsageByModel
	var events []models.AcpEvent
	absorbChat(&usage, &byModel, &events, res)

	if len(events) != 4 {
		t.Fatalf("want prompt, thought, message, turn_end; got %+v", events)
	}
	p, end := events[0], events[3]
	if p.Kind != models.AcpKindPrompt || p.Text != "do the thing" || p.Title != "2 images" || p.At != "2026-10-01T08:00:00Z" {
		t.Fatalf("bad prompt event: %+v", p)
	}
	if events[1].Kind != "thought" || events[2].Kind != "message" {
		t.Fatalf("reply events out of order: %+v", events[1:3])
	}
	if end.Kind != models.AcpKindTurnEnd || end.Status != "completed" || end.Usage == nil || end.Usage.InputTokens != 7 || end.At != "2026-10-01T08:00:03Z" {
		t.Fatalf("bad turn_end event: %+v", end)
	}
}

func TestTranscriptTurnEventsFailureAndTruncation(t *testing.T) {
	res := &sandbox.ChatResult{
		Prompt: strings.Repeat("a", transcriptPromptMaxBytes+10), StartedAt: time.Now(),
		ErrorText: "quota exceeded", Failed: true,
	}
	ev := transcriptTurnEvents(res)
	if !ev[0].Truncated || len(ev[0].Text) > transcriptPromptMaxBytes+len("…(truncated)") {
		t.Fatalf("prompt not capped: truncated=%v len=%d", ev[0].Truncated, len(ev[0].Text))
	}
	last := ev[len(ev)-1]
	if last.Status != "failed" || last.Text != "quota exceeded" {
		t.Fatalf("failure not recorded: %+v", last)
	}
}

func TestTranscriptTurnEventsWithoutPromptStampIsUnchanged(t *testing.T) {
	res := &sandbox.ChatResult{Narration: "x"}
	got := transcriptTurnEvents(res)
	if len(got) != 1 || got[0].Kind != "message" {
		t.Fatalf("unstamped result must keep legacy events: %+v", got)
	}
}

func TestInflightPromptsScopedToRun(t *testing.T) {
	c := &acpProvider{}
	c.setInflightPrompt(NodeReq{RunID: "r1", NodeID: "a"}, "q1", 1, time.Now())
	c.setInflightPrompt(NodeReq{RunID: "r2", NodeID: "b"}, "q2", 0, time.Now())
	got := c.InflightPrompts("r1")
	if len(got) != 1 || got["a"].Prompt != "q1" || got["a"].ImageCount != 1 {
		t.Fatalf("unexpected inflight: %+v", got)
	}
	c.clearInflightPrompt(NodeReq{RunID: "r1", NodeID: "a"})
	if got := c.InflightPrompts("r1"); got != nil {
		t.Fatalf("want cleared, got %+v", got)
	}
	reg := &ProviderRegistry{providers: map[AcpBackend]ExecProvider{BackendCursor: c}}
	if got := reg.InflightPrompts("r2"); got["b"].Prompt != "q2" {
		t.Fatalf("registry fan-out: %+v", got)
	}
}
