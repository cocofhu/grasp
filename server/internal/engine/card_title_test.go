package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestCardTitleForNodeRef(t *testing.T) {
	c := &execCtx{graph: models.Graph{Nodes: []models.Node{
		{ID: "n1", Label: "节点A"},
		{ID: "n2", Label: "  "},
	}}}
	keys := []string{
		"plan", "clarified_requirement", "research",
		"test_result", "review", "implementation_result", "page", "content", "other",
	}
	for _, k := range keys {
		title := cardTitleForNodeRef(c, outputSourceRef{nodeID: "n1", outputKey: k})
		if title == "" {
			t.Fatalf("empty title for %s", k)
		}
	}
	if got := cardTitleForNodeRef(c, outputSourceRef{nodeID: "n2", outputKey: "x"}); got != "n2 · x" {
		t.Fatalf("blank label: %q", got)
	}
	if got := eNodeLabel(c, "missing"); got != "missing" {
		t.Fatalf("missing label: %q", got)
	}
}
