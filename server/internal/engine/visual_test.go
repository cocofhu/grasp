package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func visualGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "page", Type: "agent", Caps: capsPageAuto, Config: map[string]any{}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "page"},
			{ID: "e2", Source: "page", Target: "output"},
		},
	}
}

// TestVisualNode exercises execVisual: a visual node that writes page.html
// completes and the artifact is persisted with kind "html" (so the UI previews
// it in an iframe), while a node that fails to write page.html fails the run.
func TestVisualNode(t *testing.T) {
	// Happy path: page.html written -> completed, artifact stored as html.
	eng, db, _ := setupEngineGraphP(t, visualGraph())
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var art models.Artifact
	if err := db.Where("run_id = ? AND name = ?", run.ID, "page.html").First(&art).Error; err != nil {
		t.Fatalf("page.html artifact not stored: %v", err)
	}
	if art.Kind != "html" {
		t.Fatalf("artifact kind = %q, want html", art.Kind)
	}
	var nodeCopy models.Artifact
	if err := db.Where("run_id = ? AND name = ?", run.ID, "page.page.html").First(&nodeCopy).Error; err != nil {
		t.Fatalf("node-scoped page.page.html not stored: %v", err)
	}
	if nodeCopy.Kind != "html" || nodeCopy.NodeID != "page" {
		t.Fatalf("node copy = kind=%q node=%q", nodeCopy.Kind, nodeCopy.NodeID)
	}

	// Missing page.html -> failed (no failure edge -> routeFailure ends failed).
	eng2, db2, p2 := setupEngineGraphP(t, visualGraph())
	p2.visualSkipProduces = true
	run2, err := eng2.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db2, run2.ID, "failed")
}
