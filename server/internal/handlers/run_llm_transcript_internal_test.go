package handlers

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

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
