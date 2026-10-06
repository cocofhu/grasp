package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestStartRunFromPublishedUsesPublishedVersionNotHead(t *testing.T) {
	eng, db, p := setupBlockingEngine(t, slowGraph(), 5)
	defer p.ReleaseAll()

	published := slowGraph()
	published.Nodes[0].Label = "published v1"
	if err := db.Where("workflow_id = ?", "wf").Delete(&models.WorkflowVersion{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: "wf", Version: 1, Graph: published}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowVersion{WorkflowID: "wf", Version: 2, Graph: slowGraph()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").
		Updates(map[string]any{"version": 2, "published_version": 1}).Error; err != nil {
		t.Fatal(err)
	}

	run, err := eng.StartRunFromPublished("wf", nil, "", nil, nil)
	if err != nil {
		t.Fatalf("StartRunFromPublished: %v", err)
	}
	if run.WorkflowVersion != 1 || run.Graph.Nodes[0].Label != "published v1" {
		t.Fatalf("api run used v%d label=%q, want published v1", run.WorkflowVersion, run.Graph.Nodes[0].Label)
	}

	if err := db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").Update("published_version", 0).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := eng.StartRunFromPublished("wf", nil, "", nil, nil); err == nil {
		t.Fatal("unpublished workflow must be rejected")
	}
}
