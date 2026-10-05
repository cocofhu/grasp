package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestUnknownNodeTypeFails(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "bad", Type: "not_a_real_type"},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{Source: "input", Target: "bad", Kind: models.EdgeSuccess},
			{Source: "bad", Target: "output", Kind: models.EdgeSuccess},
		},
	}
	eng, _, _ := setupEngineGraphP(t, g)
	_, err := eng.StartRun("wf", nil, "test")
	if err == nil || !strings.Contains(err.Error(), "未知节点类型 not_a_real_type") {
		t.Fatalf("StartRun err = %v, want 未知节点类型", err)
	}
}
