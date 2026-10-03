package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestWithoutPartsKeepsReplyAndTools(t *testing.T) {
	in := models.ReactMessage{Role: "agent", Text: "ok", Tools: []models.ReactTool{{Title: "Shell"}},
		Parts: []models.AcpPart{{Kind: "tool", Title: "Shell", Output: "secret"}}}
	out := withoutParts(in)
	if out.Parts != nil || out.Text != "ok" || len(out.Tools) != 1 || len(in.Parts) != 1 {
		t.Fatalf("withoutParts: %+v (input %+v)", out, in)
	}
}
