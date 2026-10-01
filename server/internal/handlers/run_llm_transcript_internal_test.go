package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestMergeRecentTurns(t *testing.T) {
	q := func(at string) models.AcpEvent { return models.AcpEvent{Kind: models.AcpKindPrompt, Text: at, At: at} }
	msg := models.AcpEvent{Kind: "message", Text: "a"}
	end := models.AcpEvent{Kind: models.AcpKindTurnEnd}
	started := time.Date(2026, 10, 1, 0, 0, 5, 400, time.UTC)

	persisted := []models.AcpEvent{q("2026-10-01T00:00:05.5Z"), msg, end}
	recent := []models.AcpEvent{
		q("2026-10-01T00:00:01Z"), msg, end, // previous execution
		q("2026-10-01T00:00:05.5Z"), msg, end, // already persisted
		q("2026-10-01T00:00:07Z"), msg, end, // new
	}
	got := mergeRecentTurns(persisted, recent, &started)
	if len(got) != 6 || got[3].At != "2026-10-01T00:00:07Z" {
		t.Fatalf("got %+v", got)
	}
	if len(persisted) != 3 {
		t.Fatal("persisted mutated")
	}
	if got := mergeRecentTurns(persisted, recent[3:6], &started); len(got) != 3 {
		t.Fatalf("nothing new should keep persisted: %+v", got)
	}
}

func TestPreviewPromptEventsDoesNotMutateInput(t *testing.T) {
	long := strings.Repeat("x", promptPreviewRunes+1)
	in := []models.AcpEvent{{Kind: models.AcpKindPrompt, Text: long}, {Kind: "message", Text: long}}
	out := previewPromptEvents(in)
	if in[0].Text != long {
		t.Fatal("input mutated")
	}
	if len([]rune(out[0].Text)) != promptPreviewRunes || !out[0].Truncated {
		t.Fatalf("prompt not previewed: %+v", out[0])
	}
	if out[1].Text != long {
		t.Fatal("non-prompt events must be untouched")
	}
	short := []models.AcpEvent{{Kind: models.AcpKindPrompt, Text: "hi"}}
	if got := previewPromptEvents(short); &got[0] != &short[0] {
		t.Fatal("short prompts should not copy the slice")
	}
}
