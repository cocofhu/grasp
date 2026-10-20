package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestArtifactSaveOverwriteBumpsRevision(t *testing.T) {
	db := newTestDB(t)
	arts := NewArtifactService(db)
	db.Create(&models.WorkflowDef{ID: "wf1", Name: "W"})
	db.Create(&models.Run{ID: "run-wf", WorkflowID: "wf1", WorkflowName: "W"})

	if _, err := arts.Save("run-wf", "n", "out.md", "markdown", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := arts.Save("run-wf", "n2", "out.md", "markdown", "y"); err != nil {
		t.Fatal(err)
	}
	rec, _ := arts.GetRecord("run-wf", "out.md")
	if rec.Revision != 2 {
		t.Fatalf("overwrite: %+v", rec)
	}
}

func TestArtifactTree(t *testing.T) {
	db := newTestDB(t)
	arts := NewArtifactService(db)
	now := time.Now()
	db.Create(&models.WorkflowDef{ID: "wf-z", Name: "Zeta"})
	db.Create(&models.WorkflowDef{ID: "wf-b", Name: "Build"})
	for i, a := range []models.Artifact{
		{WorkflowID: "wf-z", WorkflowName: "Zeta"},
		{WorkflowID: "wf-b", WorkflowName: "old name"},
		{WorkflowID: "wf-b", WorkflowName: "old name"},
		{},
		{WorkflowID: "wf-gone", WorkflowName: "Deleted WF"},
	} {
		a.ID = "a" + string(rune('0'+i))
		a.RunID, a.Name, a.CreatedAt, a.UpdatedAt = "r", a.ID, now, now
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}

	tree, err := arts.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if tree.Count != 5 || tree.SessionCount != 1 || len(tree.Workflows) != 3 {
		t.Fatalf("tree: %+v", tree)
	}
	if w := tree.Workflows[0]; w.WorkflowID != "wf-b" || w.WorkflowName != "Build" || w.Count != 2 {
		t.Fatalf("wf[0]: %+v", w)
	}
	if w := tree.Workflows[1]; w.WorkflowName != "Deleted WF" || w.Count != 1 {
		t.Fatalf("wf[1]: %+v", w)
	}
	if w := tree.Workflows[2]; w.WorkflowID != "wf-z" || w.Count != 1 {
		t.Fatalf("wf[2]: %+v", w)
	}
}
